package kube

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeLogLinesRemovesCompleteEscapeSequences(t *testing.T) {
	for _, tt := range []struct{ name, input, want string }{
		{"coloured application log", "2026-10-03T20:59:23Z \x1b[2m2026-10-03T20:59:23Z\x1b[0m \x1b[32mINFO\x1b[0m \x1b[2mapp\x1b[0m: ready \x1b[3mrevision\x1b[0m=3", "2026-10-03T20:59:23Z 2026-10-03T20:59:23Z INFO app: ready revision=3"},
		{"screen controls", "before\x1b[2J\x1b[1;1Hafter", "beforeafter"},
		{"title and clipboard controls", "\x1b]0;hidden title\x07text\x1b]52;c;c2VjcmV0\x1b\\", "text"},
		{"hyperlink", "\x1b]8;;https://example.test\x1b\\readable\x1b]8;;\x1b\\", "readable"},
		{"multiline colour", "\x1b[32mfirst\r\nsecond\x1b[0m", "first\nsecond"},
		{"text and remaining controls", "界 e\u0301\ttext\x00", "界 e\u0301    text�"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeLogLines(tt.input); got != tt.want {
				t.Fatalf("want %q, got %q", tt.want, got)
			}
		})
	}
}

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
