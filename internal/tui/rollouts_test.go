package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
	"strings"
	"testing"
)

func TestRolloutHistoryDetailsComparisonAndConfirmedRollback(t *testing.T) {
	m := demoModel(t)
	m.Update(m.changeResource("deployments", false)())
	_, picker := m.Update(m.openRollouts(0)())
	if picker == nil || m.mode != "picker" || len(m.matches) != 2 || !strings.Contains(m.matches[0].description, "current") {
		t.Fatal("history did not offer revisions")
	}
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	key(m, "enter")
	if m.mode != "rollout-details" || !strings.Contains(ansi.Strip(m.View().Content), "nginx:1.27") {
		t.Fatal("revision details missing")
	}
	key(m, "d")
	if !strings.Contains(ansi.Strip(m.panel.GetContent()), "/spec/containers/0/image") || !strings.Contains(m.panel.GetContent(), "nginx:1.28") {
		t.Fatal("comparison missing image changes")
	}
	key(m, "r")
	if m.mode != "action-confirm" || m.actionPlan.Rollback == nil {
		t.Fatal("rollback skipped confirmation")
	}
	m.input.SetValue("wrong")
	if m.actionInputKey(tea.KeyPressMsg{Code: tea.KeyEnter}) != nil {
		t.Fatal("wrong resource confirmed rollback")
	}
	m.input.SetValue("default/web")
	cmd := m.actionInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, refresh := m.Update(cmd())
	m.Update(refresh())
	if !strings.Contains(m.notice, "Rollback to revision 1 requested") || !strings.Contains(kube.Manifest(m.visible[0], false), "nginx:1.27") {
		t.Fatal("rollback failed", m.notice)
	}
}

func TestRolloutHistoryReadOnlyCancellationAndLateResults(t *testing.T) {
	m := demoModel(t)
	m.Update(m.changeResource("deployments", false)())
	cmd := m.openRollouts(0)
	msg := cmd()
	key(m, "esc")
	m.Update(msg)
	if m.mode != "" {
		t.Fatal("late history reopened a closed view")
	}
	m.options.ReadOnly = true
	m.Update(m.openRollouts(0)())
	key(m, "enter")
	key(m, "r")
	if m.mode != "rollout-details" || !strings.Contains(m.notice, "Read-only") || m.actionPlan != nil {
		t.Fatal("read-only history permitted rollback")
	}
}

func TestRollbackConfirmationAcceptsLongResourceIdentity(t *testing.T) {
	m := demoModel(t)
	m.Update(m.changeResource("deployments", false)())
	m.Update(m.openRollouts(0)())
	plan, err := kube.PlanRollback(m.rollouts, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan.Object.SetNamespace(strings.Repeat("n", 63))
	plan.Object.SetName(strings.Repeat("d", 253))
	m.confirmAction(plan)
	identity := actionIdentity(&plan)
	m.input.SetValue(identity)
	if m.input.Value() != identity {
		t.Fatal("confirmation truncated a legal resource identity")
	}
	key(m, "esc")
	if m.input.CharLimit != 256 {
		t.Fatal("confirmation limit leaked into the command prompt")
	}
}

func TestRolloutHistoryReloadsFromPickerAndErrorPanel(t *testing.T) {
	m := demoModel(t)
	m.Update(m.changeResource("deployments", false)())
	m.Update(m.openRollouts(0)())
	cmd := m.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil || m.mode != "rollouts-loading" {
		t.Fatal("Ctrl+R did not reload the revision picker")
	}
	m.Update(cmd())
	m.mode, m.panelTitle = "operation-error", "Rollout history unavailable"
	cmd = m.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil || m.mode != "rollouts-loading" {
		t.Fatal("Ctrl+R did not retry unavailable history")
	}
}
