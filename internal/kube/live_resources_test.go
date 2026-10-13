package kube

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

func TestLiveResources(t *testing.T) {
	path := os.Getenv("KQT_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("disposable Kubernetes fixture only")
	}
	loader, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(loader.Current(), "k3d-kqt-smoke-") {
		t.Fatal("watch qualification requires disposable cluster")
	}
	b, _, _, e := loader.Open(loader.Current())
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	c := b.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, _ := Resolve(Builtins(), "configmaps")
	updates := make(chan EventUpdate, 128)
	done := make(chan error, 1)
	go func() {
		done <- c.FollowResources(ctx, Query{Resource: r, Namespace: "kqt-test", Fields: "metadata.name=kqt-resource-watch"}, func(v EventUpdate) { updates <- v })
	}()
	await := func(match func(EventUpdate) bool) EventUpdate {
		for {
			select {
			case v := <-updates:
				if match(v) {
					return v
				}
			case err := <-done:
				t.Fatalf("watch stopped: %v", err)
			case <-ctx.Done():
				t.Fatal("watch update timed out")
			}
		}
	}
	await(func(v EventUpdate) bool { return v.State == "live" })
	o := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "kqt-resource-watch"}, "data": map[string]interface{}{"value": "first"}}}
	o, e = c.Create(ctx, r, "kqt-test", o)
	if e != nil {
		t.Fatal(e)
	}
	await(func(v EventUpdate) bool { return v.Type == watch.Added && v.Object.GetUID() == o.GetUID() })
	patch := fmt.Sprintf(`{"metadata":{"uid":%q,"resourceVersion":%q},"data":{"value":"second"}}`, o.GetUID(), o.GetResourceVersion())
	if e = c.Patch(ctx, r, "kqt-test", o.GetName(), []byte(patch)); e != nil {
		t.Fatal(e)
	}
	changed := await(func(v EventUpdate) bool {
		return v.Type == watch.Modified && Text(v.Object, "data", "value") == "second"
	})
	if e = c.Delete(ctx, r, "kqt-test", o.GetName(), o.GetUID(), changed.Object.GetResourceVersion()); e != nil {
		t.Fatal(e)
	}
	await(func(v EventUpdate) bool { return v.Type == watch.Deleted && v.Object.GetUID() == o.GetUID() })
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("idle watch did not cancel")
	}
}
