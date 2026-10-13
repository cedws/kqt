package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

func TestWorkloadActionsAndStaleConfirmation(t *testing.T) {
	d := NewDemo()
	r, _ := Resolve(Builtins(), "deployments")
	o, _ := d.Get(context.Background(), r, "default", "web")
	plan, e := PlanAction(r, o, "scale", "3")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ApplyAction(context.Background(), d, plan); e != nil {
		t.Fatal(e)
	}
	fresh, _ := d.Get(context.Background(), r, "default", "web")
	if Number(fresh, "spec", "replicas") != 3 {
		t.Fatal("scale did not apply")
	}
	if _, e = ApplyAction(context.Background(), d, plan); e == nil {
		t.Fatal("stale confirmation applied twice")
	}
	plan, _ = PlanAction(r, fresh, "restart", "")
	if _, e = ApplyAction(context.Background(), d, plan); e != nil {
		t.Fatal(e)
	}
	fresh, _ = d.Get(context.Background(), r, "default", "web")
	if Text(fresh, "spec", "template", "metadata", "annotations", "kubectl.kubernetes.io/restartedAt") == "" {
		t.Fatal("restart annotation missing")
	}
	for _, value := range []string{"-1", "abc", "2147483648"} {
		if _, e = PlanAction(r, fresh, "scale", value); e == nil {
			t.Fatal("invalid replica count accepted")
		}
	}
	if _, e = PlanAction(r, fresh, "run", ""); e == nil {
		t.Fatal("non-CronJob run accepted")
	}
}
func TestCronJobActionsAndJobTemplate(t *testing.T) {
	d := NewDemo()
	r, _ := Resolve(Builtins(), "cronjobs")
	for _, action := range []string{"suspend", "resume", "run"} {
		o, e := d.Get(context.Background(), r, "default", "nightly-report")
		if e != nil {
			t.Fatal(e)
		}
		plan, e := PlanAction(r, o, action, "")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ApplyAction(context.Background(), d, plan); e != nil {
			t.Fatal(e)
		}
		fresh, _ := d.Get(context.Background(), r, "default", "nightly-report")
		if action != "run" {
			value, _, _ := unstructured.NestedBool(fresh.Object, "spec", "suspend")
			if value != (action == "suspend") {
				t.Fatal("suspend/resume failed")
			}
		}
	}
	jobs, _ := Resolve(Builtins(), "jobs")
	items, _ := d.List(context.Background(), Query{Resource: jobs, Namespace: "default"})
	if len(items) != 1 || len(items[0].GetOwnerReferences()) != 1 || items[0].GetAnnotations()["cronjob.kubernetes.io/instantiate"] != "manual" || Text(items[0], "spec", "template", "spec", "restartPolicy") != "Never" {
		t.Fatal("manual Job lost template or owner")
	}
	plan, _ := PlanAction(jobs, items[0], "delete", "")
	if _, e := ApplyAction(context.Background(), d, plan); e != nil {
		t.Fatal(e)
	}
	items, _ = d.List(context.Background(), Query{Resource: jobs, Namespace: "default"})
	if len(items) != 0 {
		t.Fatal("delete did not apply")
	}
}
func TestActionKubernetesHTTPPreconditions(t *testing.T) {
	for _, action := range []string{"scale", "restart", "delete"} {
		t.Run(action, func(t *testing.T) {
			writes := 0
			object := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"apps","uid":"uid","resourceVersion":"7"},"spec":{"replicas":2}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/apis/apps/v1/namespaces/apps/deployments/web" {
					t.Errorf("wrong action scope: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					fmt.Fprint(w, object)
					return
				}
				writes++
				body, _ := io.ReadAll(r.Body)
				var data map[string]interface{}
				json.Unmarshal(body, &data)
				if action == "delete" {
					pre := data["preconditions"].(map[string]interface{})
					if pre["uid"] != "uid" || pre["resourceVersion"] != "7" || data["propagationPolicy"] != "Background" {
						t.Errorf("unsafe delete options: %s", body)
					}
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"Status","status":"Success"}`)
				} else {
					metadata := data["metadata"].(map[string]interface{})
					if metadata["uid"] != "uid" || metadata["resourceVersion"] != "7" || r.Method != http.MethodPatch {
						t.Errorf("unsafe patch: %s", body)
					}
					fmt.Fprint(w, object)
				}
			}))
			defer server.Close()
			c, e := NewClient(&rest.Config{Host: server.URL})
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			r, _ := Resolve(Builtins(), "deployments")
			o, e := c.Get(context.Background(), r, "apps", "web")
			if e != nil {
				t.Fatal(e)
			}
			arg := ""
			if action == "scale" {
				arg = "4"
			}
			plan, _ := PlanAction(r, o, action, arg)
			if _, e = ApplyAction(context.Background(), c, plan); e != nil || writes != 1 {
				t.Fatalf("action failed %d %v", writes, e)
			}
			plan.Object.SetUID("replacement")
			if _, e = ApplyAction(context.Background(), c, plan); e == nil || !strings.Contains(e.Error(), "changed") || writes != 1 {
				t.Fatal("replacement resource mutated")
			}
		})
	}
}

func TestActionsAllowStatusOnlyUpdatesButRejectChangedSpecs(t *testing.T) {
	d := NewDemo()
	r, _ := Resolve(Builtins(), "deployments")
	o, _ := d.Get(context.Background(), r, "default", "web")
	plan, _ := PlanAction(r, o, "scale", "3")
	for i := range d.objects {
		if d.objects[i].GetUID() == o.GetUID() {
			d.objects[i].SetResourceVersion("2")
			unstructured.SetNestedField(d.objects[i].Object, int64(11), "status", "readyReplicas")
		}
	}
	if _, e := ApplyAction(context.Background(), d, plan); e != nil {
		t.Fatalf("status update blocked action: %v", e)
	}
	if _, e := ApplyAction(context.Background(), d, plan); e == nil {
		t.Fatal("changed workload spec accepted")
	}
}
