package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLiveOperator(t *testing.T) {
	path := os.Getenv("KQT_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("disposable Kubernetes fixture only")
	}
	loader, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loader.Current(), "k3d-kqt-smoke-") {
		t.Fatal("live qualification requires a disposable kqt-smoke cluster")
	}
	backend, _, _, err := loader.Open(loader.Current())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	client := backend.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resource, _ := Resolve(Builtins(), "pods")
	pod, err := client.Get(ctx, resource, "kqt-test", "kqt-log-fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("events-and-metrics", func(t *testing.T) {
		text, err := ResourceEvents(ctx, client, pod)
		if err != nil || !strings.Contains(text, "kqt-event-marker") {
			t.Fatalf("events not scoped to fixture: %s %v", text, err)
		}
		waitLive(t, 60*time.Second, func() bool {
			usage, err := client.Metrics(ctx, "Pod", "kqt-test")
			_, present := usage[UsageKey(pod)]
			return err == nil && present
		})
		waitLive(t, 60*time.Second, func() bool { usage, err := client.Metrics(ctx, "Node", ""); return err == nil && len(usage) > 0 })
	})
	for _, fallback := range []bool{false, true} {
		name := "websocket"
		if fallback {
			name = "forced-spdy-fallback"
		}
		t.Run(name, func(t *testing.T) {
			var forced atomic.Int32
			cfg := rest.CopyConfig(client.config)
			if fallback {
				cfg.Wrap(func(base http.RoundTripper) http.RoundTripper {
					return roundTripFunc(func(r *http.Request) (*http.Response, error) {
						if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/exec") || strings.HasSuffix(r.URL.Path, "/portforward")) {
							forced.Add(1)
							return nil, &httpstream.UpgradeFailureError{Cause: fmt.Errorf("fixture disables WebSocket upgrade")}
						}
						return base.RoundTrip(r)
					})
				})
			}
			operator, err := NewClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer operator.Close()
			var stdout, stderr bytes.Buffer
			err = operator.Exec(ctx, pod, "fixture", []string{"/bin/sh", "-c", "printf kqt-exec-marker"}, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
			if err != nil || stdout.String() != "kqt-exec-marker" {
				t.Fatalf("exec failed: %q %s %v", stdout.String(), stderr.String(), err)
			}
			if err = operator.Exec(ctx, pod, "fixture", []string{"/kqt-missing-shell"}, remotecommand.StreamOptions{Stdout: io.Discard, Stderr: io.Discard}); err == nil {
				t.Fatal("missing shell reported success")
			}
			serviceResource, _ := Resolve(Builtins(), "services")
			service, err := client.Get(ctx, serviceResource, "kqt-test", "kqt-http")
			if err != nil {
				t.Fatal(err)
			}
			target, port, err := ForwardTarget(ctx, client, service, 80)
			if err != nil {
				t.Fatal(err)
			}
			forwardCtx, stop := context.WithCancel(ctx)
			defer stop()
			ready := make(chan uint16, 1)
			done := make(chan error, 1)
			go func() {
				done <- operator.Forward(forwardCtx, target, fmt.Sprintf("0:%d", port), func(local uint16) { ready <- local })
			}()
			var local uint16
			select {
			case local = <-ready:
			case err := <-done:
				t.Fatalf("forward failed: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("forward did not become ready")
			}
			url := fmt.Sprintf("http://127.0.0.1:%d/", local)
			httpClient := &http.Client{Timeout: 3 * time.Second}
			response, err := httpClient.Get(url)
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || !strings.Contains(string(content), "kqt-forward-marker") {
				t.Fatalf("forwarded wrong traffic: %q %v", content, err)
			}
			stop()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("forward failed to stop")
			}
			if response, err := httpClient.Get(url); err == nil {
				response.Body.Close()
				t.Fatal("listener survived cancellation")
			}
			if fallback && forced.Load() < 2 {
				t.Fatal("fallback was not exercised for both operations")
			}
		})
	}
	t.Run("rbac-denial", func(t *testing.T) {
		lifetime := int64(600)
		token, err := client.core.ServiceAccounts("kqt-test").CreateToken(ctx, "kqt-reader", &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &lifetime}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		cfg := rest.CopyConfig(client.config)
		cfg.CertData = nil
		cfg.KeyData = nil
		cfg.CertFile = ""
		cfg.KeyFile = ""
		cfg.Username = ""
		cfg.Password = ""
		cfg.BearerTokenFile = ""
		cfg.BearerToken = token.Status.Token
		cfg.ExecProvider = nil
		cfg.AuthProvider = nil
		limited, err := NewClient(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer limited.Close()
		err = limited.Exec(ctx, pod, "fixture", []string{"/bin/sh"}, remotecommand.StreamOptions{Stdout: io.Discard})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "forbidden") {
			t.Fatalf("exec denial not surfaced: %v", err)
		}
		err = limited.Forward(ctx, pod, "0:8080", func(uint16) { t.Error("denied forward became ready") })
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "forbidden") {
			t.Fatalf("forward denial not surfaced: %v", err)
		}
	})
	t.Run("workload-live-logs", func(t *testing.T) {
		deployResource, _ := Resolve(Builtins(), "deployments")
		deployment, err := client.Get(ctx, deployResource, "kqt-test", "kqt-deployment")
		if err != nil {
			t.Fatal(err)
		}
		logsCtx, stop := context.WithCancel(ctx)
		defer stop()
		lines := make(chan string, 2048)
		finished := make(chan error, 1)
		go func() {
			finished <- FollowWorkload(logsCtx, client, deployment, "fixture", func(line string) {
				select {
				case lines <- line:
				case <-logsCtx.Done():
				}
			})
		}()
		podResource, _ := Resolve(Builtins(), "pods")
		query := Query{Resource: podResource, Namespace: "kqt-test", Labels: "app=kqt-deployment"}
		before, err := client.List(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		known := map[string]bool{}
		for _, p := range before {
			known[p.GetName()] = true
		}
		scale := func(replicas int64) {
			t.Helper()
			for attempt := 0; attempt < 5; attempt++ {
				fresh, err := client.Get(ctx, deployResource, "kqt-test", "kqt-deployment")
				if err != nil {
					t.Fatal(err)
				}
				patch, _ := json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{"uid": fresh.GetUID(), "resourceVersion": fresh.GetResourceVersion()}, "spec": map[string]interface{}{"replicas": replicas}})
				if client.Patch(ctx, deployResource, "kqt-test", "kqt-deployment", patch) == nil {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Fatal("fixture scale failed")
		}
		defer func() {
			scale(2)
			waitLive(t, 30*time.Second, func() bool { pods, err := client.List(ctx, query); return err == nil && len(pods) == 2 })
		}()
		scale(3)
		waitLive(t, 30*time.Second, func() bool {
			for {
				select {
				case line := <-lines:
					for _, word := range strings.Fields(line) {
						if strings.Contains(word, "kqt-deployment-log-marker-") {
							name := strings.TrimPrefix(word, "kqt-deployment-log-marker-")
							if !known[name] {
								return true
							}
						}
					}
				default:
					return false
				}
			}
		})
		stop()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("live logs did not cancel")
		}
	})
}
func waitLive(t *testing.T, limit time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("live fixture check timed out")
}
