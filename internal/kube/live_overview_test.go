package kube

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestLiveOverview(t *testing.T) {
	path := os.Getenv("KQT_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("disposable Kubernetes fixture only")
	}
	loader, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(loader.Current(), "k3d-kqt-smoke-") {
		t.Fatal("overview qualification requires disposable cluster")
	}
	b, _, _, e := loader.Open(loader.Current())
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	admin := b.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	overview := ReadOverview(ctx, admin, "kqt-test")
	if len(overview.Objects["pods"]) != 5 || len(overview.Objects["nodes"]) != 1 || len(overview.Objects["deployments"]) != 1 {
		t.Fatalf("live overview counts wrong: pods=%d nodes=%d deployments=%d errors=%v", len(overview.Objects["pods"]), len(overview.Objects["nodes"]), len(overview.Objects["deployments"]), overview.Errors)
	}
	cpu, memory, reported := overview.Capacity()
	if cpu <= 0 || memory <= 0 || reported != 1 {
		t.Fatal("live capacity absent")
	}
	lifetime := int64(600)
	token, e := admin.core.ServiceAccounts("kqt-test").CreateToken(ctx, "kqt-reader", &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &lifetime}}, metav1.CreateOptions{})
	if e != nil {
		t.Fatal(e)
	}
	cfg := rest.CopyConfig(admin.config)
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
	limited, e := NewClient(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer limited.Close()
	partial := ReadOverview(ctx, limited, "kqt-test")
	if len(partial.Objects["pods"]) != 5 || partial.Errors["nodes"] == "" || partial.Errors["deployments"] == "" {
		t.Fatal("partial permissions lost usable data or hid unavailable sections")
	}
}
