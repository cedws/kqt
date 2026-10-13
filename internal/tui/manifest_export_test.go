package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestManifestExportPrivateFilesNoOverwriteAndCancelledPrompt(t *testing.T) {
	m := demoModel(t)
	path := filepath.Join(t.TempDir(), "saved manifest.json")
	cmd := m.manifestCommand("json clean \""+path+"\"", false)
	if cmd == nil {
		t.Fatal(m.notice)
	}
	m.Update(cmd())
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"kind": "Pod"`) || strings.Contains(string(data), `"uid"`) {
		t.Fatalf("invalid clean manifest %s %v", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("export is not private")
	}
	m.Update(m.manifestCommand("json \""+path+"\"", false)())
	after, _ := os.ReadFile(path)
	if string(after) != string(data) || !strings.Contains(m.notice, "Unable to save") {
		t.Fatal("export overwrote file or hid failure")
	}
	m.openDetails(false, true)
	m.promptManifestExport()
	key(m, "esc")
	if m.mode != "details" || m.manifestContent != "" {
		t.Fatal("cancel did not restore view/clear payload")
	}
	m.promptManifestExport()
	m.input.SetValue(filepath.Join(t.TempDir(), "details.json"))
	m.Update(m.manifestInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
	if m.mode != "details" || m.manifestContent != "" {
		t.Fatal("save did not restore view/clear payload")
	}
}
func TestManifestExportSecretRedactionAndCanonicalRevealedValues(t *testing.T) {
	m := demoModel(t)
	cmd := m.changeResource("secrets", false)
	m.Update(cmd())
	content, err := m.exportContent("json", true)
	var hidden map[string]interface{}
	if err == nil {
		err = json.Unmarshal([]byte(content), &hidden)
	}
	if err != nil || strings.Contains(content, "ZGVtby1vbmx5") || hidden["data"].(map[string]interface{})["token"] != "<redacted>" {
		t.Fatal("hidden Secret leaked")
	}
	m.Update(m.toggleSecret()())
	content, err = m.exportContent("json", true)
	if err != nil || !strings.Contains(content, "ZGVtby1vbmx5") || strings.Contains(content, "demo-only") {
		t.Fatal("export did not use original Kubernetes base64")
	}
	m.clearSecret()
	content, _ = m.exportContent("json", false)
	if strings.Contains(content, "ZGVtby1vbmx5") {
		t.Fatal("hidden Secret remained exportable")
	}
}
