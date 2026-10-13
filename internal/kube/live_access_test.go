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

func TestLiveAccess(t *testing.T) {
	path := os.Getenv("KQT_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("disposable Kubernetes fixture only")
	}
	loader, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(loader.Current(), "k3d-kqt-smoke-") {
		t.Fatal("permission qualification requires disposable cluster")
	}
	backend, _, _, e := loader.Open(loader.Current())
	if e != nil {
		t.Fatal(e)
	}
	defer backend.Close()
	admin := backend.(*Client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
	identity, e := limited.Identity(ctx)
	if e != nil || identity.Username != "system:serviceaccount:kqt-test:kqt-reader" {
		t.Fatal("live identity review failed", e)
	}
	pods, _ := Resolve(Builtins(), "pods")
	for _, tc := range []struct {
		verb, sub string
		allowed   bool
	}{{"get", "", true}, {"list", "", true}, {"patch", "", false}, {"delete", "", false}, {"create", "exec", false}} {
		name := "kqt-log-fixture"
		if tc.verb == "list" {
			name = ""
		}
		result, e := limited.ReviewAccess(ctx, AccessRequest{Resource: pods, Namespace: "kqt-test", Name: name, Verb: tc.verb, Subresource: tc.sub})
		if e != nil || result.Allowed != tc.allowed {
			t.Fatalf("live %s %s access mismatch: %+v %v", tc.verb, tc.sub, result, e)
		}
	}
}
