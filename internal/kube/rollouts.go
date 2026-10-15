package kube

import (
	"context"
	"encoding/json"
	"fmt"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const revisionAnnotation = "deployment.kubernetes.io/revision"

type RolloutRevision struct {
	Number        int64
	Source        unstructured.Unstructured
	Template      map[string]interface{}
	Images, Cause string
}
type RolloutHistory struct {
	Resource   Resource
	Deployment unstructured.Unstructured
	Revisions  []RolloutRevision
}

func deploymentTemplate(o unstructured.Unstructured) (map[string]interface{}, error) {
	template, found, err := unstructured.NestedMap(o.Object, "spec", "template")
	if err != nil || !found {
		return nil, fmt.Errorf("%s %s has no valid pod template", o.GetKind(), o.GetName())
	}
	if metadata, ok := template["metadata"].(map[string]interface{}); ok {
		if labels, ok := metadata["labels"].(map[string]interface{}); ok {
			delete(labels, "pod-template-hash")
			if len(labels) == 0 {
				delete(metadata, "labels")
			}
		}
		if len(metadata) == 0 {
			delete(template, "metadata")
		}
	}
	return template, nil
}

func rolloutOwned(source, deployment unstructured.Unstructured) bool {
	if source.GetNamespace() != deployment.GetNamespace() {
		return false
	}
	for _, owner := range source.GetOwnerReferences() {
		if owner.UID == deployment.GetUID() && owner.Kind == "Deployment" && owner.Name == deployment.GetName() && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

func templateImages(template map[string]interface{}) string {
	var images []string
	o := unstructured.Unstructured{Object: template}
	for _, group := range []string{"initContainers", "containers"} {
		for _, value := range Slice(o, "spec", group) {
			c, _ := value.(map[string]interface{})
			images = append(images, fmt.Sprint(c["name"])+"="+fmt.Sprint(c["image"]))
		}
	}
	return strings.Join(images, ", ")
}

func ReadRolloutHistory(ctx context.Context, b Backend, resources []Resource, target unstructured.Unstructured) (RolloutHistory, error) {
	var result RolloutHistory
	if target.GetKind() != "Deployment" {
		return result, fmt.Errorf("rollout history supports Deployments; select a Deployment")
	}
	reader, ok := b.(ResourceGetter)
	if !ok {
		return result, fmt.Errorf("individual resource reads are unavailable")
	}
	r, err := ResourceFor(resources, "apps/v1", "Deployment")
	if err != nil {
		return result, err
	}
	fresh, err := reader.Get(ctx, r, target.GetNamespace(), target.GetName())
	if err != nil {
		return result, err
	}
	if target.GetUID() == "" || fresh.GetUID() != target.GetUID() {
		return result, fmt.Errorf("Deployment was replaced; refresh and select it again")
	}
	result.Resource, result.Deployment = r, fresh
	rs, err := ResourceFor(resources, "apps/v1", "ReplicaSet")
	if err != nil {
		return result, err
	}
	objects, err := b.List(ctx, Query{Resource: rs, Namespace: fresh.GetNamespace()})
	if err != nil {
		return result, err
	}
	seen := map[int64]bool{}
	for _, o := range objects {
		if !rolloutOwned(o, fresh) {
			continue
		}
		n, err := strconv.ParseInt(o.GetAnnotations()[revisionAnnotation], 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		if seen[n] {
			return result, fmt.Errorf("revision %d is ambiguous; refresh rollout history", n)
		}
		seen[n] = true
		template, err := deploymentTemplate(o)
		if err != nil {
			return result, err
		}
		if len(result.Revisions) >= 2000 {
			return result, fmt.Errorf("rollout history exceeds 2,000 revisions")
		}
		result.Revisions = append(result.Revisions, RolloutRevision{n, *o.DeepCopy(), template, templateImages(template), o.GetAnnotations()["kubernetes.io/change-cause"]})
	}
	slices.SortFunc(result.Revisions, func(a, b RolloutRevision) int {
		if a.Number > b.Number {
			return -1
		}
		if a.Number < b.Number {
			return 1
		}
		return 0
	})
	return result, nil
}

func PlanRollback(history RolloutHistory, revision int64) (ActionPlan, error) {
	for _, entry := range history.Revisions {
		if entry.Number == revision {
			if history.Deployment.GetUID() == "" || history.Deployment.GetResourceVersion() == "" {
				return ActionPlan{}, fmt.Errorf("refresh before rollback")
			}
			return ActionPlan{Resource: history.Resource, Object: *history.Deployment.DeepCopy(), Action: "rollback", Rollback: &entry, Summary: fmt.Sprintf("Restore pod template from revision %d\nImages: %s\nReplica count and rollout strategy stay as currently configured.", revision, Safe(entry.Images))}, nil
		}
	}
	return ActionPlan{}, fmt.Errorf("revision %d is unavailable; reopen rollout history", revision)
}

func applyRollback(ctx context.Context, b Backend, plan ActionPlan) (string, error) {
	writer, ok := b.(Editable)
	if !ok {
		return "", fmt.Errorf("resource updates are unavailable")
	}
	if plan.Rollback == nil || plan.Object.GetKind() != "Deployment" || plan.Object.GetUID() == "" {
		return "", fmt.Errorf("invalid rollback plan")
	}
	fresh, err := writer.Get(ctx, plan.Resource, plan.Object.GetNamespace(), plan.Object.GetName())
	if err != nil {
		return "", err
	}
	if fresh.GetUID() != plan.Object.GetUID() || fresh.GetDeletionTimestamp() != nil {
		return "", fmt.Errorf("Deployment was replaced or is terminating; reopen rollout history")
	}
	if paused, _, _ := unstructured.NestedBool(fresh.Object, "spec", "paused"); paused {
		return "", fmt.Errorf("Deployment is paused; resume it before rollback")
	}
	old, err := deploymentTemplate(plan.Object)
	if err != nil {
		return "", err
	}
	current, err := deploymentTemplate(fresh)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(old, current) {
		return "", fmt.Errorf("Deployment template changed; reopen rollout history before rollback")
	}
	source := plan.Rollback.Source
	rs, _ := ResourceFor(Builtins(), "apps/v1", "ReplicaSet")
	snapshot, err := writer.Get(ctx, rs, source.GetNamespace(), source.GetName())
	if err != nil {
		return "", fmt.Errorf("revision is unavailable: %w", err)
	}
	if source.GetUID() == "" || snapshot.GetUID() != source.GetUID() || !rolloutOwned(snapshot, fresh) || snapshot.GetAnnotations()[revisionAnnotation] != strconv.FormatInt(plan.Rollback.Number, 10) {
		return "", fmt.Errorf("revision changed or was replaced; reopen rollout history")
	}
	wanted, err := deploymentTemplate(snapshot)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(wanted, plan.Rollback.Template) {
		return "", fmt.Errorf("revision template changed; reopen rollout history")
	}
	if reflect.DeepEqual(current, wanted) {
		return fmt.Sprintf("Current template already matches revision %d", plan.Rollback.Number), nil
	}
	before, err := json.Marshal(fresh.Object)
	if err != nil {
		return "", err
	}
	after := fresh.DeepCopy()
	if err := unstructured.SetNestedMap(after.Object, wanted, "spec", "template"); err != nil {
		return "", err
	}
	next, err := json.Marshal(after.Object)
	if err != nil {
		return "", err
	}
	patch, err := jsonpatch.CreateMergePatch(before, next)
	if err != nil {
		return "", err
	}
	var values map[string]interface{}
	if err = json.Unmarshal(patch, &values); err != nil {
		return "", err
	}
	values["metadata"] = map[string]interface{}{"uid": string(fresh.GetUID()), "resourceVersion": fresh.GetResourceVersion()}
	patch, err = json.Marshal(values)
	if err != nil {
		return "", err
	}
	err = writer.Patch(ctx, plan.Resource, fresh.GetNamespace(), fresh.GetName(), patch)
	return fmt.Sprintf("Rollback to revision %d requested for %s", plan.Rollback.Number, fresh.GetName()), err
}

type TemplateChange struct{ Path, Before, After string }

func TemplateChanges(before, after map[string]interface{}) []TemplateChange {
	var changes []TemplateChange
	format := func(value interface{}, present bool) string {
		if !present {
			return "<absent>"
		}
		data, _ := json.Marshal(value)
		return string(data)
	}
	var walk func(string, interface{}, interface{}, bool, bool)
	walk = func(path string, a, b interface{}, hasA, hasB bool) {
		if hasA == hasB && reflect.DeepEqual(a, b) {
			return
		}
		am, aok := a.(map[string]interface{})
		bm, bok := b.(map[string]interface{})
		if aok && bok {
			keys := []string{}
			for k := range am {
				keys = append(keys, k)
			}
			for k := range bm {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			keys = slices.Compact(keys)
			for _, k := range keys {
				av, ae := am[k]
				bv, be := bm[k]
				walk(path+"/"+strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1"), av, bv, ae, be)
			}
			return
		}
		if aa, ok := a.([]interface{}); ok {
			if bb, ok := b.([]interface{}); ok {
				for i := range max(len(aa), len(bb)) {
					var av, bv interface{}
					if i < len(aa) {
						av = aa[i]
					}
					if i < len(bb) {
						bv = bb[i]
					}
					walk(path+"/"+strconv.Itoa(i), av, bv, i < len(aa), i < len(bb))
				}
				return
			}
		}
		changes = append(changes, TemplateChange{path, format(a, hasA), format(b, hasB)})
	}
	walk("", before, after, true, true)
	return changes
}

func RolloutCreated(revision RolloutRevision) string {
	when := revision.Source.GetCreationTimestamp()
	if when.IsZero() {
		return "Unknown"
	}
	return when.UTC().Format("2006-01-02 15:04:05 UTC")
}
