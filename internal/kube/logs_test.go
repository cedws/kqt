package kube

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type measuredLogs struct {
	*Demo
	active, maximum, calls atomic.Int32
	fail                   string
	large                  bool
}

func (b *measuredLogs) Logs(ctx context.Context, ns, pod, container string, previous bool) (string, error) {
	n := b.active.Add(1)
	defer b.active.Add(-1)
	for old := b.maximum.Load(); n > old; old = b.maximum.Load() {
		if b.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	b.calls.Add(1)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(time.Millisecond):
	}
	if pod == b.fail {
		return "", errors.New("forbidden pod logs")
	}
	if b.large {
		return strings.Repeat("long line\n", 200000), nil
	}
	return b.Demo.Logs(ctx, ns, pod, container, previous)
}
func TestWorkloadLogsSelectAllPodsAndLimitConcurrency(t *testing.T) {
	b := &measuredLogs{Demo: NewDemo(), fail: "web-7c9f6d8b5-0004"}
	deploy, _ := Resolve(Builtins(), "deploy")
	rows, _ := b.List(context.Background(), Query{Resource: deploy, Namespace: "default"})
	out, err := LogSnapshot(context.Background(), b, rows[0], "web", false)
	if err != nil {
		t.Fatal(err)
	}
	if b.calls.Load() != 12 || b.maximum.Load() > 4 {
		t.Fatalf("calls=%d concurrency=%d", b.calls.Load(), b.maximum.Load())
	}
	for _, name := range []string{"web-7c9f6d8b5-0000", "web-7c9f6d8b5-0011"} {
		if !strings.Contains(out, name) {
			t.Fatalf("pod missing: %s", name)
		}
	}
	if strings.Contains(out, "gateway") || !strings.Contains(out, "forbidden pod logs") {
		t.Fatal("namespace scoping or per-pod error reporting failed")
	}
	sts, _ := Resolve(Builtins(), "sts")
	rows, _ = b.List(context.Background(), Query{Resource: sts, Namespace: "platform"})
	out, err = LogSnapshot(context.Background(), b, rows[0], "redis", false)
	if err != nil || !strings.Contains(out, "redis-0") || !strings.Contains(out, "redis-1") {
		t.Fatalf("statefulset logs: %v", err)
	}
}
func TestAllContainerLogsCancellationAndOutputLimit(t *testing.T) {
	b := &measuredLogs{Demo: NewDemo()}
	pod, _ := Resolve(Builtins(), "po")
	rows, _ := b.List(context.Background(), Query{Resource: pod, Namespace: "default"})
	out, err := LogSnapshot(context.Background(), b, rows[0], "*", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{":web]", ":sidecar]", ":setup]"} {
		if !strings.Contains(out, name) {
			t.Fatalf("container missing: %s", name)
		}
	}
	deploy, _ := Resolve(Builtins(), "deploy")
	rows, _ = b.List(context.Background(), Query{Resource: deploy, Namespace: "default"})
	b.large = true
	out, err = LogSnapshot(context.Background(), b, rows[0], "web", false)
	if err != nil || len(out) > 4*1024*1024+128 || !strings.Contains(out, "truncated") {
		t.Fatalf("combined limit failed: %d %v", len(out), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = LogSnapshot(ctx, b, rows[0], "web", false); err == nil {
		t.Fatal("cancelled log request succeeded")
	}
}
