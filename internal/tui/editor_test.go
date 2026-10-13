package tui

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"ket/internal/kube"
	"sigs.k8s.io/yaml"
)

func TestEditorCommandArguments(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{{`code --wait`, []string{"code", "--wait"}}, {`"/Applications/My Editor/editor" --title 'ket resource'`, []string{"/Applications/My Editor/editor", "--title", "ket resource"}}, {`editor --literal '$(echo nope)'`, []string{"editor", "--literal", "$(echo nope)"}}, {`editor --empty ""`, []string{"editor", "--empty", ""}}} {
		got, err := editorArgs(tc.command)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %v %v", tc.command, got, err)
		}
	}
	for _, command := range []string{"", `editor 'unfinished`, `editor \`} {
		if _, err := editorArgs(command); err == nil {
			t.Fatal("invalid editor accepted")
		}
	}
	t.Setenv("EDITOR", "environment-editor --wait")
	t.Setenv("VISUAL", "visual-editor")
	if editorCommand("") != "environment-editor --wait" || editorCommand("configured") != "configured" {
		t.Fatal("editor precedence")
	}
}
func editFixture(t *testing.T) (*editBuffer, *kube.Demo) {
	t.Helper()
	demo := kube.NewDemo()
	pod, _ := kube.Resolve(kube.Builtins(), "pods")
	o, err := demo.Get(context.Background(), pod, "default", "web-7c9f6d8b5-0000")
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := newEditBuffer(o, pod, demo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(buffer.remove)
	return buffer, demo
}
func writeEdit(t *testing.T, b *editBuffer, o unstructured.Unstructured) {
	t.Helper()
	data, err := yaml.Marshal(o.Object)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(b.path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestEditorNoChangeAndMinimalVersionedPatch(t *testing.T) {
	buffer, demo := editFixture(t)
	info, _ := os.Stat(buffer.path)
	dir, _ := os.Stat(buffer.directory)
	if info.Mode().Perm() != 0600 || dir.Mode().Perm() != 0700 {
		t.Fatal("edit file is not private")
	}
	changed, err := buffer.apply(context.Background())
	if changed || err != nil {
		t.Fatalf("unchanged edit applied: %v %v", changed, err)
	}
	edited := *buffer.editable.DeepCopy()
	edited.SetAnnotations(map[string]string{"ket.dev/edited": "yes"})
	writeEdit(t, buffer, edited)
	changed, err = buffer.apply(context.Background())
	if !changed || err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	updated, err := demo.Get(context.Background(), buffer.resource, "default", buffer.original.GetName())
	if err != nil || updated.GetAnnotations()["ket.dev/edited"] != "yes" || updated.GetResourceVersion() == buffer.original.GetResourceVersion() {
		t.Fatal("patch not applied")
	}
	if kube.Status(updated) != kube.Status(buffer.original) || len(kube.Containers(updated)) != len(kube.Containers(buffer.original)) {
		t.Fatal("edit removed unrelated fields/status")
	}
}
func TestEditorRejectsIdentityInvalidYAMLAndConflicts(t *testing.T) {
	for _, change := range []string{"name", "uid", "version", "status", "duplicate", "documents"} {
		t.Run(change, func(t *testing.T) {
			b, _ := editFixture(t)
			edited := *b.editable.DeepCopy()
			switch change {
			case "name":
				edited.SetName("other")
			case "uid":
				edited.SetUID("replaced")
			case "version":
				edited.SetResourceVersion("999")
			case "status":
				edited.Object["status"] = map[string]any{"phase": "Failed"}
			}
			writeEdit(t, b, edited)
			if change == "duplicate" {
				os.WriteFile(b.path, []byte("kind: Pod\nkind: Node\n"), 0600)
			}
			if change == "documents" {
				data, _ := os.ReadFile(b.path)
				os.WriteFile(b.path, append(data, []byte("---\nkind: Secret\n")...), 0600)
			}
			if _, err := b.apply(context.Background()); err == nil {
				t.Fatal("invalid edit accepted")
			}
		})
	}
	b, demo := editFixture(t)
	other := *b.editable.DeepCopy()
	other.SetAnnotations(map[string]string{"other": "change"})
	second, err := newEditBuffer(b.original, b.resource, demo)
	if err != nil {
		t.Fatal(err)
	}
	defer second.remove()
	writeEdit(t, second, other)
	if _, err = second.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	edit := *b.editable.DeepCopy()
	edit.SetAnnotations(map[string]string{"own": "change"})
	writeEdit(t, b, edit)
	if _, err = b.apply(context.Background()); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("version conflict missed: %v", err)
	}
	m := demoModel(t)
	m.editFinished(editedMsg{generation: m.generation, token: m.editToken, buffer: b, changed: true, err: err})
	if _, statErr := os.Stat(b.path); statErr != nil || !strings.Contains(m.notice, "file retained") {
		t.Fatal("conflicting edited file was discarded")
	}
}
func TestEditorFetchesFreshSecretAndRejectsLateResults(t *testing.T) {
	m := demoModel(t)
	cmd := m.changeResource("secrets", false)
	m.Update(cmd())
	shown, _ := m.current()
	if shown.Object["data"].(map[string]any)["token"] != "<redacted>" {
		t.Fatal("secret not redacted in UI")
	}
	ready := m.openEditor()().(editReadyMsg)
	if ready.err != nil || ready.original.Object["data"].(map[string]any)["token"] != "ZGVtby1vbmx5" {
		t.Fatal("editor would overwrite secret with redaction placeholder")
	}
	m.cancelEdit()
	if cmd := m.editorReady(ready); cmd != nil {
		t.Fatal("late edit opened an editor")
	}
}
