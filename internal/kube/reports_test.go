package kube

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func reportObject(t *testing.T, value any, kind string) unstructured.Unstructured {
	t.Helper()
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(value)
	if err != nil {
		t.Fatal(err)
	}
	o := unstructured.Unstructured{Object: object}
	o.SetKind(kind)
	o.SetAPIVersion("v1")
	if kind == "EndpointSlice" {
		o.SetAPIVersion("discovery.k8s.io/v1")
	}
	return o
}
func networkFixture(t *testing.T) (*Demo, unstructured.Unstructured) {
	t.Helper()
	meta := func(name, uid string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: "apps", UID: types.UID(uid), ResourceVersion: "1"}
	}
	svc := reportObject(t, &corev1.Service{ObjectMeta: meta("api", "svc-1"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: map[string]string{"app": "api"}, Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("http")}}}}, "Service")
	p := corev1.Pod{ObjectMeta: meta("api-1", "pod-1"), Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	p.Labels = map[string]string{"app": "api"}
	port := int32(8080)
	ready := true
	slice := discoveryv1.EndpointSlice{ObjectMeta: meta("api-a", "slice-1"), AddressType: discoveryv1.AddressTypeIPv4, Ports: []discoveryv1.EndpointPort{{Name: ptr("http"), Port: &port}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "apps", Name: "api-1", UID: "pod-1"}}}}
	slice.Labels = map[string]string{discoveryv1.LabelServiceName: "api"}
	d := &Demo{objects: []unstructured.Unstructured{svc, reportObject(t, &p, "Pod"), reportObject(t, &slice, "EndpointSlice")}}
	return d, svc
}
func ptr[T any](v T) *T { return &v }

type diagnosticBackend struct {
	*Demo
	denied       map[string]bool
	queries      []Query
	secretReads  int
	replaceAfter int
	gets         int
}

