package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestResourceDiagnosticsTabsAndStaleResults(t *testing.T) {
	m := demoModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, batchCmd := m.Update(m.command("diagnose")())
	if m.mode != "events" || !m.reportLoading || !strings.Contains(ansi.Strip(m.resourcePanelTabs()), "5: Diagnostics") {
		t.Fatal("diagnostic tab did not load")
	}
	batch := batchCmd().(tea.BatchMsg)
	msg := batch[len(batch)-1]()
	m.Update(msg)
	if !strings.Contains(ansi.Strip(m.panel.GetContent()), "Scheduling diagnostics") || m.reportLoading {
		t.Fatal("scheduling report not rendered")
	}
	key(m, "2")
	key(m, "5")
	if m.resourcePanelTab != resourceDiagnosticsTab {
		t.Fatal("5 did not select diagnostics")
	}
	key(m, "esc")
	m.Update(msg)
	if m.resourceReport.Title != "" || m.mode != "" {
		t.Fatal("closed diagnostic report was resurrected")
	}
	r, _ := kube.Resolve(m.resources, "services")
	m.Update(m.navigate(route{resource: r, namespace: "default"}, false)())
	_, batchCmd = m.Update(m.command("diagnose")())
	batch = batchCmd().(tea.BatchMsg)
	m.Update(batch[len(batch)-1]())
	text := ansi.Strip(m.panel.GetContent())
	if !strings.Contains(text, "Service troubleshooting") || !strings.Contains(text, "ready published addresses") {
		t.Fatal(text)
	}
}
func TestDiagnosticsUnsupportedAndReadOnly(t *testing.T) {
	m := demoModel(t)
	m.options.ReadOnly = true
	if cmd := m.command("diagnose"); cmd == nil {
		t.Fatal("read-only mode blocked diagnostics")
	}
	m.closeEvents()
	m.mode = ""
	r, _ := kube.Resolve(m.resources, "configmaps")
	m.Update(m.navigate(route{resource: r, namespace: "default"}, false)())
	if cmd := m.command("diagnose"); cmd != nil || !strings.Contains(m.notice, "Pod or Service") {
		t.Fatal("unsupported resource accepted")
	}
	m.eventObject = m.rows[0]
	if strings.Contains(ansi.Strip(m.resourcePanelTabs()), "5:") {
		t.Fatal("unsupported tab shown")
	}
}

func TestDiagnosticsUnavailableWhenSelectedResourceGetIsDenied(t *testing.T) {
	m := demoModel(t)
	m.backend = &secretReader{Demo: m.backend.(*kube.Demo), read: func(context.Context) (unstructured.Unstructured, error) {
		return unstructured.Unstructured{}, errors.New("get pods is forbidden")
	}}
	m.Update(m.command("diagnose")())
	key(m, "5")
	text := ansi.Strip(m.panel.GetContent())
	if !strings.Contains(text, "Diagnostics unavailable: get pods is forbidden") || strings.Contains(text, "Loading diagnostics") {
		t.Fatal(text)
	}
}
