package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
)

func TestActionConfirmationCancellationAndExactTarget(t *testing.T) {
	m := demoModel(t)
	m.Update(m.changeResource("deployments", false)())
	m.actionCommand("scale", "3")
	if m.mode != "action-confirm" {
		t.Fatal("missing confirmation")
	}
	m.input.SetValue("wrong")
	if m.actionInputKey(tea.KeyPressMsg{Code: tea.KeyEnter}) != nil {
		t.Fatal("wrong confirmation applied")
	}
	key(m, "esc")
	if m.actionPlan != nil || m.mode != "" {
		t.Fatal("cancel retained action")
	}
	if kube.Number(m.visible[0], "spec", "replicas") != 12 {
		t.Fatal("cancel changed replicas")
	}
	m.actionCommand("scale", "3")
	m.input.SetValue("default/web")
	cmd := m.actionInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, refresh := m.Update(cmd())
	m.Update(refresh())
	if kube.Number(m.visible[0], "spec", "replicas") != 3 || !strings.Contains(m.notice, "requested") {
		t.Fatal("confirmed scale failed")
	}
	m.Update(actionResultMsg{generation: m.generation - 1, message: "old"})
	if m.notice == "old" {
		t.Fatal("obsolete action updated UI")
	}
}
