package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func debugTestProfile() DebugProfile {
	yes, no := true, false
	return DebugProfile{Image: "123456789012.dkr.ecr.eu-west-2.amazonaws.com/tools:approved", Command: []string{"/bin/sh"}, PullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{RunAsNonRoot: &yes, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}}
}
func debugTestPod(t *testing.T, d *Demo) unstructured.Unstructured {
	t.Helper()
	r, _ := Resolve(Builtins(), "pods")
	pod, err := d.Get(context.Background(), r, "default", "web-7c9f6d8b5-0000")
	if err != nil {
		t.Fatal(err)
	}
	return pod
}

type debugBackend struct {
	*Demo
	deny, unknown                    bool
	denyVerb, denySub                string
	dryRuns, creates                 int
	admit                            func(*unstructured.Unstructured, bool)
	conflictDryRun, conflictCreation bool
	requests                         []AccessRequest
}

func (b *debugBackend) ReviewAccess(ctx context.Context, q AccessRequest) (AccessDecision, error) {
	b.requests = append(b.requests, q)
	if b.unknown {
		return AccessDecision{}, apierrors.NewForbidden(schema.GroupResource{Group: "authorization.k8s.io", Resource: "selfsubjectaccessreviews"}, "", fmt.Errorf("reviews restricted"))
	}
	return AccessDecision{Allowed: !b.deny || q.Verb != b.denyVerb || q.Subresource != b.denySub, Reason: "fixture RBAC"}, nil
}
func (b *debugBackend) PatchEphemeral(ctx context.Context, pod unstructured.Unstructured, container corev1.EphemeralContainer, dryRun bool) (unstructured.Unstructured, error) {
	if dryRun {
		b.dryRuns++
		if b.conflictDryRun {
			b.conflictDryRun = false
			for i := range b.objects {
				if b.objects[i].GetUID() == pod.GetUID() {
					b.objects[i].SetResourceVersion(pod.GetResourceVersion() + "1")
				}
			}
			return unstructured.Unstructured{}, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, pod.GetName(), fmt.Errorf("status update"))
		}
	} else {
		b.creates++
		if b.conflictCreation {
			return unstructured.Unstructured{}, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, pod.GetName(), fmt.Errorf("persistent conflict"))
		}
	}
	result, err := b.Demo.PatchEphemeral(ctx, pod, container, dryRun)
	if b.admit != nil {
		b.admit(&result, dryRun)
	}
	return result, err
}

func TestDebugRetriesOnlySafeDryRunsAfterStatusChurn(t *testing.T) {
	b := &debugBackend{Demo: NewDemo(), conflictDryRun: true}
	plan, err := PrepareDebug(context.Background(), b, debugTestPod(t, b.Demo), debugTestProfile(), "", "")
	if err != nil || b.dryRuns != 2 || b.creates != 0 {
		t.Fatalf("safe dry-run did not retry after status change: %v", err)
	}
	b.conflictDryRun = true
	b.conflictCreation = true
	if _, err = ApplyDebug(context.Background(), b, plan); err == nil || b.creates != 1 {
		t.Fatalf("persistent patch was retried: creates %d error %v", b.creates, err)
	}
}

