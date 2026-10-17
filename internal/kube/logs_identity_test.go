package kube

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type snapshotIdentityBackend struct {
	*Demo
	replacedKind, replacedName string
	mu                         sync.Mutex
	reads                      map[string]int
}

func (b *snapshotIdentityBackend) Get(ctx context.Context, r Resource, ns, name string) (unstructured.Unstructured, error) {
	o, err := b.Demo.Get(ctx, r, ns, name)
	if r.Kind == b.replacedKind && (b.replacedName == "" || name == b.replacedName) {
		o.SetUID("replacement")
	}
	return o, err
}

func (b *snapshotIdentityBackend) Logs(ctx context.Context, ns, pod, container string, previous bool, options ...LogOptions) (string, error) {
	b.mu.Lock()
	b.reads[pod]++
	b.mu.Unlock()
	return b.Demo.Logs(ctx, ns, pod, container, previous, options...)
}

func TestLogSnapshotsRejectReplacedSelectedResources(t *testing.T) {
	for _, kind := range []string{"Pod", "Deployment", "StatefulSet"} {
		for _, container := range []string{"web", "*"} {
			for _, previous := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/previous=%t", kind, container, previous), func(t *testing.T) {
					b := &snapshotIdentityBackend{Demo: NewDemo(), replacedKind: kind, reads: map[string]int{}}
					r, _ := Resolve(Builtins(), strings.ToLower(kind))
					objects, _ := b.List(context.Background(), Query{Resource: r})
					_, err := LogSnapshot(context.Background(), b, objects[0], container, previous)
					if err == nil || !strings.Contains(err.Error(), "replaced") || len(b.reads) != 0 {
						t.Fatalf("read replacement logs: err=%v reads=%v", err, b.reads)
					}
				})
			}
		}
	}
}

func TestLogSnapshotChecksEachSourceAndKeepsHealthyPods(t *testing.T) {
	for _, previous := range []bool{false, true} {
		b := &snapshotIdentityBackend{Demo: NewDemo(), replacedKind: "Pod", replacedName: "web-7c9f6d8b5-0000", reads: map[string]int{}}
		r, _ := Resolve(Builtins(), "deployments")
		workload, _ := b.Get(context.Background(), r, "default", "web")
		out, err := LogSnapshot(context.Background(), b, workload, "*", previous)
		if err != nil || !strings.Contains(out, "was replaced") || b.reads[b.replacedName] != 0 || len(b.reads) != 11 {
			t.Fatalf("source validation failed: err=%v reads=%v", err, b.reads)
		}
	}
}

func TestPreviousLogSnapshotAllowsCompletedPodWithSameUID(t *testing.T) {
	b := NewDemo()
	r, _ := Resolve(Builtins(), "pods")
	pod, _ := b.Get(context.Background(), r, "default", "web-7c9f6d8b5-0000")
	_ = unstructured.SetNestedField(pod.Object, "Succeeded", "status", "phase")
	b.mu.Lock()
	for i := range b.objects {
		if b.objects[i].GetUID() == pod.GetUID() {
			b.objects[i] = *pod.DeepCopy()
		}
	}
	b.mu.Unlock()
	if _, err := LogSnapshot(context.Background(), b, pod, "web", true); err != nil {
		t.Fatalf("completed pod rejected: %v", err)
	}
}
