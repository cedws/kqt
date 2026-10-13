package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
)

func TestReadOnlyBlocksMutationEntrypointsButAllowsInspection(t *testing.T) {
	m := demoModel(t)
	m.options.ReadOnly = true
	for _, command := range []string{"edit", "scale 2", "restart", "delete", "suspend", "resume", "run", "shell"} {
		if m.command(command) != nil || !strings.Contains(m.notice, "Read-only") || m.actionPlan != nil || m.editBuffer != nil {
			t.Fatalf("read-only command was not blocked: %s", command)
		}
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "READ ONLY") {
		t.Fatal("read-only marker missing")
	}
	m.command("permissions")
	if m.mode != "access-loading" {
		t.Fatal("read-only prevented permissions")
	}
	key(m, "esc")
	if _, e := m.exportContent("yaml", true); e != nil {
		t.Fatal("read-only prevented export")
	}
	if m.copyManifest("yaml", false) == nil {
		t.Fatal("read-only prevented copy")
	}
	m.options.ReadOnly = false
	m.Update(m.changeResource("deployments", false)())
	m.actionCommand("scale", "2")
	m.input.SetValue("default/web")
	m.options.ReadOnly = true
	if m.actionInputKey(tea.KeyPressMsg{Code: tea.KeyEnter}) != nil {
		t.Fatal("confirmation bypassed read-only")
	}
	key(m, "esc")
}
func TestReadOnlyConfigAndFlagPrecedence(t *testing.T) {
	m := demoModel(t)
	m.settings.Config.ReadOnly = true
	configured, e := newModel(context.Background(), m.settings, Options{Demo: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer configured.close()
	overridden, e := newModel(context.Background(), m.settings, Options{Demo: true, ReadOnlyExplicit: true, ReadOnly: false}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer overridden.close()
	if !configured.options.ReadOnly || overridden.options.ReadOnly {
		t.Fatal("read-only flag precedence failed")
	}
}
func TestPermissionsRenderAllowedDeniedUnknownAndRejectStaleReplies(t *testing.T) {
	m := demoModel(t)
	cmd := m.openAccess("")
	message := cmd().(accessMsg)
	message.lines = []accessLine{{label: "get", decision: kube.AccessDecision{Allowed: true}}, {label: "delete", decision: kube.AccessDecision{Allowed: false}}, {label: "watch", err: fmt.Errorf("review unavailable")}}
	m.Update(message)
	content := ansi.Strip(m.panel.GetContent())
	for _, text := range []string{"demo-user", "Allowed", "Denied", "Unknown", "review unavailable"} {
		if !strings.Contains(content, text) {
			t.Fatalf("missing permission detail %s", text)
		}
	}
	m.openAccess("")
	key(m, "esc")
	m.Update(message)
	if m.mode != "" {
		t.Fatal("stale permission reply reopened panel")
	}
}

func TestSinglePermissionRefreshKeepsItsQuery(t *testing.T) {
	m := demoModel(t)
	message := m.openAccess("get")().(accessMsg)
	m.accessReady(message)
	cmd := m.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	refreshed := cmd().(accessMsg)
	if len(refreshed.lines) != 1 || refreshed.lines[0].label != "get" {
		t.Fatal("single-action refresh changed the query")
	}
}
