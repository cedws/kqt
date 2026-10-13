package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
)

func fit(text string, width int) string {
	width = max(0, width)
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}
func (m *model) frame(title, content string, width, height int, focused bool) string {
	width = max(2, width)
	height = max(2, height)
	inner := width - 2
	top := ansi.Truncate("─ "+kube.Safe(title)+" ", inner, "")
	top += strings.Repeat("─", max(0, inner-ansi.StringWidth(top)))
	b := m.theme.style("border")
	if focused {
		b = m.theme.style("focus").Bold(true)
	}
	lines := []string{b.Render("╭" + top + "╮")}
	parts := strings.Split(content, "\n")
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(parts) {
			line = parts[i]
		}
		lines = append(lines, b.Render("│")+fit(line, inner)+b.Render("│"))
	}
	lines = append(lines, b.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(lines, "\n")
}

type dimensions struct {
	width, height int
	bottom        bool
}

func (m *model) inspectorDimensions() dimensions {
	body := m.height - 6
	p := m.settings.Layouts[m.layout]
	if p.Inspector == "right" && m.width >= 120 && body >= 8 {
		return dimensions{m.width * p.InspectorPercent / 100, body, false}
	}
	if p.Inspector == "bottom" && m.height >= 24 {
		return dimensions{m.width, max(7, body*p.InspectorPercent/100), true}
	}
	return dimensions{}
}
func (m *model) resize() {
	m.input.SetWidth(max(1, m.width-8))
	if m.mode == "picker" {
		m.input.SetWidth(max(1, min(90, m.width-4)-6))
	}
	panelWidth, panelHeight := m.width-6, m.height-6
	if m.showingLogs() || m.showingEventPanel() {
		panelWidth, panelHeight = m.width-2, m.height-8
	}
	m.panel.SetWidth(max(1, panelWidth))
	m.panel.SetHeight(max(1, panelHeight))
	d := m.inspectorDimensions()
	m.inspector.SetWidth(max(1, d.width-2))
	m.inspector.SetHeight(max(1, d.height-2))
	if d.width == 0 {
		m.inspectorFocused = false
	}
}
func (m *model) statusToken(phase string) string {
	switch strings.ToLower(phase) {
	case "ready", "running", "active", "bound", "succeeded", "normal":
		return "success"
	case "failed", "notready", "crashloopbackoff", "imagepullbackoff", "errimagepull", "error", "oomkilled":
		return "error"
	case "pending", "progressing", "terminating", "containercreating", "warning":
		return "warning"
	}
	return "foreground"
}
func heading(c string) string {
	if strings.HasPrefix(c, "/") {
		parts := strings.Split(c, "/")
		return strings.ToUpper(parts[len(parts)-1])
	}
	return strings.ToUpper(c)
}

type tableGeometry struct {
	columns                  []string
	widths                   []int
	total, offset, dataWidth int
}

func (m *model) tableGeometry(width int) tableGeometry {
	cols := m.columns()
	widths := make([]int, len(cols))
	desired := make([]int, len(cols))
	total := 2
	inner := width - 2
	for i, c := range cols {
		widths[i] = min(12, max(4, ansi.StringWidth(heading(c))+2))
		desired[i] = widths[i]
		for _, o := range m.visible {
			desired[i] = max(desired[i], min(64, ansi.StringWidth(kube.Safe(m.column(o, c)))))
		}
		if c == "name" {
			desired[i] = min(64, desired[i]+1)
		}
		if m.wide {
			widths[i] = desired[i]
		}
		total += widths[i] + 1
	}
	if !m.wide {
		for i, c := range cols {
			if c == "name" {
				growth := min(desired[i]-widths[i], max(0, inner-total))
				widths[i] += growth
				total += growth
			}
		}
		for total < inner {
			changed := false
			for i := range widths {
				if widths[i] < desired[i] {
					widths[i]++
					total++
					changed = true
					if total >= inner {
						break
					}
				}
			}
			if !changed {
				break
			}
		}
	}
	x := min(m.horizontal, max(0, total-inner))
	return tableGeometry{cols, widths, total, x, max(0, inner-2)}
}

