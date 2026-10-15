package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func TestLogRequestsApplyHistoryLimitsAndResumeFromCheckpoint(t *testing.T) {
	requests := make(chan url.Values, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/apps/pods/web/log" {
			t.Errorf("wrong log scope: %s", r.URL.Path)
		}
		requests <- r.URL.Query()
		fmt.Fprintln(w, "2026-10-15T12:00:00Z INFO ready")
	}))
	defer server.Close()
	client, err := NewClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	since := time.Date(2026, 10, 15, 11, 50, 0, 0, time.UTC)
	checkpoint := since.Add(9 * time.Minute)
	for _, count := range []int64{1000, 0, -1} {
		options := LogOptions{SinceTime: &since, TailLines: &count}
		if _, err := client.Logs(context.Background(), "apps", "web", "app", true, options); err != nil {
			t.Fatal(err)
		}
		query := <-requests
		if query.Get("tailLines") != fmt.Sprint(count) || query.Get("sinceTime") != since.Format(time.RFC3339) || query.Get("previous") != "true" || query.Get("timestamps") != "true" || query.Get("limitBytes") != "1048576" || query.Get("container") != "app" {
			t.Fatalf("snapshot lost history limits: %v", query)
		}
		for _, resume := range []*time.Time{nil, &checkpoint} {
			reader, err := client.StreamLogs(context.Background(), "apps", "web", "app", resume, options)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, reader)
			reader.Close()
			query = <-requests
			if query.Get("follow") != "true" || query.Get("timestamps") != "true" || query.Get("limitBytes") != "" {
				t.Fatalf("stream lost collection options: %v", query)
			}
			if resume == nil {
				if query.Get("tailLines") != fmt.Sprint(count) || query.Get("sinceTime") != since.Format(time.RFC3339) {
					t.Fatalf("initial stream lost limits: %v", query)
				}
			} else if query.Get("tailLines") != "" || query.Get("sinceTime") != checkpoint.Format(time.RFC3339) {
				t.Fatalf("resume reused initial tail/window instead of checkpoint: %v", query)
			}
		}
	}
	client.Logs(context.Background(), "apps", "web", "app", false)
	if query := <-requests; query.Get("tailLines") != "500" || query.Get("sinceTime") != "" {
		t.Fatalf("snapshot defaults changed: %v", query)
	}
	reader, err := client.StreamLogs(context.Background(), "apps", "web", "app", nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, reader)
	reader.Close()
	if query := <-requests; query.Get("tailLines") != "200" {
		t.Fatalf("live defaults changed: %v", query)
	}
}

func TestDemoHistoryWindowsAreStableAndAppliedPerSource(t *testing.T) {
	demo := NewDemo()
	ctx := context.Background()
	all, _ := demo.Logs(ctx, "default", "web", "web", false)
	count := int64(2)
	tail, _ := demo.Logs(ctx, "default", "web", "web", false, LogOptions{TailLines: &count})
	if len(strings.Split(tail, "\n")) != 2 || !strings.HasSuffix(all, tail) {
		t.Fatalf("wrong tail: %s", tail)
	}
	count = 0
	if empty, _ := demo.Logs(ctx, "default", "web", "web", false, LogOptions{TailLines: &count}); empty != "" {
		t.Fatal("tail 0 returned initial history")
	}
	since := demo.logStart.Add(8 * time.Second)
	window, _ := demo.Logs(ctx, "default", "web", "web", false, LogOptions{SinceTime: &since})
	if len(strings.Split(window, "\n")) != 3 || strings.Contains(window, "listening") {
		t.Fatalf("wrong window: %s", window)
	}
	again, _ := demo.Logs(ctx, "default", "web", "web", false)
	if all != again {
		t.Fatal("demo log timestamps moved between requests")
	}
	resource, _ := Resolve(Builtins(), "deployments")
	workload, _ := demo.Get(ctx, resource, "default", "web")
	count = 1
	combined, err := LogSnapshot(ctx, demo, workload, "web", false, LogOptions{SinceTime: &since, TailLines: &count})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(combined, "WARN slow request") != 12 || strings.Contains(combined, "configuration loaded") {
		t.Fatalf("limits were not forwarded to every pod: %s", combined)
	}
}
