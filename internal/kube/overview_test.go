package kube

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type overviewLimited struct{ Backend }

func (b overviewLimited) List(ctx context.Context, q Query) ([]unstructured.Unstructured, error) {
	if q.Resource.Kind == "Node" {
		return nil, fmt.Errorf("forbidden node reads")
	}
	return b.Backend.List(ctx, q)
}
func TestOverviewScopeHealthCapacityAndPartialPermissions(t *testing.T) {
	d := NewDemo()
	snapshot := ReadOverview(context.Background(), d, "default")
	if _, total := snapshot.Health("pods"); total != 12 {
		t.Fatal("overview crossed namespace")
	}
	if len(snapshot.Warnings()) != 1 || len(snapshot.Problems()) < 2 {
		t.Fatal("unhealthy resources or warnings missing")
	}
	all := ReadOverview(context.Background(), d, "")
	if _, total := all.Health("pods"); total != 26 {
		t.Fatal("all-namespace overview omitted pods")
	}
	limited := ReadOverview(context.Background(), overviewLimited{d}, "default")
	if !strings.Contains(limited.Summary("nodes"), "Unavailable") || limited.Errors["Pod metrics"] == "" || len(limited.Objects["pods"]) != 12 {
		t.Fatal("partial access became zero or hid usable sections")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := ReadOverview(cancelled, d, "default")
	if stopped.Errors["refresh"] == "" || !strings.Contains(stopped.Summary("nodes"), "Unavailable") {
		t.Fatal("cancelled overview became empty healthy data")
	}
}
func TestOverviewPressureRolloutAndExactQuantities(t *testing.T) {
	node := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "Node", "metadata": map[string]interface{}{"name": "worker", "uid": "node"}, "status": map[string]interface{}{"allocatable": map[string]interface{}{"cpu": "2500m", "memory": "1Gi"}, "conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}, map[string]interface{}{"type": "MemoryPressure", "status": "True"}}}}}
	snapshot := Overview{Objects: map[string][]unstructured.Unstructured{"nodes": {node}}, NodeUsage: map[string]Usage{UsageKey(node): {CPUMilli: 125, MemoryBytes: 1024 * 1024}}}
	cpu, memory, reported := snapshot.Capacity()
	if cpu != 2500 || memory != 1024*1024*1024 || reported != 1 {
		t.Fatal("capacity lost numeric accuracy")
	}
	if len(snapshot.Problems()) != 1 || WorkloadStatus(node) != "MemoryPressure" {
		t.Fatal("node pressure was reported healthy")
	}
	totals, count := snapshot.UsageTotals(true)
	if count != 1 || totals.CPUMilli != 125 {
		t.Fatal("usage aggregation failed")
	}
	d := NewDemo()
	r, _ := Resolve(Builtins(), "deployments")
	deployment, _ := d.Get(context.Background(), r, "default", "web")
	unstructured.SetNestedField(deployment.Object, int64(0), "status", "updatedReplicas")
	if WorkloadStatus(deployment) != "Progressing" {
		t.Fatal("old ready replicas hid incomplete rollout")
	}
}
