package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apihttp "k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	streamhttp "k8s.io/streaming/pkg/httpstream"
)

func TestStreamingLogsOutliveListTimeoutAndCancel(t *testing.T) {
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("follow") != "true" || r.URL.Query().Get("timestamps") != "true" || r.URL.Query().Get("limitBytes") != "" {
			t.Errorf("unexpected log options: %s", r.URL)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.(http.Flusher).Flush()
		select {
		case <-time.After(200 * time.Millisecond):
			fmt.Fprintln(w, "2026-10-13T10:00:00Z late-line")
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			close(stopped)
			return
		}
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	client, err := NewClient(&rest.Config{Host: server.URL, Timeout: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, err := client.StreamLogs(ctx, "test", "pod", "container", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data := make([]byte, 256)
	n, err := reader.Read(data)
	if err != nil || !strings.Contains(string(data[:n]), "late-line") {
		t.Fatalf("long-lived stream interrupted: %q %v", data[:n], err)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stream did not cancel")
	}
}

func TestMetricsVersionFallbackAndQuantities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/v1/") {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
			return
		}
		if r.URL.Path != "/apis/metrics.k8s.io/v1beta1/namespaces/apps/pods" {
			t.Errorf("wrong metrics scope %s", r.URL)
		}
		fmt.Fprint(w, `{"kind":"PodMetricsList","apiVersion":"metrics.k8s.io/v1beta1","items":[{"metadata":{"name":"web","namespace":"apps"},"containers":[{"name":"one","usage":{"cpu":"125000000n","memory":"128Mi"}},{"name":"two","usage":{"cpu":"25m","memory":"32Mi"}}]}]}`)
	}))
	defer server.Close()
	client, err := NewClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := client.Metrics(context.Background(), "Pod", "apps")
	if err != nil {
		t.Fatal(err)
	}
	if usage["apps/web"] != (Usage{150, 160 * 1024 * 1024}) {
		t.Fatalf("wrong aggregation: %#v", usage)
	}
}

func TestSelectedEventsDoNotCrossResourceUIDs(t *testing.T) {
	demo := NewDemo()
	pods, _ := demo.List(context.Background(), Query{Resource: Builtins()[0], Namespace: "default"})
	text, err := ResourceEvents(context.Background(), demo, pods[0])
	if err != nil || !strings.Contains(text, "Back-off restarting") {
		t.Fatalf("expected scoped event: %s %v", text, err)
	}
	text, err = ResourceEvents(context.Background(), demo, pods[1])
	if err != nil || strings.Contains(text, "Back-off restarting") || !strings.Contains(text, "No events") {
		t.Fatalf("unrelated event leaked: %s %v", text, err)
	}
}

func TestServiceForwardResolvesNamedPortAndReadyPod(t *testing.T) {
	demo := NewDemo()
	pod := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]interface{}{"name": "target", "namespace": "apps", "uid": "target", "labels": map[string]interface{}{"app": "test"}}, "spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "web", "ports": []interface{}{map[string]interface{}{"name": "http", "containerPort": int64(8080)}}}}}, "status": map[string]interface{}{"phase": "Running", "conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}}}}}
	service := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "Service", "metadata": map[string]interface{}{"name": "service", "namespace": "apps", "uid": "service"}, "spec": map[string]interface{}{"selector": map[string]interface{}{"app": "test"}, "ports": []interface{}{map[string]interface{}{"port": int64(80), "targetPort": "http"}}}}}
	demo.objects = append(demo.objects, pod, service)
	target, port, err := ForwardTarget(context.Background(), demo, service, 80)
	if err != nil || port != 8080 || target.GetUID() != pod.GetUID() {
		t.Fatalf("wrong target: %s %d %v", target.GetName(), port, err)
	}
	if _, _, err = ForwardTarget(context.Background(), demo, service, 81); err == nil {
		t.Fatal("accepted unexposed service port")
	}
}

func TestExecRejectsReplacedPodBeforeOpeningStreams(t *testing.T) {
	operations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/exec") {
			operations++
			t.Error("opened exec on replacement pod")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod","namespace":"apps","uid":"replacement"},"status":{"phase":"Running"}}`)
	}))
	defer server.Close()
	client, err := NewClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	selected := unstructured.Unstructured{}
	selected.SetKind("Pod")
	selected.SetName("pod")
	selected.SetNamespace("apps")
	selected.SetUID("old")
	if err = client.Exec(context.Background(), selected, "web", []string{"/bin/sh"}, remotecommand.StreamOptions{Stdout: io.Discard}); err == nil || !strings.Contains(err.Error(), "replaced") || operations != 0 {
		t.Fatalf("stale pod accepted: %v", err)
	}
}

type replayStream struct {
	*Demo
	calls int
}

func (b *replayStream) StreamLogs(ctx context.Context, ns, pod, container string, since *time.Time) (io.ReadCloser, error) {
	b.calls++
	text := "2026-10-13T10:00:00Z first\n2026-10-13T10:00:00Z second\n"
	if b.calls > 1 {
		if since == nil {
			return nil, fmt.Errorf("reconnect lost checkpoint")
		}
		text += "2026-10-13T10:00:00Z third\n2026-10-13T10:00:01Z fourth\n"
	}
	return io.NopCloser(strings.NewReader(text)), nil
}
func TestLogReconnectPreservesEqualTimestampLinesWithoutReplay(t *testing.T) {
	backend := &replayStream{Demo: NewDemo()}
	r, _ := Resolve(Builtins(), "pods")
	pod, _ := backend.Get(context.Background(), r, "default", "web-7c9f6d8b5-0000")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var lines []string
	followSource(ctx, backend, backend, logSource{string(pod.GetUID()), pod.GetNamespace(), pod.GetName(), "web"}, func(line string) {
		lines = append(lines, line)
		if strings.Contains(line, "fourth") {
			cancel()
		}
	})
	if len(lines) != 4 || !strings.Contains(lines[2], "third") {
		t.Fatalf("lost or replayed equal-time lines: %v", lines)
	}
}

func TestForwardCancelInterruptsStartupHandshake(t *testing.T) {
	handshake := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/portforward") {
			close(handshake)
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod","namespace":"apps","uid":"original"},"status":{"phase":"Running"}}`)
	}))
	defer func() { server.CloseClientConnections(); server.Close() }()
	client, err := NewClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	pod := unstructured.Unstructured{}
	pod.SetKind("Pod")
	pod.SetName("pod")
	pod.SetNamespace("apps")
	pod.SetUID("original")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.Forward(ctx, pod, "0:8080", func(uint16) { t.Error("incomplete handshake became ready") })
	}()
	select {
	case <-handshake:
	case <-time.After(2 * time.Second):
		t.Fatal("handshake did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel left startup handshake blocked")
	}
}

func TestStreamingFallbackRecognizesBothClientGoErrorTypes(t *testing.T) {
	for _, err := range []error{&apihttp.UpgradeFailureError{Cause: fmt.Errorf("old upgrade")}, &streamhttp.UpgradeFailureError{Cause: fmt.Errorf("new upgrade")}} {
		if !streamFallback(err) {
			t.Fatalf("streaming error did not trigger fallback: %v", err)
		}
	}
	if streamFallback(context.Canceled) {
		t.Fatal("cancellation triggered a second connection attempt")
	}
}
