package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type ResourceMutator interface {
	Delete(context.Context, Resource, string, string, types.UID, string) error
	Create(context.Context, Resource, string, unstructured.Unstructured) (unstructured.Unstructured, error)
}
type ActionPlan struct {
	Resource        Resource
	Object          unstructured.Unstructured
	Action, Summary string
	Replicas        int64
}

func Actions(kind string) []string {
	result := []string{}
	if slices.Contains([]string{"Deployment", "StatefulSet", "ReplicaSet"}, kind) {
		result = append(result, "scale")
	}
	if slices.Contains([]string{"Deployment", "StatefulSet", "DaemonSet"}, kind) {
		result = append(result, "restart")
	}
	if kind == "CronJob" {
		result = append(result, "suspend", "resume", "run")
	}
	return append(result, "delete")
}
func PlanAction(r Resource, o unstructured.Unstructured, action, arg string) (ActionPlan, error) {
	plan := ActionPlan{Resource: r, Object: *o.DeepCopy(), Action: action}
	if !slices.Contains(Actions(o.GetKind()), action) {
		return plan, fmt.Errorf("%s is unavailable for %s", action, o.GetKind())
	}
	if o.GetUID() == "" || o.GetResourceVersion() == "" {
		return plan, fmt.Errorf("refresh before changing this resource")
	}
	if o.GetDeletionTimestamp() != nil {
		return plan, fmt.Errorf("resource is already terminating")
	}
	if action == "scale" {
		n, e := strconv.ParseInt(arg, 10, 32)
		if e != nil || n < 0 {
			return plan, fmt.Errorf("replicas must be a nonnegative integer")
		}
		plan.Replicas = n
		plan.Summary = fmt.Sprintf("Scale from %d to %d replicas", Number(o, "spec", "replicas"), n)
	} else {
		if arg != "" {
			return plan, fmt.Errorf("%s does not accept arguments", action)
		}
		plan.Summary = map[string]string{"restart": "Restart the workload rollout", "suspend": "Suspend future scheduled runs", "resume": "Resume scheduled runs", "run": "Create a Job from this CronJob's template now", "delete": "Delete this resource with normal Kubernetes termination"}[action]
	}
	return plan, nil
}
func ApplyAction(ctx context.Context, b Backend, plan ActionPlan) (string, error) {
	arg := ""
	if plan.Action == "scale" {
		arg = strconv.FormatInt(plan.Replicas, 10)
	}
	if _, err := PlanAction(plan.Resource, plan.Object, plan.Action, arg); err != nil {
		return "", err
	}
	for attempt := 0; attempt < 3; attempt++ {
		message, err := applyActionAttempt(ctx, b, plan)
		if !apierrors.IsConflict(err) || attempt == 2 {
			return message, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", fmt.Errorf("resource kept changing; refresh and try again")
}
func applyActionAttempt(ctx context.Context, b Backend, plan ActionPlan) (string, error) {
	reader, ok := b.(ResourceGetter)
	if !ok {
		return "", fmt.Errorf("resource reads are unavailable")
	}
	o := *plan.Object.DeepCopy()
	fresh, err := reader.Get(ctx, plan.Resource, o.GetNamespace(), o.GetName())
	if err != nil {
		return "", err
	}
	before, hasSpec := o.Object["spec"]
	changed := fresh.GetResourceVersion() != o.GetResourceVersion()
	if hasSpec {
		changed = !reflect.DeepEqual(before, fresh.Object["spec"])
	}
	if fresh.GetUID() != o.GetUID() || changed {
		return "", fmt.Errorf("resource changed since confirmation began; refresh and try again")
	}
	// Status updates may advance resourceVersion during confirmation. Apply
	// against the fresh version after checking the captured desired state.
	o.SetResourceVersion(fresh.GetResourceVersion())
	switch plan.Action {
	case "delete":
		writer, ok := b.(ResourceMutator)
		if !ok {
			return "", fmt.Errorf("resource deletion is unavailable")
		}
		err = writer.Delete(ctx, plan.Resource, o.GetNamespace(), o.GetName(), o.GetUID(), o.GetResourceVersion())
		return "Deletion requested for " + o.GetName(), err
	case "run":
		writer, ok := b.(ResourceMutator)
		if !ok {
			return "", fmt.Errorf("Job creation is unavailable")
		}
		spec, exists, e := unstructured.NestedMap(fresh.Object, "spec", "jobTemplate", "spec")
		if e != nil || !exists {
			return "", fmt.Errorf("CronJob has no valid job template")
		}
		job := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "batch/v1", "kind": "Job", "spec": spec}}
		job.SetGenerateName(o.GetName()[:min(48, len(o.GetName()))] + "-manual-")
		job.SetNamespace(o.GetNamespace())
		labels, _, _ := unstructured.NestedStringMap(fresh.Object, "spec", "jobTemplate", "metadata", "labels")
		job.SetLabels(labels)
		annotations, _, _ := unstructured.NestedStringMap(fresh.Object, "spec", "jobTemplate", "metadata", "annotations")
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["cronjob.kubernetes.io/instantiate"] = "manual"
		job.SetAnnotations(annotations)
		controller := true
		job.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: o.GetAPIVersion(), Kind: o.GetKind(), Name: o.GetName(), UID: o.GetUID(), Controller: &controller}})
		r, _ := Resolve(Builtins(), "jobs")
		created, e := writer.Create(ctx, r, o.GetNamespace(), job)
		return "Created Job " + created.GetName(), e
	default:
		writer, ok := b.(Editable)
		if !ok {
			return "", fmt.Errorf("resource updates are unavailable")
		}
		var spec map[string]interface{}
		switch plan.Action {
		case "scale":
			spec = map[string]interface{}{"replicas": plan.Replicas}
		case "suspend", "resume":
			spec = map[string]interface{}{"suspend": plan.Action == "suspend"}
		case "restart":
			spec = map[string]interface{}{"template": map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339Nano)}}}}
		default:
			return "", fmt.Errorf("unknown action")
		}
		patch, e := json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{"uid": string(o.GetUID()), "resourceVersion": o.GetResourceVersion()}, "spec": spec})
		if e != nil {
			return "", e
		}
		err = writer.Patch(ctx, plan.Resource, o.GetNamespace(), o.GetName(), patch)
		return plan.Action + " requested for " + o.GetName(), err
	}
}
func (c *Client) Delete(ctx context.Context, r Resource, ns, name string, uid types.UID, version string) error {
	policy := metav1.DeletePropagationBackground
	return c.resource(r, ns).Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}, PropagationPolicy: &policy})
}
func (c *Client) Create(ctx context.Context, r Resource, ns string, o unstructured.Unstructured) (unstructured.Unstructured, error) {
	created, err := c.resource(r, ns).Create(ctx, &o, metav1.CreateOptions{FieldManager: "kqt"})
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	return *created, nil
}
func (d *Demo) Delete(ctx context.Context, r Resource, ns, name string, uid types.UID, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, o := range d.objects {
		if o.GetKind() == r.Kind && o.GetNamespace() == ns && o.GetName() == name {
			if o.GetUID() != uid || o.GetResourceVersion() != version {
				return fmt.Errorf("resource changed")
			}
			d.objects = append(d.objects[:i], d.objects[i+1:]...)
			d.notifyResourcesLocked()
			return nil
		}
	}
	return fmt.Errorf("resource no longer exists")
}
func (d *Demo) Create(ctx context.Context, r Resource, ns string, o unstructured.Unstructured) (unstructured.Unstructured, error) {
	if ctx.Err() != nil {
		return o, ctx.Err()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	o = *o.DeepCopy()
	o.SetName(o.GetGenerateName() + strconv.FormatInt(time.Now().UnixNano(), 36))
	o.SetGenerateName("")
	o.SetNamespace(ns)
	o.SetUID(types.UID(ns + "/" + o.GetName()))
	o.SetResourceVersion("1")
	o.SetCreationTimestamp(metav1.Now())
	d.objects = append(d.objects, o)
	d.notifyResourcesLocked()
	return *o.DeepCopy(), nil
}
