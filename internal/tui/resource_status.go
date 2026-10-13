package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type statusWriter struct {
	strings.Builder
	theme theme
}

func (w *statusWriter) heading(text string) {
	w.WriteString(w.theme.style("accent").Bold(true).Render(text) + "\n")
}
func (w *statusWriter) field(label, value, colour string) {
	if value == "" {
		value = "—"
	}
	fmt.Fprintf(w, "  %-16s %s\n", kube.Safe(label), w.theme.style(colour).Render(kube.SafeLines(value)))
}
func (w *statusWriter) note(text string) {
	if text != "" {
		for _, line := range strings.Split(kube.SafeLines(text), "\n") {
			w.WriteString("    " + line + "\n")
		}
	}
}

// Conditions express different predicates; False is healthy for pressure or
// failure conditions. Unknown custom predicates keep a neutral colour.
func conditionColour(kind, name, state, reason string) string {
	if state == "Unknown" || state == "" {
		return "warning"
	}
	if state != "True" && state != "False" {
		return "muted"
	}
	switch name {
	case "MemoryPressure", "DiskPressure", "PIDPressure", "NetworkUnavailable", "OutOfDisk", "Failed", "FailureTarget", "ReplicaFailure", "Degraded", "DisruptionTarget":
		if state == "True" {
			return "error"
		}
		return "success"
	case "Ready", "ContainersReady", "Initialized", "PodScheduled", "PodReadyToStartContainers", "Available", "Complete", "Established", "NamesAccepted", "Accepted", "Healthy", "Synced":
		if state == "True" {
			return "success"
		}
		return "warning"
	case "Progressing":
		if state == "False" {
			return "error"
		}
		if kind == "Deployment" && reason == "NewReplicaSetAvailable" {
			return "success"
		}
		return "warning"
	default:
		return "focus"
	}
}

func (m *model) resourceState(o unstructured.Unstructured) (string, string) {
	if o.GetDeletionTimestamp() != nil {
		return "Terminating", "warning"
	}
	if status, present, _ := unstructured.NestedMap(o.Object, "status"); !present || len(status) == 0 {
		return "No reported status", "muted"
	}
	state := kube.Status(o)
	if o.GetKind() == "Deployment" {
		for _, value := range kube.Slice(o, "status", "conditions") {
			c, _ := value.(map[string]interface{})
			if c["type"] == "Progressing" && c["status"] == "False" {
				return "Rollout blocked", "error"
			}
		}
		desired := desiredReplicas(o)
		if kube.Number(o, "status", "observedGeneration") < o.GetGeneration() || kube.Number(o, "status", "updatedReplicas") < desired || kube.Number(o, "status", "availableReplicas") < desired || kube.Number(o, "status", "replicas") > desired {
			state = "Progressing"
		}
	}
	if state == "—" {
		return "Reported below", "muted"
	}
	return state, m.statusToken(state)
}

func desiredReplicas(o unstructured.Unstructured) int64 {
	if _, exists, _ := unstructured.NestedFieldNoCopy(o.Object, "spec", "replicas"); !exists {
		return 1
	}
	return kube.Number(o, "spec", "replicas")
}

