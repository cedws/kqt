package kube

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

func SchedulingDiagnostics(ctx context.Context, b Backend, selected unstructured.Unstructured) (DiagnosticReport, error) {
	r := DiagnosticReport{Title: "Scheduling diagnostics · " + selected.GetNamespace() + "/" + selected.GetName()}
	if selected.GetKind() != "Pod" {
		return r, fmt.Errorf("select a Pod for scheduling diagnostics")
	}
	o, err := diagnosticTarget(ctx, b, selected)
	if err != nil {
		return r, err
	}
	var pod corev1.Pod
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &pod); err != nil {
		return r, err
	}
	scheduler := pod.Spec.SchedulerName
	if scheduler == "" {
		scheduler = "default-scheduler"
	}
	state := []string{"Scheduler: " + scheduler, "Phase: " + string(pod.Status.Phase)}
	if pod.Spec.NodeName != "" {
		state = append(state, "Assigned node: "+pod.Spec.NodeName)
	}
	found := false
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled {
			found = true
			state = append(state, fmt.Sprintf("PodScheduled=%s · %s · %s", c.Status, c.Reason, c.Message))
		}
	}
	if !found {
		state = append(state, "PodScheduled condition is not yet available.")
	}
	for _, g := range pod.Spec.SchedulingGates {
		state = append(state, "Scheduling gate: "+g.Name)
	}
	r.add("Scheduling state", state...)
	events, e := EventsForResource(ctx, b, o)
	if e != nil {
		r.add("Scheduler events · unavailable", e.Error())
	} else {
		slices.SortFunc(events, func(a, b unstructured.Unstructured) int { return EventTime(a).Compare(EventTime(b)) })
		var lines []string
		for _, event := range events {
			reason := Text(event, "reason")
			source := Text(event, "reportingComponent") + Text(event, "source", "component")
			if reason == "FailedScheduling" || reason == "Scheduled" || strings.Contains(source, "scheduler") {
				lines = append(lines, EventTime(event).Format("2006-01-02 15:04:05Z07:00")+" · "+reason+" · "+Text(event, "message"))
			}
		}
		if len(lines) == 0 {
			lines = []string{"No scheduler events are retained for this Pod UID."}
		}
		if len(lines) > 50 {
			lines = append([]string{"Only the latest 50 scheduler events are shown."}, lines[len(lines)-50:]...)
		}
		r.add("Scheduler events", lines...)
	}
	var constraints []string
	if len(pod.Spec.NodeSelector) > 0 {
		constraints = append(constraints, "nodeSelector: "+labels.Set(pod.Spec.NodeSelector).AsSelector().String())
	}
	if pod.Spec.Affinity != nil {
		data, _ := yaml.Marshal(pod.Spec.Affinity)
		constraints = append(constraints, "Affinity:\n"+strings.TrimSpace(string(data)))
	}
	if len(pod.Spec.TopologySpreadConstraints) > 0 {
		data, _ := yaml.Marshal(pod.Spec.TopologySpreadConstraints)
		constraints = append(constraints, "Topology spread:\n"+strings.TrimSpace(string(data)))
	}
	if len(constraints) == 0 {
		constraints = []string{"No node selector, affinity or topology spread constraints declared."}
	}
	r.add("Placement constraints", constraints...)
	var tolerations []string
	for _, t := range pod.Spec.Tolerations {
		op := t.Operator
		if op == "" {
			op = corev1.TolerationOpEqual
		}
		line := fmt.Sprintf("%s %s %s · effect=%s", t.Key, op, t.Value, t.Effect)
		if t.TolerationSeconds != nil {
			line += fmt.Sprintf(" · %ds", *t.TolerationSeconds)
		}
		tolerations = append(tolerations, line)
	}
	if len(tolerations) == 0 {
		tolerations = []string{"No tolerations declared."}
	}
	r.add("Tolerations", tolerations...)
	var requests []string
	for _, c := range pod.Spec.Containers {
		requests = append(requests, "container "+c.Name+": "+resourceList(c.Resources.Requests))
	}
	for _, c := range pod.Spec.InitContainers {
		kind := "init container"
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			kind = "restartable init sidecar"
		}
		requests = append(requests, kind+" "+c.Name+": "+resourceList(c.Resources.Requests))
	}
	if pod.Spec.Resources != nil {
		requests = append(requests, "Pod-level requests: "+resourceList(pod.Spec.Resources.Requests))
	}
	if len(pod.Spec.Overhead) > 0 {
		requests = append(requests, "Pod overhead: "+resourceList(pod.Spec.Overhead))
	}
	requests = append(requests, "Requests are shown per container; init ordering, sidecars and Pod-level resources affect the scheduler's effective total.")
	r.add("Resource requests", requests...)
	nr, _ := Resolve(Builtins(), "nodes")
	var nodes []unstructured.Unstructured
	if pod.Spec.NodeName != "" {
		n, e2 := b.(ResourceGetter).Get(ctx, nr, "", pod.Spec.NodeName)
		e = e2
		if e == nil {
			nodes = []unstructured.Unstructured{n}
		}
	} else {
		nodes, e = b.List(ctx, Query{Resource: nr})
	}
	if e != nil {
		r.add("Nodes · unavailable", e.Error(), "Node eligibility is unknown with the current permissions.")
	} else {
		var lines []string
		for i, o := range nodes {
			if i >= 500 {
				lines = append(lines, "Only the first 500 nodes are shown.")
				break
			}
			var node corev1.Node
			if e := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &node); e != nil {
				lines = append(lines, "Unavailable: "+e.Error())
				continue
			}
			var reasons []string
			if node.Spec.Unschedulable {
				reasons = append(reasons, "cordoned")
			}
			ready := "unknown"
			for _, c := range node.Status.Conditions {
				if c.Type == corev1.NodeReady {
					ready = string(c.Status)
				}
			}
			for key, value := range pod.Spec.NodeSelector {
				if actual, present := node.Labels[key]; !present || actual != value {
					reasons = append(reasons, "nodeSelector mismatch: "+key)
				}
			}
			for _, taint := range node.Spec.Taints {
				if !tolerates(pod.Spec.Tolerations, taint) {
					message := "untolerated " + string(taint.Effect) + " taint: " + taint.Key + "=" + taint.Value
					if taint.Effect == corev1.TaintEffectPreferNoSchedule {
						message += " (soft preference)"
					}
					reasons = append(reasons, message)
				}
			}
			if len(reasons) == 0 {
				reasons = []string{"no selector/taint conflict observed"}
			}
			lines = append(lines, fmt.Sprintf("%s · Ready=%s · %s", node.Name, ready, strings.Join(reasons, "; ")), "  Allocatable: "+resourceList(node.Status.Allocatable))
		}
		if len(nodes) == 0 {
			lines = []string{"No nodes returned."}
		}
		r.add("Node observations", lines...)
	}
	reader := b.(ResourceGetter)
	pvcResource, _ := Resolve(Builtins(), "pvc")
	seen := map[string]bool{}
	var claims []string
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		name := v.PersistentVolumeClaim.ClaimName
		if seen[name] {
			continue
		}
		seen[name] = true
		if len(seen) > 50 {
			claims = append(claims, "Only the first 50 PVCs are checked.")
			break
		}
		claim, e := reader.Get(ctx, pvcResource, pod.Namespace, name)
		if e != nil {
			claims = append(claims, name+" · unavailable: "+e.Error())
			continue
		}
		phase := Text(claim, "status", "phase")
		claims = append(claims, fmt.Sprintf("%s · %s · volume=%s · storageClass=%s", name, phase, Text(claim, "spec", "volumeName"), Text(claim, "spec", "storageClassName")))
		if phase == "Pending" {
			claims = append(claims, "  Pending may be expected with WaitForFirstConsumer; consult scheduler/provisioner events.")
		}
	}
	if len(claims) == 0 {
		claims = []string{"No persistentVolumeClaim volumes declared."}
	}
	r.add("PVC dependencies", claims...)
	r.add("Scope", "Scheduler events are the primary explanation. Affinity, topology, admission plugins, available capacity after other pods, quotas, volume topology and custom schedulers are not fully evaluated here. Node observations do not prove a Pod can schedule.")
	return r, ctx.Err()
}
func resourceList(resources corev1.ResourceList) string {
	if len(resources) == 0 {
		return "none declared"
	}
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, string(key))
	}
	slices.Sort(keys)
	var values []string
	for _, key := range keys {
		q := resources[corev1.ResourceName(key)]
		values = append(values, key+"="+q.String())
	}
	return strings.Join(values, ", ")
}
func tolerates(tolerations []corev1.Toleration, taint corev1.Taint) bool {
	for _, t := range tolerations {
		if t.Effect != "" && t.Effect != taint.Effect {
			continue
		}
		if t.Key != "" && t.Key != taint.Key {
			continue
		}
		if t.Operator == corev1.TolerationOpExists {
			return true
		}
		if (t.Operator == "" || t.Operator == corev1.TolerationOpEqual) && t.Key == taint.Key && t.Value == taint.Value {
			return true
		}
	}
	return false
}