func TestDebugScopedPreflightAndDryRunNeverCreate(t *testing.T) {
	b := &debugBackend{Demo: NewDemo()}
	pod := debugTestPod(t, b.Demo)
	plan, err := PrepareDebug(context.Background(), b, pod, debugTestProfile(), "web", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.creates != 0 || b.dryRuns != 1 || len(DebugContainers(debugTestPod(t, b.Demo))) != 0 {
		t.Fatal("preview persisted a debugger")
	}
	if len(b.requests) != 4 {
		t.Fatalf("unexpected access checks: %#v", b.requests)
	}
	for _, q := range b.requests {
		if q.Namespace != pod.GetNamespace() || q.Name != pod.GetName() || q.Resource.GVR.Resource != "pods" || q.Subresource == "exec" {
			t.Fatalf("overbroad access check %#v", q)
		}
	}
	if plan.Container.Image != debugTestProfile().Image || plan.Container.TargetContainerName != "web" || !plan.Container.TTY || !plan.Container.Stdin {
		t.Fatalf("wrong debugger: %#v", plan.Container)
	}
	created, err := ApplyDebug(context.Background(), b, plan)
	if err != nil || b.creates != 1 || b.dryRuns != 2 {
		t.Fatalf("apply: %v, creates %d, dryrun %d", err, b.creates, b.dryRuns)
	}
	if _, err = WaitDebug(context.Background(), b, created, plan.Container.Name); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(debugSpec(created), debugSpec(pod)) {
		t.Fatal("debug changed the pod's scheduling or workload spec")
	}
	if _, err = ApplyDebug(context.Background(), b, plan); err == nil || b.creates != 1 {
		t.Fatal("creation retried on existing container")
	}
}

func TestDebugDenialsNeverCreateAndAttachDoesNotNeedPatch(t *testing.T) {
	for _, check := range []struct{ verb, sub string }{{"get", ""}, {"patch", "ephemeralcontainers"}, {"create", "attach"}} {
		t.Run(check.verb+check.sub, func(t *testing.T) {
			b := &debugBackend{Demo: NewDemo(), deny: true, denyVerb: check.verb, denySub: check.sub}
			_, err := PrepareDebug(context.Background(), b, debugTestPod(t, b.Demo), debugTestProfile(), "", "")
			if err == nil || b.creates != 0 || b.dryRuns != 0 {
				t.Fatalf("RBAC denied but continued: %v", err)
			}
		})
	}
	b := &debugBackend{Demo: NewDemo(), deny: true, denyVerb: "get", denySub: "attach"}
	plan, err := PrepareDebug(context.Background(), b, debugTestPod(t, b.Demo), debugTestProfile(), "", "")
	if err != nil || !plan.SPDYOnly {
		t.Fatalf("POST-only attach not supported: %v", err)
	}
	created, err := ApplyDebug(context.Background(), b, plan)
	if err != nil {
		t.Fatal(err)
	}
	b.denyVerb, b.denySub = "patch", "ephemeralcontainers"
	b.requests = nil
	reattach, err := PrepareDebug(context.Background(), b, created, DebugProfile{}, "", plan.Container.Name)
	if err != nil || !reattach.AttachOnly {
		t.Fatalf("reattach requires creation rights/profile: %v", err)
	}
	for _, q := range b.requests {
		if q.Subresource == "ephemeralcontainers" {
			t.Fatal("reattach checked creation permission")
		}
	}
	before := b.creates
	if _, err = ApplyDebug(context.Background(), b, reattach); err != nil || b.creates != before {
		t.Fatal("reattach created another container")
	}
	b.unknown = true
	unknown, err := PrepareDebug(context.Background(), b, created, DebugProfile{}, "", plan.Container.Name)
	if err != nil || len(unknown.Warnings) == 0 || !unknown.SPDYOnly {
		t.Fatalf("unavailable reviews: %#v %v", unknown, err)
	}
}

func TestDebugRejectsReplacementChangedSpecAndAdmissionDrift(t *testing.T) {
	for _, change := range []string{"replacement", "spec", "admission"} {
		t.Run(change, func(t *testing.T) {
			b := &debugBackend{Demo: NewDemo()}
			pod := debugTestPod(t, b.Demo)
			plan, err := PrepareDebug(context.Background(), b, pod, debugTestProfile(), "", "")
			if err != nil {
				t.Fatal(err)
			}
			for i := range b.objects {
				if b.objects[i].GetUID() == pod.GetUID() {
					switch change {
					case "replacement":
						b.objects[i].SetUID(types.UID("new-pod"))
					case "spec":
						_ = unstructured.SetNestedField(b.objects[i].Object, "different-node", "spec", "nodeName")
					}
				}
			}
			if change == "admission" {
				b.admit = func(pod *unstructured.Unstructured, _ bool) {
					entries := Slice(*pod, "spec", "ephemeralContainers")
					entries[len(entries)-1].(map[string]interface{})["image"] = "registry.invalid/changed:tag"
					_ = unstructured.SetNestedSlice(pod.Object, entries, "spec", "ephemeralContainers")
				}
			}
			if _, err = ApplyDebug(context.Background(), b, plan); err == nil || b.creates != 0 {
				t.Fatalf("stale preview accepted: %v", err)
			}
		})
	}
}

func TestDebugRejectsUnsupportedPodsAndReportsStartFailures(t *testing.T) {
	for _, change := range []string{"unscheduled", "completed", "mirror", "windows"} {
		t.Run(change, func(t *testing.T) {
			d := NewDemo()
			pod := debugTestPod(t, d)
			for i := range d.objects {
				if d.objects[i].GetUID() == pod.GetUID() {
					switch change {
					case "unscheduled":
						unstructured.RemoveNestedField(d.objects[i].Object, "spec", "nodeName")
					case "completed":
						_ = unstructured.SetNestedField(d.objects[i].Object, "Succeeded", "status", "phase")
					case "mirror":
						d.objects[i].SetAnnotations(map[string]string{"kubernetes.io/config.mirror": "hash"})
					case "windows":
						_ = unstructured.SetNestedField(d.objects[i].Object, "windows", "spec", "os", "name")
					}
				}
			}
			if _, err := PrepareDebug(context.Background(), d, pod, debugTestProfile(), "", ""); err == nil {
				t.Fatal("unsupported pod accepted")
			}
		})
	}
	for _, reason := range []string{"ImagePullBackOff", "CreateContainerConfigError", "ContainerCreating", "terminated"} {
		t.Run(reason, func(t *testing.T) {
			d := NewDemo()
			pod := debugTestPod(t, d)
			for i := range d.objects {
				if d.objects[i].GetUID() == pod.GetUID() {
					state := map[string]interface{}{"waiting": map[string]interface{}{"reason": reason, "message": "fixture failure"}}
					if reason == "terminated" {
						state = map[string]interface{}{"terminated": map[string]interface{}{"reason": "Error", "exitCode": int64(1)}}
					}
					_ = unstructured.SetNestedSlice(d.objects[i].Object, []interface{}{map[string]interface{}{"name": "debug", "state": state}}, "status", "ephemeralContainerStatuses")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, err := WaitDebug(ctx, d, pod, "debug")
			if err == nil || !strings.Contains(err.Error(), "debug") {
				t.Fatalf("missing useful failure: %v", err)
			}
		})
	}
}

func TestDebugHTTPOnlyPatchesEphemeralSubresourceWithIdentityTests(t *testing.T) {
	pod := debugTestPod(t, NewDemo())
	_ = unstructured.SetNestedSlice(pod.Object, []interface{}{map[string]interface{}{"key": "regulated", "operator": "Exists", "effect": "NoSchedule"}}, "spec", "tolerations")
	_ = unstructured.SetNestedSlice(pod.Object, []interface{}{map[string]interface{}{"name": "ecr-pull"}}, "spec", "imagePullSecrets")
	original := pod.DeepCopy()
	persists := 0
	dryRuns := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/default/pods/"+pod.GetName():
			_ = json.NewEncoder(w).Encode(pod.Object)
		case r.Method == http.MethodPost && r.URL.Path == "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			var request map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&request)
			attrs := request["spec"].(map[string]interface{})["resourceAttributes"].(map[string]interface{})
			if attrs["name"] != pod.GetName() || attrs["namespace"] != "default" || attrs["resource"] != "pods" {
				t.Errorf("overbroad review: %v", attrs)
			}
			fmt.Fprint(w, `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/namespaces/default/pods/"+pod.GetName()+"/ephemeralcontainers":
			data, _ := io.ReadAll(r.Body)
			if r.Header.Get("Content-Type") != "application/json-patch+json" || r.URL.Query().Get("fieldValidation") != "Strict" {
				t.Error("missing strict JSON patch")
			}
			var ops []map[string]interface{}
			_ = json.Unmarshal(data, &ops)
			if len(ops) != 3 || ops[0]["path"] != "/metadata/uid" || ops[1]["path"] != "/metadata/resourceVersion" || ops[2]["path"] != "/spec/ephemeralContainers" {
				t.Errorf("unsafe patch: %s", data)
			}
			patch, err := jsonpatch.DecodePatch(data)
			if err != nil {
				t.Fatal(err)
			}
			base, _ := json.Marshal(pod.Object)
			patched, err := patch.Apply(base)
			if err != nil {
				t.Fatal(err)
			}
			if r.URL.Query().Get("dryRun") == "All" {
				dryRuns++
			} else {
				persists++
				_ = pod.UnmarshalJSON(patched)
				pod.SetResourceVersion("2")
				patched, _ = json.Marshal(pod.Object)
			}
			_, _ = w.Write(patched)
		default:
			t.Errorf("unexpected access: %s %s", r.Method, r.URL)
			http.Error(w, "forbidden endpoint", 403)
		}
	}))
	defer server.Close()
	client, err := NewClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	plan, err := PrepareDebug(context.Background(), client, pod, debugTestProfile(), "web", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyDebug(context.Background(), client, plan); err != nil {
		t.Fatal(err)
	}
	if persists != 1 || dryRuns != 2 || !reflect.DeepEqual(debugSpec(pod), debugSpec(*original)) {
		t.Fatalf("unsafe apply: persistent %d, dryrun %d", persists, dryRuns)
	}
}

func TestDebugDemoDisconnectAndReattach(t *testing.T) {
	d := NewDemo()
	pod := debugTestPod(t, d)
	plan, err := PrepareDebug(context.Background(), d, pod, debugTestProfile(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	pod, err = ApplyDebug(context.Background(), d, plan)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = d.Attach(context.Background(), pod, plan.Container.Name, false, remotecommand.StreamOptions{Stdin: strings.NewReader(""), Stdout: &output}); err != nil {
		t.Fatal(err)
	}
	if !sameRunningDebugger(debugTestPod(t, d), plan.Container.Name) {
		t.Fatal("disconnect stopped the container")
	}
	if err = d.Attach(context.Background(), pod, plan.Container.Name, false, remotecommand.StreamOptions{Stdin: strings.NewReader("exit\n"), Stdout: &output}); err != nil {
		t.Fatal(err)
	}
	if sameRunningDebugger(debugTestPod(t, d), plan.Container.Name) {
		t.Fatal("exit did not stop demo container")
	}
}

func TestDebugAttachUsesAttachAndNeverRetriesForbidden(t *testing.T) {
	for _, spdyOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(spdyOnly), func(t *testing.T) {
			d := NewDemo()
			pod := debugTestPod(t, d)
			plan, err := PrepareDebug(context.Background(), d, pod, debugTestProfile(), "", "")
			if err != nil {
				t.Fatal(err)
			}
			pod, err = ApplyDebug(context.Background(), d, plan)
			if err != nil {
				t.Fatal(err)
			}
			methods := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/attach") {
					methods = append(methods, r.Method)
					q := r.URL.Query()
					if q.Get("container") != plan.Container.Name || q.Get("command") != "" || q.Get("tty") != "true" || q.Get("stdout") != "true" {
						t.Errorf("wrong attach options: %s", r.URL)
					}
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"Status","reason":"Forbidden","status":"Failure","code":403,"message":"attach denied"}`)
					return
				}
				if r.Method != http.MethodGet || strings.Contains(r.URL.Path, "/exec") {
					t.Errorf("unexpected access %s %s", r.Method, r.URL)
				}
				_ = json.NewEncoder(w).Encode(pod.Object)
			}))
			defer server.Close()
			client, err := NewClient(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			err = client.Attach(context.Background(), pod, plan.Container.Name, spdyOnly, remotecommand.StreamOptions{Stdout: io.Discard, Tty: true})
			wanted := http.MethodGet
			if spdyOnly {
				wanted = http.MethodPost
			}
			if err == nil || len(methods) != 1 || methods[0] != wanted {
				t.Fatalf("unexpected fallback after denial: %v, %v", methods, err)
			}
		})
	}
}
