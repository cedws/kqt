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
	m.cancelAction()
	m.actionPlan = &plan
	m.mode = "action-confirm"
	m.panelTitle = "Confirm " + action
	m.panel.SoftWrap = true
	m.panel.GotoTop()
	m.renderActionConfirmation("")
	m.input.Prompt = "Confirm: "
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
		plan := m.actionPlan
		if plan == nil || m.input.Value() != actionIdentity(plan) {
			m.notice = "Type the exact resource name shown to confirm"
			m.renderActionConfirmation(m.notice)
			return nil
		}
		m.input.Blur()
		m.mode = "action-running"
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
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(v)
	return cmd
}
func (m *model) actionResult(v actionResultMsg) tea.Cmd {
	if v.generation != m.generation || v.token != m.actionToken || m.mode != "action-running" {
		return nil
	}
	m.cancelAction()
	m.mode = ""
	if v.err != nil {
		m.notice = "Action failed: " + kube.Safe(v.err.Error())
	} else {
		m.notice = kube.Safe(v.message)
	}
	return m.refreshResources()
}
