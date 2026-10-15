package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func rolloutFixture(t *testing.T) (*Demo, RolloutHistory) {
	t.Helper()
	b := NewDemo()
	r, _ := Resolve(Builtins(), "deployments")
	target, err := b.Get(context.Background(), r, "default", "web")
	if err != nil {
		t.Fatal(err)
	}
	history, err := ReadRolloutHistory(context.Background(), b, Builtins(), target)
	if err != nil {
		t.Fatal(err)
	}
	return b, history
}

func TestRolloutHistorySortsRevisionsAndFencesOwners(t *testing.T) {
	b, history := rolloutFixture(t)
	if len(history.Revisions) != 2 || history.Revisions[0].Number != 2 || history.Revisions[1].Number != 1 || history.Revisions[1].Images != "web=nginx:1.27" {
		t.Fatalf("bad history: %#v", history.Revisions)
	}
	other := *history.Revisions[0].Source.DeepCopy()
	other.SetName("unrelated")
	other.SetUID("unrelated")
	refs := other.GetOwnerReferences()
	refs[0].UID = "another-deployment"
	other.SetOwnerReferences(refs)
	b.objects = append(b.objects, other)
	got, err := ReadRolloutHistory(context.Background(), b, Builtins(), history.Deployment)
	if err != nil || len(got.Revisions) != 2 {
		t.Fatal("history included another Deployment's revision", err)
	}
	replaced := *history.Deployment.DeepCopy()
	replaced.SetUID("replaced")
	if _, err = ReadRolloutHistory(context.Background(), b, Builtins(), replaced); err == nil {
		t.Fatal("read replacement as selected Deployment")
	}
	if _, err = PlanRollback(history, 999); err == nil {
		t.Fatal("accepted missing revision")
	}
}

func TestRollbackRestoresExactTemplateAndPreservesCurrentReplicaCount(t *testing.T) {
	b, history := rolloutFixture(t)
	plan, err := PlanRollback(history, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range b.objects {
		if b.objects[i].GetKind() == "Deployment" && b.objects[i].GetUID() == history.Deployment.GetUID() {
			_ = unstructured.SetNestedField(b.objects[i].Object, int64(3), "spec", "replicas")
			b.objects[i].SetResourceVersion("20") // Unrelated scaling/status changes are retained.
			template, _ := deploymentTemplate(b.objects[i])
			template["metadata"] = map[string]interface{}{"annotations": map[string]interface{}{"only-in-new-template": "remove me"}}
			_ = unstructured.SetNestedMap(b.objects[i].Object, template, "spec", "template")
			plan.Object = *b.objects[i].DeepCopy()
		}
	}
	message, err := ApplyAction(context.Background(), b, plan)
	if err != nil || !strings.Contains(message, "revision 1 requested") {
		t.Fatal(message, err)
	}
	fresh, _ := b.Get(context.Background(), history.Resource, "default", "web")
	template, _ := deploymentTemplate(fresh)
	if !reflect.DeepEqual(template, plan.Rollback.Template) || Number(fresh, "spec", "replicas") != 3 {
		t.Fatal("rollback merged old/new templates or changed replicas")
	}
}

func TestRollbackRejectsChangedDeploymentRevisionAndPausedWorkload(t *testing.T) {
	for _, failure := range []string{"deployment UID", "deployment template", "revision UID", "revision number", "revision template", "owner", "paused", "deleted revision"} {
		t.Run(failure, func(t *testing.T) {
			b, history := rolloutFixture(t)
			plan, _ := PlanRollback(history, 1)
			for i := range b.objects {
				o := &b.objects[i]
				if o.GetKind() == "Deployment" && o.GetUID() == plan.Object.GetUID() {
					switch failure {
					case "deployment UID":
						o.SetUID("replacement")
					case "deployment template":
						_ = unstructured.SetNestedField(o.Object, "another", "spec", "template", "spec", "serviceAccountName")
					case "paused":
						_ = unstructured.SetNestedField(o.Object, true, "spec", "paused")
					}
				}
				if o.GetUID() == plan.Rollback.Source.GetUID() {
					switch failure {
					case "revision UID":
						o.SetUID("replacement-revision")
					case "revision number":
						a := o.GetAnnotations()
						a[revisionAnnotation] = "99"
						o.SetAnnotations(a)
					case "revision template":
						_ = unstructured.SetNestedField(o.Object, "changed", "spec", "template", "spec", "serviceAccountName")
					case "owner":
						refs := o.GetOwnerReferences()
						refs[0].UID = types.UID("replacement-owner")
						o.SetOwnerReferences(refs)
					case "deleted revision":
						o.SetName("not-here")
					}
				}
			}
			before, _ := json.Marshal(b.objects)
			if _, err := ApplyAction(context.Background(), b, plan); err == nil {
				t.Fatal("unsafe rollback succeeded")
			}
			after, _ := json.Marshal(b.objects)
			if string(before) != string(after) {
				t.Fatal("failed rollback wrote data")
			}
		})
	}
}

type deniedRollouts struct{ *Demo }

func (d deniedRollouts) List(context.Context, Query) ([]unstructured.Unstructured, error) {
	return nil, errors.New("forbidden")
}
func TestRolloutHistoryDoesNotTurnPermissionsFailureIntoEmptyHistory(t *testing.T) {
	b, history := rolloutFixture(t)
	if _, err := ReadRolloutHistory(context.Background(), deniedRollouts{b}, Builtins(), history.Deployment); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatal("permission failure disappeared")
	}
	current, _ := deploymentTemplate(history.Deployment)
	changes := TemplateChanges(current, history.Revisions[1].Template)
	if len(changes) != 1 || changes[0].Path != "/spec/containers/0/image" || changes[0].Before != `"nginx:1.28"` || changes[0].After != `"nginx:1.27"` {
		t.Fatalf("bad template comparison: %#v", changes)
	}
}

func TestRollbackKubernetesHTTPUsesVersionedPatchInSelectedNamespace(t *testing.T) {
	_, history := rolloutFixture(t)
	plan, _ := PlanRollback(history, 1)
	deployment := *history.Deployment.DeepCopy()
	deployment.SetResourceVersion("17")
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/default/replicasets/web-previous":
			if r.Method != http.MethodGet {
				t.Errorf("revision was mutated: %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(plan.Rollback.Source.Object)
		case "/apis/apps/v1/namespaces/default/deployments/web":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(deployment.Object)
				return
			}
			writes++
			body, _ := io.ReadAll(r.Body)
			var patch map[string]interface{}
			if err := json.Unmarshal(body, &patch); err != nil {
				t.Error(err)
				return
			}
			metadata, _ := patch["metadata"].(map[string]interface{})
			if r.Method != http.MethodPatch || r.Header.Get("Content-Type") != "application/merge-patch+json" || metadata["uid"] != string(deployment.GetUID()) || metadata["resourceVersion"] != "17" {
				t.Errorf("unsafe rollback request: %s %s %s", r.Method, r.Header.Get("Content-Type"), body)
			}
			spec, _ := patch["spec"].(map[string]interface{})
			if _, found := spec["replicas"]; found {
				t.Error("rollback overwrote replicas")
			}
			if !strings.Contains(string(body), "nginx:1.27") {
				t.Error("rollback omitted the selected image")
			}
			_ = json.NewEncoder(w).Encode(deployment.Object)
		default:
			t.Errorf("unexpected rollout endpoint: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	client, err := NewClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err = ApplyAction(context.Background(), client, plan); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("rollback writes: %d", writes)
	}
}