func (b *diagnosticBackend) List(ctx context.Context, q Query) ([]unstructured.Unstructured, error) {
	b.queries = append(b.queries, q)
	if b.denied[q.Resource.GVR.Resource] {
		return nil, apierrors.NewForbidden(q.Resource.GVR.GroupResource(), "", fmt.Errorf("test RBAC"))
	}
	return b.Demo.List(ctx, q)
}
func (b *diagnosticBackend) Get(ctx context.Context, r Resource, ns, name string) (unstructured.Unstructured, error) {
	if r.Kind == "Secret" {
		b.secretReads++
	}
	if b.denied[r.GVR.Resource] {
		return unstructured.Unstructured{}, apierrors.NewForbidden(r.GVR.GroupResource(), name, fmt.Errorf("test RBAC"))
	}
	b.gets++
	o, err := b.Demo.Get(ctx, r, ns, name)
	if b.replaceAfter > 0 && b.gets > b.replaceAfter {
		o.SetUID("replacement")
	}
	return o, err
}
func TestServiceDiagnosticsScopesAndDeduplicatesBackends(t *testing.T) {
	d, svc := networkFixture(t)
	dup := *d.objects[2].DeepCopy()
	dup.SetName("api-b")
	d.objects = append(d.objects, dup)
	b := &diagnosticBackend{Demo: d}
	report, err := ServiceDiagnostics(context.Background(), b, svc)
	if err != nil {
		t.Fatal(err)
	}
	text := report.Text()
	for _, want := range []string{"http 80/TCP → http", "1/1 ready published addresses", "api-1", "8080/TCP", "Matching pods (1)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "WARNING:") {
		t.Fatal(text)
	}
	for _, q := range b.queries {
		if q.Namespace != "apps" || q.Labels == "" {
			t.Fatalf("unscoped diagnostic query: %+v", q)
		}
	}
}
func TestServiceDiagnosticsMismatchesAndUnavailableData(t *testing.T) {
	d, svc := networkFixture(t)
	d.objects[1].SetLabels(map[string]string{"app": "different"})
	report, err := ServiceDiagnostics(context.Background(), d, svc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Text(), "selector matches no pods") || !strings.Contains(report.Text(), "not a current selector-matching pod") {
		t.Fatal(report.Text())
	}
	b := &diagnosticBackend{Demo: d, denied: map[string]bool{"pods": true, "endpointslices": true}}
	report, err = ServiceDiagnostics(context.Background(), b, svc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Text(), "Ready backend count is unknown") || !strings.Contains(report.Text(), "Matching pods · unavailable") {
		t.Fatal(report.Text())
	}
	d.objects[0].Object["spec"].(map[string]any)["selector"] = map[string]any{}
	b.queries = nil
	_, err = ServiceDiagnostics(context.Background(), b, d.objects[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range b.queries {
		if q.Resource.Kind == "Pod" {
			t.Fatal("selectorless Service scanned namespace pods")
		}
	}
}
func TestServiceDiagnosticsNamedPortMismatchAndUIDGuard(t *testing.T) {
	d, svc := networkFixture(t)
	unstructured.SetNestedSlice(d.objects[1].Object, []any{map[string]any{"name": "api", "ports": []any{map[string]any{"name": "wrong", "containerPort": int64(8080)}}}}, "spec", "containers")
	report, err := ServiceDiagnostics(context.Background(), d, svc)
	if err != nil || !strings.Contains(report.Text(), "has no container port named http/TCP") {
		t.Fatal(err, report.Text())
	}
	svc = *svc.DeepCopy()
	svc.SetUID("stale")
	if _, err := ServiceDiagnostics(context.Background(), d, svc); err == nil {
		t.Fatal("replaced Service accepted")
	}
}
func TestSchedulingDiagnosticsAndRestrictedRBAC(t *testing.T) {
	d, _ := networkFixture(t)
	pod := *d.objects[1].DeepCopy()
	pod.Object["status"] = map[string]any{"phase": "Pending", "conditions": []any{map[string]any{"type": "PodScheduled", "status": "False", "reason": "Unschedulable", "message": "taint requires toleration"}}}
	pod.Object["spec"].(map[string]any)["volumes"] = []any{map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "data"}}}
	d.objects[1] = pod
	node := reportObject(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}, Spec: corev1.NodeSpec{Taints: []corev1.Taint{{Key: "regulated", Value: "true", Effect: corev1.TaintEffectNoSchedule}}}}, "Node")
	event := unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Event", "metadata": map[string]any{"namespace": "apps", "name": "scheduling"}, "involvedObject": map[string]any{"uid": "pod-1"}, "reason": "FailedScheduling", "message": "0/1 nodes available: untolerated taint"}}
	d.objects = append(d.objects, node, event)
	report, err := SchedulingDiagnostics(context.Background(), d, pod)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PodScheduled=False", "FailedScheduling", "untolerated NoSchedule taint: regulated=true", "data · unavailable", "Resource requests", "not fully evaluated"} {
		if !strings.Contains(report.Text(), want) {
			t.Fatalf("missing %q: %s", want, report.Text())
		}
	}
	b := &diagnosticBackend{Demo: d, denied: map[string]bool{"nodes": true, "events": true, "persistentvolumeclaims": true}}
	report, err = SchedulingDiagnostics(context.Background(), b, pod)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Nodes · unavailable", "Scheduler events · unavailable", "eligibility is unknown", "data · unavailable"} {
		if !strings.Contains(report.Text(), want) {
			t.Fatal(report.Text())
		}
	}
}
func TestDiagnosticTolerationRules(t *testing.T) {
	taint := corev1.Taint{Key: "team", Value: "finance", Effect: corev1.TaintEffectNoSchedule}
	for _, tc := range []struct {
		t    corev1.Toleration
		want bool
	}{{corev1.Toleration{Operator: corev1.TolerationOpExists}, true}, {corev1.Toleration{Key: "team", Value: "finance"}, true}, {corev1.Toleration{Key: "team", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}, false}, {corev1.Toleration{Key: "team", Value: "other"}, false}} {
		if tolerates([]corev1.Toleration{tc.t}, taint) != tc.want {
			t.Fatalf("wrong taint match: %+v", tc)
		}
	}
}
