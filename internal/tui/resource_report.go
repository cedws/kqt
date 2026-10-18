package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
)

type resourceReportMsg struct {
	generation, eventToken, token uint64
	report                        kube.DiagnosticReport
	err                           error
}

func (m *model) hasResourceDiagnostics() bool {
	kind := m.eventObject.GetKind()
	return kind == "Pod" || kind == "Service"
}
func (m *model) closeResourceReport() {
	m.reportToken++
	if m.reportCancel != nil {
		m.reportCancel()
		m.reportCancel = nil
	}
	m.reportLoading = false
	m.reportError = ""
	m.resourceReport = kube.DiagnosticReport{}
	m.reportUpdated = time.Time{}
}
func (m *model) loadResourceReport() tea.Cmd {
	if m.eventGlobal || !m.hasResourceDiagnostics() || m.resourcePanelTab != resourceDiagnosticsTab || m.reportLoading || (!m.reportUpdated.IsZero() && time.Since(m.reportUpdated) < 5*time.Second) {
		return nil
	}
	parent := m.eventContext
	if parent == nil || parent.Err() != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	m.reportCancel = cancel
	m.reportLoading = true
	m.reportToken++
	g, eventToken, token, b, o := m.generation, m.eventToken, m.reportToken, m.backend, *m.eventObject.DeepCopy()
	return func() tea.Msg {
		defer cancel()
		var report kube.DiagnosticReport
		var err error
		if o.GetKind() == "Service" {
			report, err = kube.ServiceDiagnostics(ctx, b, o)
		} else {
			report, err = kube.SchedulingDiagnostics(ctx, b, o)
		}
		return resourceReportMsg{g, eventToken, token, report, err}
	}
}
func (m *model) resourceReportReady(v resourceReportMsg) tea.Cmd {
	if v.generation != m.generation || v.eventToken != m.eventToken || v.token != m.reportToken || !m.showingEventPanel() {
		return nil
	}
	m.reportLoading = false
	m.reportUpdated = time.Now()
	m.reportError = ""
	if v.err != nil {
		m.reportError = kube.Safe(v.err.Error())
	} else {
		m.resourceReport = v.report
	}
	if !m.eventPaused {
		m.renderEventPanel()
	}
	return nil
}
func (m *model) renderResourceReport() string {
	w := &statusWriter{theme: m.theme}
	if m.reportError != "" {
		w.note("Diagnostics unavailable: " + m.reportError)
	}
	if m.resourceReport.Title == "" {
		if m.reportLoading || m.reportError == "" {
			w.note("Loading diagnostics…")
		}
		return w.String()
	}
	w.heading(kube.Safe(m.resourceReport.Title))
	for _, section := range m.resourceReport.Sections {
		w.WriteByte('\n')
		w.heading(kube.Safe(section.Title))
		for _, line := range section.Lines {
			text := kube.SafeLines(line)
			if strings.HasPrefix(line, "WARNING:") {
				text = m.theme.style("warning").Render(text)
			}
			w.WriteString("  " + text + "\n")
		}
	}
	return w.String()
}

func (m *model) resourceTabKeys() string {
	if m.hasResourceDiagnostics() {
		return "1–5"
	}
	return "1–4"
}
