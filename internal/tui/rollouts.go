package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"sigs.k8s.io/yaml"
	"strconv"
	"strings"
	"time"
)

type rolloutsMsg struct {
	generation, token uint64
	source            string
	history           kube.RolloutHistory
	revision          int64
	err               error
}

func (m *model) cancelRollouts() {
	m.rolloutToken++
	if m.rolloutCancel != nil {
		m.rolloutCancel()
		m.rolloutCancel = nil
	}
}

func (m *model) openRollouts(revision int64) tea.Cmd {
	o, ok := m.current()
	if !ok || m.backend == nil {
		m.notice = "Select a Deployment first"
		return nil
	}
	if o.GetKind() != "Deployment" {
		m.notice = "Rollout history supports Deployments; select a Deployment"
		return nil
	}
	if revision > 0 && !m.allowMutation("rollback") {
		return nil
	}
	m.cancelRollouts()
	m.mode = "rollouts-loading"
	m.resize()
	m.panelTitle = "Rollout history"
	m.panel.SetContent("Loading Deployment revisions…")
	m.panel.GotoTop()
	ctx, cancel := context.WithTimeout(m.session.ctx, 15*time.Second)
	m.rolloutCancel = cancel
	g, token, b, resources := m.generation, m.rolloutToken, m.backend, m.resources
	return func() tea.Msg {
		defer cancel()
		history, err := kube.ReadRolloutHistory(ctx, b, resources, o)
		return rolloutsMsg{g, token, kube.Identity(o), history, revision, err}
	}
}

func (m *model) rolloutsReady(v rolloutsMsg) tea.Cmd {
	if v.generation != m.generation || v.token != m.rolloutToken || m.mode != "rollouts-loading" {
		return nil
	}
	o, ok := m.current()
	if !ok || kube.Identity(o) != v.source {
		m.mode = ""
		m.notice = "Selection changed; open rollout history again"
		return nil
	}
	if v.err != nil {
		m.mode = "operation-error"
		m.panelTitle = "Rollout history unavailable"
		m.panel.SetContent(kube.Safe(v.err.Error()))
		return nil
	}
	m.rollouts = v.history
	if v.revision > 0 {
		return m.confirmRollback(v.revision)
	}
	if len(v.history.Revisions) == 0 {
		m.mode = "operation-error"
		m.panelTitle = "Rollout history"
		m.panel.SetContent("No retained revisions. History is limited by the Deployment's revisionHistoryLimit.\n\nCtrl+R reloads; Esc closes.")
		return nil
	}
	return m.openPicker("rollouts")
}

func (m *model) rolloutChoices() []choice {
	var choices []choice
	current, _, _ := unstructured.NestedMap(m.rollouts.Deployment.Object, "spec", "template")
	for _, entry := range m.rollouts.Revisions {
		description := kube.Safe(entry.Images)
		if reflect.DeepEqual(current, entry.Template) {
			description = "current · " + description
		}
		if entry.Cause != "" {
			description += " · " + kube.Safe(entry.Cause)
		}
		choices = append(choices, choice{fmt.Sprintf("revision %d", entry.Number), description})
	}
	return choices
}

func (m *model) showRollout(revision int64, diff bool) {
	for _, entry := range m.rollouts.Revisions {
		if entry.Number != revision {
			continue
		}
		m.rolloutSelected = revision
		m.mode = "rollout-details"
		m.resize()
		m.panel.SoftWrap = true
		m.panelTitle = fmt.Sprintf("Revision %d", revision)
		header := fmt.Sprintf("Deployment · %s/%s\nRevision   %d\nCreated    %s\nImages     %s\n", kube.Safe(m.rollouts.Deployment.GetNamespace()), kube.Safe(m.rollouts.Deployment.GetName()), revision, kube.RolloutCreated(entry), kube.Safe(entry.Images))
		if entry.Cause != "" {
			header += "Cause      " + kube.Safe(entry.Cause) + "\n"
		}
		content := ""
		if diff {
			current, _, _ := unstructured.NestedMap(m.rollouts.Deployment.Object, "spec", "template")
			changes := kube.TemplateChanges(current, entry.Template)
			content = "\nDeployment at history load → selected revision\n"
			if len(changes) == 0 {
				content += "No template changes.\n"
			}
			for _, change := range changes {
				content += "\n" + m.theme.style("accent").Render(kube.Safe(change.Path)) + "\n" + m.theme.style("error").Render("- "+kube.Safe(change.Before)) + "\n" + m.theme.style("success").Render("+ "+kube.Safe(change.After)) + "\n"
			}
		} else {
			data, err := yaml.Marshal(entry.Template)
			if err != nil {
				m.notice = kube.Safe(err.Error())
				return
			}
			content = "\nPod template\n" + m.theme.renderYAML(string(data))
		}
		m.panel.SetContent(header + content)
		m.panel.GotoTop()
		return
	}
	m.notice = "Revision is unavailable; reload rollout history"
}

func (m *model) confirmRollback(revision int64) tea.Cmd {
	if !m.allowMutation("rollback") {
		return nil
	}
	plan, err := kube.PlanRollback(m.rollouts, revision)
	if err != nil {
		m.mode = ""
		m.notice = kube.Safe(err.Error())
		return nil
	}
	return m.confirmAction(plan)
}

func (m *model) rolloutCommand(arg string, rollback bool) tea.Cmd {
	if !rollback {
		if arg != "" && arg != "history" {
			m.notice = "Use :rollouts or :rollout history"
			return nil
		}
		return m.openRollouts(0)
	}
	if arg == "" {
		return m.openRollouts(0)
	}
	revision, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil || revision <= 0 {
		m.notice = "Use :rollback REVISION (a positive number)"
		return nil
	}
	return m.openRollouts(revision)
}