func tableStart(selected, count, capacity int) int {
	return min(max(0, selected-capacity+1), max(0, count-capacity))
}

func (m *model) table(width, height int) string {
	geometry := m.tableGeometry(width)
	cols, widths, total, x := geometry.columns, geometry.widths, geometry.total, geometry.offset
	inner := width - 2
	chosen := m.orderColumn()
	var cells []string
	for i, c := range cols {
		h := heading(c)
		if c == m.sortColumn {
			if m.descending {
				h += " ↓"
			} else {
				h += " ↑"
			}
		}
		cell := fit(h, widths[i])
		if c == chosen {
			cell = m.theme.style("focus").Bold(true).Underline(true).Render(cell)
		}
		cells = append(cells, cell)
	}
	lines := []string{m.theme.style("accent").Bold(true).Render(fit("  "+ansi.Cut(strings.Join(cells, " "), x, x+max(0, inner-2)), inner))}
	capacity := max(0, height-3)
	start := tableStart(m.selected, len(m.visible), capacity)
	for i := start; i < min(len(m.visible), start+capacity); i++ {
		o := m.visible[i]
		cells = nil
		for j, c := range cols {
			value := fit(kube.Safe(m.column(o, c)), widths[j])
			if (c == "status" || o.GetKind() == "Event" && c == "type") && i != m.selected {
				value = m.theme.style(m.statusToken(kube.Status(o))).Render(value)
			}
			cells = append(cells, value)
		}
		marker := "  "
		if i == m.selected {
			marker = "› "
		}
		line := fit(marker+ansi.Cut(strings.Join(cells, " "), x, x+max(0, inner-2)), inner)
		if i == m.selected {
			line = lipgloss.NewStyle().Foreground(m.theme.color("selection_foreground")).Background(m.theme.color("selection_background")).Bold(true).Render(line)
		}
		lines = append(lines, line)
	}
	if len(m.visible) == 0 && capacity > 0 {
		text := "No resources in this scope"
		if m.busy {
			text = "Connecting / loading…"
		} else if m.filter != "" {
			text = "No resources match this filter"
		} else if m.dataErr != "" {
			text = "Unable to load resources · Ctrl+R retries"
		} else if m.contextName == "" {
			text = "Choose a context with c"
		}
		lines = append(lines, m.theme.style("muted").Render(text))
	}
	title := m.route.resource.ID()
	if title == "" {
		title = "resources"
	}
	title += fmt.Sprintf(" [%d]", len(m.visible))
	if m.eventGlobal && m.route.resource.Kind == "Event" {
		title += " · " + m.eventViewState()
	}
	if total > inner {
		title += fmt.Sprintf(" · scroll %d/%d", x, total-inner)
	}
	return m.frame(title, strings.Join(lines, "\n"), width, height, !m.inspectorFocused)
}
func (m *model) inspectorView(d dimensions) string {
	title := "Inspector · " + strings.ToUpper(m.inspectorFormat) + " · Tab focus"
	if m.inspectorFocused {
		title = fmt.Sprintf("Inspector · focused · %s · %.0f%%", strings.ToUpper(m.inspectorFormat), m.inspector.ScrollPercent()*100)
	}
	if o, ok := m.current(); ok && o.GetKind() == "Secret" {
		title = "Inspector · " + m.secretLabel() + " · " + strings.ToUpper(m.inspectorFormat)
		if m.inspectorFocused {
			title = "Inspector · focused · " + m.secretLabel() + " · " + strings.ToUpper(m.inspectorFormat)
		}
	}
	return m.frame(title, m.inspector.View(), d.width, d.height, m.inspectorFocused)
}
func (m *model) pickerView(width, height int) string {
	var lines []string
	lines = append(lines, fit(m.input.View(), width-2), "")
	capacity := max(1, height-6)
	start := min(max(0, m.pickerIndex-capacity+1), max(0, len(m.matches)-capacity))
	for i := start; i < min(len(m.matches), start+capacity); i++ {
		c := m.matches[i]
		label := kube.Safe(c.name) + " · " + kube.Safe(c.description)
		if m.pickerKind == "relations" {
			label = kube.Safe(c.description)
		}
		text := "  " + label
		if i == m.pickerIndex {
			text = "› " + label
			text = lipgloss.NewStyle().Foreground(m.theme.color("selection_foreground")).Background(m.theme.color("selection_background")).Render(fit(text, width-2))
		}
		lines = append(lines, text)
	}
	if len(m.matches) == 0 {
		lines = append(lines, "No matches")
	}
	lines = append(lines, "", " Type to search · ↑/↓ Ctrl+J/K move · Enter select · Esc close")
	if m.pickerKind == "forwards" {
		lines[len(lines)-1] = " Type to search · ↑/↓ select · x stop · Esc close"
	}
	return m.frame(strings.Title(m.pickerKind), strings.Join(lines, "\n"), width, height, true)
}
func (m *model) View() tea.View {
	w, h := m.width, m.height
	var content string
	if w < 40 || h < 12 {
		content = fit("kqt needs at least 40×12. Resize or press q.", w)
	} else {
		ns := m.route.namespace
		if ns == "" {
			ns = "all"
		}
		ctx := m.contextName
		if ctx == "" {
			ctx = "select context"
		}
		state := "connected"
		if m.backend == nil {
			state = "disconnected"
		}
		if m.busy && (m.backend == nil || m.lastRefresh.IsZero()) {
			state = "loading"
		}
		if m.dataErr != "" {
			state = "stale / disconnected"
		}
		brand := " kqt  ·  A Kubernetes Query Tool"
		if m.options.Demo {
			brand += "  ·  DEMO"
		}
		connectionLine := " " + kube.Safe(m.server) + " · " + state
		if time.Now().Before(m.themeUntil) {
			connectionLine += " · " + m.theme.Name
		}
		lines := []string{m.theme.style("accent").Bold(true).Render(fit(brand, w)), m.theme.style("foreground").Render(fit(" Context: "+kube.Safe(ctx)+"  ·  Namespace: "+kube.Safe(ns)+"  ·  Explorer · e edit", w)), m.theme.style("muted").Render(fit(connectionLine, w))}
		breadcrumb := " " + m.route.resource.ID()
		if m.route.labels != "" {
			breadcrumb += " · " + kube.Safe(m.route.labels)
		}
		if m.route.fields != "" {
			breadcrumb += " · " + kube.Safe(m.route.fields)
		}
		if m.filter != "" {
			breadcrumb += " / " + kube.Safe(m.filter)
		}
		if m.wide {
			breadcrumb += " · wide"
		}
		lines = append(lines, m.theme.style("foreground").Render(fit(breadcrumb, w)))
		bodyHeight := h - 6
		d := m.inspectorDimensions()
		body := ""
		if m.showingLogs() || m.showingEventPanel() {
			title := m.panelTitle + " · Esc close · hjkl scroll · Ctrl+R reload"
			if m.showingLogs() {
				title = m.panelTitle + " · Esc close · hjkl scroll · c container · p previous · Ctrl+R reload"
			}
			body = m.frame(title, m.panel.View(), w, bodyHeight, true)
			body = lipgloss.NewStyle().Background(m.theme.color("surface")).Foreground(m.theme.color("foreground")).Render(body)
		} else if d.width > 0 {
			if d.bottom {
				body = m.table(w, bodyHeight-d.height) + "\n" + m.inspectorView(d)
			} else {
				body = lipgloss.JoinHorizontal(lipgloss.Top, m.table(w-d.width, bodyHeight), m.inspectorView(d))
			}
		} else {
			body = m.table(w, bodyHeight)
		}
		lines = append(lines, body)
		status, token := m.notice, "warning"
		if status == "" {
			status = m.metricsWarning
		}
		if status == "" {
			status = m.discoveryWarning
		}
		if status == "" {
			status = fmt.Sprintf(" %d/%d resources · sort %s · %s", len(m.visible), len(m.rows), m.sortColumn, m.layout)
			if !m.lastRefresh.IsZero() {
				status += " · refreshed " + m.lastRefresh.Format("15:04:05")
			}
			token = "muted"
		}
		if m.dataErr != "" {
			status = " STALE · " + m.dataErr
			token = "error"
		}
		if m.secretTarget != "" {
			status = " Secret " + m.secretLabel() + " · " + status
			if m.dataErr == "" {
				token = "warning"
			}
		}
		if m.eventGlobal && m.route.resource.Kind == "Event" && m.notice == "" {
			status = " Events · " + m.eventViewState()
			if m.eventError != "" {
				status += " · " + m.eventError
				token = "warning"
			}
			if dropped := m.eventDropped + m.eventStore.dropped; dropped > 0 {
				status += fmt.Sprintf(" · %d records omitted", dropped)
			}
			if m.eventGaps > 0 {
				status += " · resynced; history may have gaps"
			}
		}
		if m.mode == "command" || m.mode == "filter" || m.mode == "log-search" || m.mode == "log-save" || m.mode == "event-search" || m.mode == "event-save" {
			status = m.input.View()
			token = "foreground"
		}
		lines = append(lines, m.theme.style(token).Render(fit(status, w)))
		help := " j/k move  [/] column  O next sort  R reverse  S status  : command  ? help  q quit"
		if d.width > 0 {
			help = " j/k move  Tab inspector  O next sort  R reverse  S status  : command  ? help"
		}
		if m.inspectorFocused {
			help = " j/k scroll  Ctrl+U/D page  g/G ends  Tab/Esc list  ? help  q quit"
		}
		if m.mode == "command" {
			help = " ↑/↓ history  Tab/Shift+Tab complete  Enter run  Esc cancel"
		} else if m.mode == "" && len(m.resourceForwards()) > 0 {
			help = " x stop forwards for resource  F manage forwards  : command  ? help  q quit"
		}
		if o, ok := m.current(); ok && o.GetKind() == "Secret" && (m.mode == "" || m.mode == "details") {
			action := "reveal"
			if m.secretTarget != "" {
				action = "hide"
			}
			help = " x " + action + " values  y YAML  J JSON  Tab inspector  ? help  Esc close"
			if m.mode == "details" {
				help = " x " + action + " values  y YAML  J JSON  hjkl scroll  Esc close"
			} else if m.inspectorFocused {
				help = " x " + action + " values  y YAML  J JSON  j/k scroll  Tab/Esc list"
			}
		}
		if m.showingLogs() {
			help = " Space pause  / search  f live/snapshot  o save  c container  p previous  Esc close"
		}
		if m.showingEventPanel() {
			help = " Space pause  / filter  o save  G follow  Ctrl+R reload  Esc close"
		} else if m.eventGlobal && m.mode == "" && !m.inspectorFocused {
			help = " Space pause  / filter  o save  G follow  0 all namespaces  : command  q quit"
		}
		lines = append(lines, m.theme.style("muted").Render(fit(help, w)))
		content = strings.Join(lines, "\n")
		if m.mode == "picker" || m.mode == "details" || m.mode == "help" || m.mode == "edit-error" || m.mode == "operation-error" || m.mode == "forward-details" || m.mode == "relations-loading" {
			mw, mh := w-4, h-4
			modal := ""
			if m.mode == "picker" {
				mw = min(90, mw)
				mh = min(24, mh)
				modal = m.pickerView(mw, mh)
			} else {
				title := kube.Safe(m.panelTitle) + " · Esc close · hjkl scroll"
				modal = m.frame(title, m.panel.View(), mw, mh, true)
			}
			modal = lipgloss.NewStyle().Background(m.theme.color("surface")).Foreground(m.theme.color("foreground")).Render(modal)
			content = lipgloss.NewCompositor(lipgloss.NewLayer(content), lipgloss.NewLayer(modal).X((w-mw)/2).Y((h-mh)/2).Z(1)).Render()
		}
	}
	v := tea.NewView(content)
	v.AltScreen = true
	if w >= 40 && h >= 12 && m.mode != "shell" && m.mode != "editing" && m.mode != "edit-loading" {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if m.theme.Colors["background"] != "default" {
		v.BackgroundColor = m.theme.color("background")
	}
	if m.theme.Colors["foreground"] != "default" {
		v.ForegroundColor = m.theme.color("foreground")
	}
	return v
}
