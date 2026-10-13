package kube

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestLiveActions(t *testing.T) {
	path := os.Getenv("KQT_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("disposable Kubernetes fixture only")
	}
	loader, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(loader.Current(), "k3d-kqt-smoke-") {
		t.Fatal("action qualification requires a disposable cluster")
	}
	b, _, _, e := loader.Open(loader.Current())
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	c := b.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, _ := Resolve(Builtins(), "deployments")
	template, e := c.Get(ctx, r, "kqt-test", "kqt-deployment")
	if e != nil {
		t.Fatal(e)
	}
	spec, _, _ := unstructured.NestedMap(template.Object, "spec")
	unstructured.SetNestedField(spec, int64(0), "replicas")
	// A distinct selector keeps this qualification isolated from other fixtures.
	unstructured.SetNestedField(spec, "kqt-actions", "selector", "matchLabels", "app")
	unstructured.SetNestedField(spec, "kqt-actions", "template", "metadata", "labels", "app")
	object := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]interface{}{"name": "kqt-actions"}, "spec": spec}}
	current, e := c.Create(ctx, r, "kqt-test", object)
	if e != nil {
		t.Fatal(e)
	}
	for _, step := range []struct{ action, arg string }{{"scale", "1"}, {"restart", ""}, {"delete", ""}} {
		current, e = c.Get(ctx, r, "kqt-test", current.GetName())
		if e != nil {
			t.Fatal(e)
		}
		plan, e := PlanAction(r, current, step.action, step.arg)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ApplyAction(ctx, c, plan); e != nil {
			t.Fatal(e)
		}
		if step.action == "scale" {
			updated, e := c.Get(ctx, r, "kqt-test", current.GetName())
			if e != nil || Number(updated, "spec", "replicas") != 1 {
				t.Fatal("live scale failed", e)
			}
		}
		if step.action == "restart" {
			updated, e := c.Get(ctx, r, "kqt-test", current.GetName())
			if e != nil || Text(updated, "spec", "template", "metadata", "annotations", "kubectl.kubernetes.io/restartedAt") == "" {
				t.Fatal("live restart failed", e)
			}
		}
	}
	cronResource, _ := Resolve(Builtins(), "cronjobs")
	cron := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "batch/v1", "kind": "CronJob", "metadata": map[string]interface{}{"name": "kqt-actions-cron"}, "spec": map[string]interface{}{"schedule": "0 0 1 1 *", "suspend": true, "jobTemplate": map[string]interface{}{"spec": map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{"restartPolicy": "Never", "containers": []interface{}{map[string]interface{}{"name": "fixture", "image": "busybox:1.37", "command": []interface{}{"sh", "-c", "echo kqt-action-job"}}}}}}}}}}
	cron, e = c.Create(ctx, cronResource, "kqt-test", cron)
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"resume", "suspend", "run", "delete"} {
		cron, e = c.Get(ctx, cronResource, "kqt-test", cron.GetName())
		if e != nil {
			t.Fatal(e)
		}
		plan, e := PlanAction(cronResource, cron, action, "")
		if e != nil {
			t.Fatal(e)
		}
		message, e := ApplyAction(ctx, c, plan)
		if e != nil {
			t.Fatal(e)
		}
		if action == "run" && !strings.HasPrefix(message, "Created Job kqt-actions-cron-manual-") {
			t.Fatal("live run did not create Job", message)
		}
	}
}
