package kube

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type unscopedEvents struct {
	*Demo
	events []unstructured.Unstructured
	query  Query
}

func (b *unscopedEvents) List(_ context.Context, query Query) ([]unstructured.Unstructured, error) {
	b.query = query
	return b.events, nil
}

func TestEventsForResourceFiltersSortsAndPreservesBackendRows(t *testing.T) {
	makeEvent := func(name, uid, stamp string) unstructured.Unstructured {
		return unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": name}, "involvedObject": map[string]interface{}{"uid": uid}, "lastTimestamp": stamp}}
	}
	backend := &unscopedEvents{Demo: NewDemo(), events: []unstructured.Unstructured{
		makeEvent("old", "node-uid", "2026-10-13T12:00:00Z"),
		makeEvent("unrelated", "pod-uid", "2026-10-13T14:00:00Z"),
		makeEvent("new", "node-uid", "2026-10-13T13:00:00Z"),
	}}
	target := unstructured.Unstructured{Object: map[string]interface{}{"kind": "Node", "metadata": map[string]interface{}{"name": "worker", "uid": "node-uid"}}}
	events, err := EventsForResource(context.Background(), backend, target)
	if err != nil || len(events) != 2 || events[0].GetName() != "new" || events[1].GetName() != "old" {
		t.Fatalf("wrong scoped event order: %v %v", events, err)
	}
	if backend.query.Namespace != "" || backend.query.Fields != "involvedObject.uid=node-uid" || backend.query.Resource.Kind != "Event" {
		t.Fatal("cluster-scoped events did not use the target UID")
	}
	if len(backend.events) != 3 || backend.events[0].GetName() != "old" || backend.events[1].GetName() != "unrelated" {
		t.Fatal("filtering/sorting modified backend-owned rows")
	}
}
