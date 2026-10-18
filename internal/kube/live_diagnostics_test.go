package kube

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestLiveDiagnostics(t *testing.T) {
	path := os.Getenv("KQT_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("disposable Kubernetes fixture only")
	}
	loader, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loader.Current(), "k3d-kqt-smoke-") {
		t.Fatal("diagnostics qualification requires a disposable kqt-smoke cluster")
	}
	b, _, _, err := loader.Open(loader.Current())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	admin := b.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	svcResource, _ := Resolve(Builtins(), "services")
	svc, err := admin.Get(ctx, svcResource, "kqt-test", "kqt-http")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, 30*time.Second, func() bool {
		report, err := ServiceDiagnostics(ctx, admin, svc)
		return err == nil && strings.Contains(report.Text(), "1/1 ready published addresses")
	})
	report, err := ServiceDiagnostics(ctx, admin, svc)
	if err != nil || !strings.Contains(report.Text(), "fixture-http") || strings.Contains(report.Text(), "WARNING:") {
		t.Fatal(err, report.Text())
	}
	pods, _ := Resolve(Builtins(), "pods")
	pod, err := admin.Get(ctx, pods, "kqt-test", "kqt-log-fixture")
	if err != nil {
		t.Fatal(err)
	}
	cfg := rest.CopyConfig(admin.config)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:kqt-test:kqt-reader", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:kqt-test", "system:authenticated"}}
	limited, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	report, err = SchedulingDiagnostics(ctx, limited, pod)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Nodes · unavailable", "Scheduler events · unavailable", "PodScheduled=True"} {
		if !strings.Contains(report.Text(), want) {
			t.Fatal(report.Text())
		}
	}
	bundle, err := CollectBundle(ctx, limited, Builtins(), pod, BundleOptions{Context: loader.Current()})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files["resource.yaml"]) == 0 || !strings.Contains(string(bundle.Files["README.txt"]), "Events unavailable") {
		t.Fatal("restricted bundle failed to capture partial diagnostics")
	}
	if _, present := bundle.Files["logs.txt"]; present {
		t.Fatal("logs exported without consent")
	}
	if _, err = bundle.Archive(); err != nil {
		t.Fatal(err)
	}
	no := false
	pending, err := admin.core.Pods("kqt-test").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "kqt-unschedulable"}, Spec: corev1.PodSpec{AutomountServiceAccountToken: &no, NodeSelector: map[string]string{"kqt.dev/nonexistent": "true"}, Containers: []corev1.Container{{Name: "app", Image: "busybox:1.37", Command: []string{"sleep", "3600"}}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = admin.core.Pods("kqt-test").Delete(context.Background(), pending.Name, metav1.DeleteOptions{})
	})
	selected, err := admin.Get(ctx, pods, "kqt-test", pending.Name)
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, 40*time.Second, func() bool {
		report, err = SchedulingDiagnostics(ctx, admin, selected)
		return err == nil && strings.Contains(report.Text(), "FailedScheduling")
	})
	if !strings.Contains(report.Text(), "nodeSelector mismatch: kqt.dev/nonexistent") || !strings.Contains(report.Text(), "PodScheduled=False") {
		t.Fatal(report.Text())
	}
}
