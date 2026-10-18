package kube

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
)

func TestBundleArchiveRedactionAndOptInLogs(t *testing.T) {
	d, _ := networkFixture(t)
	pod := &d.objects[1]
	pod.SetAnnotations(map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "buried-secret", "note": "keep"})
	pod.Object["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)["env"] = []any{map[string]any{"name": "PASSWORD", "value": "literal-secret"}, map[string]any{"name": "TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "credentials", "key": "token"}}}}
	b := &diagnosticBackend{Demo: d, denied: map[string]bool{"nodes": true}}
	bundle, err := CollectBundle(context.Background(), b, Builtins(), *pod, BundleOptions{Context: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if b.secretReads != 0 {
		t.Fatal("bundle attempted to read a Secret")
	}
	if _, ok := bundle.Files["logs.txt"]; ok {
		t.Fatal("logs included without opt-in")
	}
	manifest := string(bundle.Files["resource.yaml"])
	if strings.Contains(manifest, "literal-secret") || strings.Contains(manifest, "buried-secret") || !strings.Contains(manifest, "<redacted>") {
		t.Fatal("manifest leaked known secret fields")
	}
	data, err := bundle.Archive()
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Mode != 0600 || header.Typeflag != tar.TypeReg || strings.Contains(header.Name, "/") {
			t.Fatal("unsafe archive entry")
		}
		content, _ := io.ReadAll(tr)
		if strings.Contains(string(content), "literal-secret") {
			t.Fatal("secret value in archive")
		}
		count++
	}
	if count != len(bundle.Files) || count < 5 {
		t.Fatal("missing bundle entries")
	}
	bundle, err = CollectBundle(context.Background(), b, Builtins(), *pod, BundleOptions{IncludeLogs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files["logs.txt"]) == 0 || !strings.Contains(string(bundle.Files["README.txt"]), "not redacted") {
		t.Fatal("opt-in logs or privacy note missing")
	}
}
func TestBundleRejectsSecretsReplacementAndCancellation(t *testing.T) {
	d, _ := networkFixture(t)
	b := &diagnosticBackend{Demo: d}
	secret := *d.objects[1].DeepCopy()
	secret.SetKind("Secret")
	if _, err := CollectBundle(context.Background(), b, Builtins(), secret, BundleOptions{}); err == nil || b.gets != 0 {
		t.Fatal("Secret export performed reads")
	}
	b.replaceAfter = 1
	if _, err := CollectBundle(context.Background(), b, Builtins(), d.objects[1], BundleOptions{}); err == nil {
		t.Fatal("replaced resource exported")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CollectBundle(ctx, d, Builtins(), d.objects[1], BundleOptions{}); err == nil {
		t.Fatal("cancelled export succeeded")
	}
	if _, err := (DiagnosticBundle{Files: map[string][]byte{"../secret": {}}}).Archive(); err == nil {
		t.Fatal("archive traversal accepted")
	}
}