func (m *model) renderResourceStatus(o unstructured.Unstructured, events []unstructured.Unstructured, eventsError error) string {
	w := &statusWriter{theme: m.theme}
	name := o.GetName()
	if o.GetNamespace() != "" {
		name = o.GetNamespace() + "/" + name
	}
	w.heading(kube.Safe(o.GetKind()) + " · " + kube.Safe(name))
	state, colour := m.resourceState(o)
	w.field("Status", state, colour)
	w.field("Age", kube.Age(o), "foreground")
	if o.GetGeneration() > 0 {
		generation := fmt.Sprintf("%d", o.GetGeneration())
		if observed, present, _ := unstructured.NestedInt64(o.Object, "status", "observedGeneration"); present {
			generation += fmt.Sprintf(" · observed %d", observed)
			if observed < o.GetGeneration() {
				generation += " · awaiting reconciliation"
			}
		}
		w.field("Generation", generation, "muted")
	}
	for _, field := range []string{"reason", "message"} {
		if value := kube.Text(o, "status", field); value != "" {
			w.field(strings.Title(field), value, "foreground")
		}
	}
	switch o.GetKind() {
	case "Pod":
		w.field("Phase", kube.Text(o, "status", "phase"), "foreground")
		w.field("Ready", kube.Column(o, "ready"), "foreground")
		w.field("Node", kube.Text(o, "spec", "nodeName"), "foreground")
		w.field("Pod IP", kube.Text(o, "status", "podIP"), "foreground")
		w.WriteByte('\n')
		m.writeContainers(w, o)
	case "Deployment", "StatefulSet", "ReplicaSet":
		desired := desiredReplicas(o)
		w.field("Replicas", fmt.Sprintf("%d ready / %d desired", kube.Number(o, "status", "readyReplicas"), desired), "foreground")
		for _, field := range []struct{ label, key string }{{"Total", "replicas"}, {"Updated", "updatedReplicas"}, {"Available", "availableReplicas"}, {"Unavailable", "unavailableReplicas"}} {
			if value, present, _ := unstructured.NestedInt64(o.Object, "status", field.key); present {
				w.field(field.label, fmt.Sprint(value), "foreground")
			}
		}
	case "DaemonSet":
		w.field("Replicas", fmt.Sprintf("%d ready / %d desired", kube.Number(o, "status", "numberReady"), kube.Number(o, "status", "desiredNumberScheduled")), "foreground")
		w.field("Updated", fmt.Sprint(kube.Number(o, "status", "updatedNumberScheduled")), "foreground")
	case "Job":
		for _, key := range []string{"active", "succeeded", "failed"} {
			w.field(strings.Title(key), fmt.Sprint(kube.Number(o, "status", key)), "foreground")
		}
	case "Node":
		if cordoned, _, _ := unstructured.NestedBool(o.Object, "spec", "unschedulable"); cordoned {
			w.field("Scheduling", "Cordoned", "warning")
		} else {
			w.field("Scheduling", "Schedulable", "foreground")
		}
	}
	w.WriteByte('\n')
	w.heading("Conditions")
	conditions := kube.Slice(o, "status", "conditions")
	if len(conditions) == 0 {
		w.note("No conditions reported by this resource.")
	}
	for _, value := range conditions {
		c, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		condition := unstructured.Unstructured{Object: c}
		name, status, reason := kube.Text(condition, "type"), kube.Text(condition, "status"), kube.Text(condition, "reason")
		colour := conditionColour(o.GetKind(), name, status, reason)
		stale := ""
		if observed, present, _ := unstructured.NestedInt64(c, "observedGeneration"); present && observed < o.GetGeneration() {
			stale = fmt.Sprintf(" · stale (generation %d/%d)", observed, o.GetGeneration())
			colour = "warning"
		}
		w.field(name, status+stale, colour)
		w.note(reason)
		w.note(kube.Text(condition, "message"))
		if timestamp := kube.Text(condition, "lastTransitionTime"); timestamp != "" {
			w.note("Changed " + timestamp)
		}
	}
	// Keep arbitrary CRD status fields available without assuming their schema.
	switch o.GetKind() {
	case "Pod", "Deployment", "StatefulSet", "ReplicaSet", "DaemonSet", "Job", "Node":
	default:
		if status, present, _ := unstructured.NestedMap(o.Object, "status"); present {
			if len(status) > 0 {
				w.WriteByte('\n')
				w.heading("Reported status fields")
				w.WriteString(m.theme.renderYAML(kube.Manifest(unstructured.Unstructured{Object: map[string]interface{}{"status": status}}, false)))
			}
		}
	}
	w.WriteByte('\n')
	if m.eventFeed != nil && !m.eventGlobal {
		w.heading("Events (oldest first · live feed)")
	} else {
		w.heading("Events (newest first)")
	}
	if eventsError != nil {
		w.field("Unavailable", eventsError.Error(), "warning")
		w.note("Resource status is shown above. Ctrl+R retries reading events.")
	} else if len(events) == 0 {
		w.note("No events for this resource. Kubernetes retains events for a limited time.")
	}
	for _, event := range events {
		timestamp := "Time not reported"
		if observed := kube.EventTime(event); !observed.IsZero() {
			timestamp = observed.UTC().Format(time.RFC3339)
		}
		repeats := kube.EventCount(event)
		kind := kube.Text(event, "type")
		if m.eventStore != nil {
			if entry := m.eventStore.items[kube.Identity(event)]; entry != nil && entry.deleted {
				kind += " (removed)"
			}
		}
		colour := "muted"
		if kube.Text(event, "type") == "Warning" {
			colour = "warning"
		}
		w.field(kind, fmt.Sprintf("%s · %s · ×%d", kube.Text(event, "reason"), timestamp, repeats), colour)
		w.note(kube.EventMessage(event))
	}
	return w.String()
}

func (m *model) writeContainers(w *statusWriter, o unstructured.Unstructured) {
	w.heading("Containers")
	for _, group := range []struct{ spec, status, label string }{{"initContainers", "initContainerStatuses", "init"}, {"containers", "containerStatuses", "app"}, {"ephemeralContainers", "ephemeralContainerStatuses", "ephemeral"}} {
		states := map[string]unstructured.Unstructured{}
		for _, value := range kube.Slice(o, "status", group.status) {
			if c, ok := value.(map[string]interface{}); ok {
				state := unstructured.Unstructured{Object: c}
				states[kube.Text(state, "name")] = state
			}
		}
		for _, value := range kube.Slice(o, "spec", group.spec) {
			c, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := c["name"].(string)
			state, reported := states[name]
			if !reported {
				w.field(name+" ("+group.label+")", "Waiting for container status", "warning")
				continue
			}
			label, colour, message := "Unknown", "muted", ""
			for _, key := range []string{"waiting", "terminated", "running"} {
				if _, present, _ := unstructured.NestedMap(state.Object, "state", key); !present {
					continue
				}
				label = strings.Title(key)
				if reason := kube.Text(state, "state", key, "reason"); reason != "" {
					label += " · " + reason
				}
				colour = "warning"
				if key == "terminated" {
					code := kube.Number(state, "state", key, "exitCode")
					label += fmt.Sprintf(" · exit %d", code)
					colour = "success"
					if code != 0 {
						colour = "error"
					}
				} else if key == "running" {
					if ready, _, _ := unstructured.NestedBool(state.Object, "ready"); ready {
						colour = "success"
					}
				} else if reason := kube.Text(state, "state", key, "reason"); reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
					colour = "error"
				}
				message = kube.Text(state, "state", key, "message")
				break
			}
			w.field(name+" ("+group.label+")", label, colour)
			w.note(message)
			ready := "Not ready"
			if value, _, _ := unstructured.NestedBool(state.Object, "ready"); value {
				ready = "Ready"
			}
			w.note(fmt.Sprintf("%s · %d restarts", ready, kube.Number(state, "restartCount")))
			if _, present, _ := unstructured.NestedMap(state.Object, "lastState", "terminated"); present {
				w.note(fmt.Sprintf("Last exit: %s · code %d · %s", kube.Text(state, "lastState", "terminated", "reason"), kube.Number(state, "lastState", "terminated", "exitCode"), kube.Text(state, "lastState", "terminated", "finishedAt")))
			}
		}
	}
}
