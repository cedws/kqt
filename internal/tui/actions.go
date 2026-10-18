package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
)

type actionResultMsg struct {
	generation, token uint64
	message           string
	err               error
}

func (m *model) cancelAction() {
	m.forgetHistoryExecution("action")
	m.input.CharLimit = 256
	m.actionToken++
	if m.actionCancel != nil {
		m.actionCancel()
		m.actionCancel = nil
	}
	m.actionPlan = nil
}
func actionIdentity(plan *kube.ActionPlan) string {
	return strings.TrimPrefix(plan.Object.GetNamespace()+"/"+plan.Object.GetName(), "/")
}
func (m *model) actionCommand(action, arg string) tea.Cmd {
	if action == "rollback" {
		return m.rolloutCommand(arg, true)
	}
	if !m.allowMutation(action) {
		return nil
	}
	o, ok := m.current()
	if !ok {
		m.notice = "Select a resource first"
		return nil
	}
	if action == "scale" && arg == "" {
		return m.startInput("command", fmt.Sprintf("scale %d", kube.Number(o, "spec", "replicas")))
	}
	plan, err := kube.PlanAction(m.route.resource, o, action, arg)
	if err != nil {
		m.notice = kube.Safe(err.Error())
		return nil
	}
	return m.confirmAction(plan)
}
func (m *model) confirmAction(plan kube.ActionPlan) tea.Cmd {
	m.cancelAction()
	m.actionPlan = &plan
	m.mode = "action-confirm"
	if plan.Action == "delete" {
		m.mode, m.deleteYes = "delete-confirm", false
		m.input.Blur()
		m.resize()
		m.panelTitle = "Delete " + plan.Object.GetKind() + "?"
		m.panel.SoftWrap = true
		m.panel.GotoTop()
		m.panel.SetContent(fmt.Sprintf("Context: %s\n%s\n\nDelete this %s?", kube.Safe(m.contextName), kube.Safe(actionIdentity(&plan)), plan.Object.GetKind()))
		return nil
	}
	m.resize()
	m.panelTitle = "Confirm " + plan.Action
	m.panel.SoftWrap = true
	m.panel.GotoTop()
	m.renderActionConfirmation("")
	m.input.Prompt = "Confirm: "
	m.input.CharLimit = max(256, len([]rune(actionIdentity(&plan))))
	m.input.SetValue("")
	return m.input.Focus()
}
func (m *model) renderActionConfirmation(message string) {
	plan := m.actionPlan
	if plan == nil {
		return
	}
	content := fmt.Sprintf("Context: %s\nResource: %s %s\n\n%s\n\nType %s and press Enter to confirm.\nEscape cancels.", kube.Safe(m.contextName), plan.Object.GetKind(), kube.Safe(actionIdentity(plan)), plan.Summary, kube.Safe(actionIdentity(plan)))
	if message != "" {
		content += "\n\n" + m.theme.style("warning").Render(message)
	}
	m.panel.SetContent(content)
}
func (m *model) actionInputKey(v tea.KeyPressMsg) tea.Cmd {
	if v.String() == "esc" {
		m.cancelAction()
		m.mode = ""
		m.input.Blur()
		return nil
	}
	if v.String() == "enter" {
		if !m.allowMutation("workload actions") {
			return nil
		}
		plan := m.actionPlan
		if plan == nil || m.input.Value() != actionIdentity(plan) {
			m.notice = "Type the exact resource name shown to confirm"
			m.renderActionConfirmation(m.notice)
			return nil
		}
		return m.applyConfirmedAction()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(v)
	return cmd
}

func (m *model) applyConfirmedAction() tea.Cmd {
	if !m.allowMutation("workload actions") || m.actionPlan == nil {
		return nil
	}
	plan := m.actionPlan
	m.input.Blur()
	m.mode = "action-running"
	m.resize()
	m.panel.SetContent("Applying " + plan.Action + " to " + kube.Safe(actionIdentity(plan)) + "…")
	ctx, cancel := context.WithTimeout(m.session.ctx, 15*time.Second)
	m.actionCancel = cancel
	g, token, b, captured := m.generation, m.actionToken, m.backend, *plan
	return func() tea.Msg {
		defer cancel()
		message, err := kube.ApplyAction(ctx, b, captured)
		return actionResultMsg{g, token, message, err}
	}
}

func (m *model) deleteKey(v tea.KeyPressMsg) tea.Cmd {
	switch v.String() {
	case "left", "right", "tab", "shift+tab":
		m.deleteYes = !m.deleteYes
	case "y", "Y":
		return m.applyConfirmedAction()
	case "enter":
		if m.deleteYes {
			return m.applyConfirmedAction()
		}
		fallthrough
	case "esc", "q", "n", "N":
		m.cancelAction()
		m.mode = ""
		m.resize()
		m.notice = "Deletion cancelled"
	default:
		scroll(&m.panel, v.String())
	}
	return nil
}
func (m *model) actionResult(v actionResultMsg) tea.Cmd {
	if v.generation != m.generation || v.token != m.actionToken || m.mode != "action-running" {
		return nil
	}
	m.cancelAction()
	m.mode = ""
	m.resize()
	if v.err != nil {
		m.notice = "Action failed: " + kube.Safe(v.err.Error())
	} else {
		m.notice = kube.Safe(v.message)
	}
	return m.refreshResources()
}
