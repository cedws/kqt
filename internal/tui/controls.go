package tui

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
)

func scroll(v *viewport.Model, key string) bool {
	switch key {
	case "j", "down":
		v.ScrollDown(1)
	case "k", "up":
		v.ScrollUp(1)
	case "ctrl+d":
		v.HalfPageDown()
	case "ctrl+u":
		v.HalfPageUp()
	case "pgdown", "ctrl+f":
		v.PageDown()
	case "pgup", "ctrl+b":
		v.PageUp()
	case "g", "home":
		v.GotoTop()
	case "G", "end":
		v.GotoBottom()
	case "h", "left":
		v.ScrollLeft(4)
	case "l", "right":
		v.ScrollRight(4)
	default:
		return false
	}
	return true
}
func (m *model) startInput(mode, value string) tea.Cmd {
	m.resize()
	m.mode = mode
	m.previousFilter = m.filter
	m.completion = completionState{}
	m.input.SetValue(value)
	m.input.CursorEnd()
	if mode == "command" {
		m.commandHistoryIndex = len(m.settings.Config.CommandHistory)
		m.commandDraft = value
		m.commandDraftCursor = m.input.Position()
	}
	m.input.Prompt = ":"
	if mode == "filter" {
		m.input.Prompt = "/"
	}
	return m.input.Focus()
}
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		m.close()
		return tea.Quit
	}
	if m.mode == "manifest-save" {
		return m.manifestInputKey(msg)
	}
	if m.mode == "action-confirm" {
		return m.actionInputKey(msg)
	}
	if m.mode == "event-search" || m.mode == "event-save" {
		return m.eventsInputKey(msg)
	}
	if m.mode == "log-search" || m.mode == "log-save" {
		return m.logsInputKey(msg)
	}
	if m.mode == "command" || m.mode == "filter" {
		if m.mode == "command" && (k == "up" || k == "down") {
			m.recallCommand(k == "up")
			return nil
		}
		if m.mode == "command" && (k == "tab" || k == "shift+tab") {
			m.completeCommand(k == "shift+tab")
			return nil
		}
		m.completion = completionState{}
		if k == "esc" {
			if m.mode == "filter" {
				m.filter = m.previousFilter
				m.rebuild("")
			}
			m.mode = ""
			m.input.Blur()
			return nil
		}
		if k == "enter" {
			mode, value := m.mode, m.input.Value()
			m.mode = ""
			m.input.Blur()
			if mode == "command" {
				cmd := m.command(strings.TrimSpace(value))
				m.rememberCommand(value)
				return cmd
			}
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.mode == "filter" {
			m.filter = m.input.Value()
			m.rebuild("")
		}
		return cmd
	}
	if m.mode == "picker" {
		return m.pickerKey(msg)
	}
	if m.mode == "edit-loading" || m.mode == "editing" {
		if k == "esc" || k == "q" {
			if m.mode == "action-running" {
				m.cancelAction()
				m.notice = "Action request cancelled; refresh to check the final state"
			}
			m.cancelEdit()
			m.mode = ""
			m.notice = "Edit cancelled"
			return m.fetch()
		}
		return nil
	}
	if m.mode != "" {
		if k == "esc" || k == "q" {
			m.cancelRelations()
			if m.mode == "details" {
				m.clearSecret()
				m.updateInspector()
			}
			m.closeLog()
			if !m.eventGlobal {
				m.closeEvents()
			}
			m.mode = ""
			m.resize()
			if m.route.resource.Kind == "Event" && m.eventFeed == nil {
				return m.followEventTable()
			}
			return nil
		}
		if m.mode == "logs" {
			switch k {
			case "/":
				return m.logsInput("log-search")
			case "o":
				return m.logsInput("log-save")
			case "f":
				m.logFollow = !m.logFollow
				m.logPrevious = false
				return m.loadLogs()
			case "space", " ":
				m.logPaused = !m.logPaused
				m.logTitle()
				if !m.logPaused {
					m.renderLogs()
				}
				return nil
			case "G", "end":
				m.logTail = true
			case "k", "up", "g", "home", "ctrl+u", "pgup", "ctrl+b":
				m.logTail = false
			}
			if k == "w" {
				m.panel.SoftWrap = !m.panel.SoftWrap
				return nil
			}
			if k == "ctrl+r" {
				return m.loadLogs()
			}
			if k == "p" {
				m.logPrevious = !m.logPrevious
				return m.loadLogs()
			}
			if k == "c" {
				return m.openPicker("containers")
			}
		}
		if m.mode == "events" {
			cmd := m.eventsKey(k)
			if cmd != nil || k == "space" || k == " " || k == "G" || k == "end" {
				return cmd
			}
		}
		if m.mode == "forward-details" && k == "x" {
			m.stopForward(m.forwardDetail)
			m.showForward(m.forwardDetail)
			return nil
		}
		if m.mode == "details" {
			switch k {
			case "C":
				return m.copyManifest(m.manifestFormat(), false)
			case "o":
				return m.promptManifestExport()
			case "x":
				if o, ok := m.current(); ok && (o.GetKind() == "Pod" || o.GetKind() == "Service") {
					m.stopResourceForwards()
					return nil
				}
				return m.toggleSecret()
			case "J":
				m.openDetails(false, true)
				return nil
			case "y", "v":
				m.openDetails(true, false)
				return nil
			case "e":
				return m.openEditor()
			}
		}
		scroll(&m.panel, k)
		return nil
	}
	if k == "q" {
		m.close()
		return tea.Quit
	}
	if m.eventGlobal && m.route.resource.Kind == "Event" {
		cmd := m.eventsKey(k)
		if cmd != nil || k == "space" || k == " " || k == "G" || k == "end" {
			return cmd
		}
	}
	if k == "x" {
		if o, ok := m.current(); ok && (o.GetKind() == "Pod" || o.GetKind() == "Service") {
			m.stopResourceForwards()
			return nil
		}
		return m.toggleSecret()
	}
	if k == "tab" || k == "shift+tab" {
		if m.inspectorDimensions().width > 0 {
			m.inspectorFocused = !m.inspectorFocused
			if m.eventGlobal && m.inspectorFocused {
				m.eventTail = false
			}
		}
		return nil
	}
	if m.inspectorFocused {
		if k == "J" {
			m.setInspectorFormat("json")
			return nil
		}
		if k == "y" {
			m.setInspectorFormat("yaml")
			return nil
		}
		if k == "esc" {
			m.inspectorFocused = false
			return nil
		}
		if scroll(&m.inspector, k) {
			return nil
		}
	}
	switch k {
	case "a":
		return m.openPicker("actions")
	case "D":
		return m.actionCommand("delete", "")
	case "C":
		return m.copyManifest(m.manifestFormat(), false)
	case "o":
		return m.promptManifestExport()
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "ctrl+d", "pgdown":
		m.move(max(1, (m.height-9)/2))
	case "ctrl+u", "pgup":
		m.move(-max(1, (m.height-9)/2))
	case "g", "home":
		m.move(-len(m.visible))
	case "G", "end":
		m.move(len(m.visible))
	case "h", "left":
		m.horizontal = max(0, m.horizontal-4)
	case "l", "right":
		m.horizontal += 4
	case ":":
		return m.startInput("command", "")
	case "/":
		return m.startInput("filter", m.filter)
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.rebuild("")
		} else if len(m.history) > 0 {
			r := m.history[len(m.history)-1]
			m.history = m.history[:len(m.history)-1]
			return m.navigate(r, false)
		}
	case "enter":
		return m.drillDown()
	case "d":
		m.openDetails(false, false)
	case "y", "v":
		m.openDetails(true, false)
	case "J":
		m.openDetails(false, true)
	case "e":
		return m.openEditor()
	case "b":
		return m.openRelations()
	case "L":
		return m.openLogs(false)
	case "P":
		return m.openLogs(true)
	case "S", "E":
		return m.openEvents()
	case "m":
		return m.toggleMetrics()
	case "f":
		return m.promptForward()
	case "F":
		return m.openPicker("forwards")
	case "n":
		return m.changeResource("namespaces", true)
	case "c":
		return m.openPicker("contexts")
	case "t":
		return m.openPicker("themes")
	case "r":
		return m.openPicker("resources")
	case "0":
		m.options.AllNamespaces = true
		m.options.Namespace = ""
		r := m.route
		r.namespace = ""
		return m.navigate(r, false)
	case "w":
		m.wide = !m.wide
	case "s":
		return m.openShell()
	case "R":
		m.setOrder(m.sortColumn, !m.descending)
	case "[", "shift+left":
		m.cycleOrder(-1)
	case "]", "shift+right":
		m.cycleOrder(1)
	case "O":
		m.cycleOrder(1)
	case "i":
		switch m.settings.Layouts[m.layout].Inspector {
		case "none":
			m.selectLayout("inspector")
		case "right":
			m.selectLayout("bottom")
		default:
			m.selectLayout("classic")
		}
	case "ctrl+r":
		if m.backend == nil {
			return m.connect(m.contextName)
		}
		return m.fetch()
	case "?":
		m.mode = "help"
		m.panelTitle = "Help"
		m.panel.SetContent(helpText)
		m.panel.GotoTop()
	}
	return nil
}
func (m *model) changeResource(name string, remember bool) tea.Cmd {
	r, err := kube.Resolve(m.resources, name)
	if err != nil {
		m.notice = kube.Safe(err.Error())
		return nil
	}
	target := m.route
	target.resource = r
	target.fields = ""
	target.labels = ""
	target.selection = ""
	return m.navigate(target, remember)
}
func (m *model) drillDown() tea.Cmd {
	o, ok := m.current()
	if !ok {
		return nil
	}
	if o.GetKind() == "Namespace" {
		m.options.AllNamespaces = false
		m.options.Namespace = o.GetName()
		r, _ := kube.Resolve(m.resources, "pods")
		return m.navigate(route{resource: r, namespace: o.GetName()}, true)
	}
	if selector, fields, ok := kube.PodSelector(o); ok {
		r, _ := kube.Resolve(m.resources, "pods")
		ns := o.GetNamespace()
		if o.GetKind() == "Node" {
			ns = ""
		}
		return m.navigate(route{resource: r, namespace: ns, labels: selector, fields: fields}, true)
	}
	m.openDetails(false, false)
	return nil
}
func (m *model) openDetails(manifest, asJSON bool) {
	_, ok := m.current()
	if !ok {
		return
	}
	m.mode = "details"
	m.panel.SoftWrap = false
	m.detailFormat = "describe"
	if manifest {
		m.detailFormat = "yaml"
	}
	if asJSON {
		m.detailFormat = "json"
	}
	m.renderDetails()
	m.panel.GotoTop()
}
func (m *model) renderDetails() {
	if m.mode != "details" {
		return
	}
	o, ok := m.current()
	if !ok {
		m.panelTitle = "Details"
		m.panel.SetContent("No resource selected.")
		return
	}
	offset := m.panel.YOffset()
	m.panelTitle = "Describe · " + o.GetName()
	content := m.describe(o)
	if m.detailFormat == "yaml" || m.detailFormat == "json" {
		asJSON := m.detailFormat == "json"
		m.panelTitle = "YAML · " + o.GetName()
		if asJSON {
			m.panelTitle = "JSON · " + o.GetName()
		}
		content = m.manifest(o, asJSON)
		if asJSON {
			content = m.theme.renderJSON(content)
		} else {
			content = m.theme.renderYAML(content)
		}
	}
	if o.GetKind() == "Secret" {
		format, _, _ := strings.Cut(m.panelTitle, " · ")
		m.panelTitle = format + " · " + m.secretLabel() + " · " + o.GetName()
	}
	m.panel.SetContent(content)
	m.panel.SetYOffset(offset)
}
func (m *model) openLogs(previous bool) tea.Cmd {
	o, ok := m.current()
	if !ok || !slices.Contains([]string{"Pod", "Deployment", "StatefulSet"}, o.GetKind()) || m.backend == nil {
		m.notice = "Select a pod, deployment, or statefulset to read logs (L)."
		return nil
	}
	m.logObject = o
	m.logPrevious = previous
	_, m.logFollow = m.backend.(kube.LogStreamer)
	containers := kube.Containers(o)
	if len(containers) == 0 {
		m.notice = "Resource has no containers."
		return nil
	}
	m.logContainer = containers[0]
	if len(containers) > 1 {
		return m.openPicker("containers")
	}
	return m.loadLogs()
}
func (m *model) command(value string) tea.Cmd {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil
	}
	cmd := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(value, cmd))
	m.notice = ""
	switch cmd {
	case "q", "quit":
		m.close()
		return tea.Quit
	case "ctx", "context", "contexts":
		if arg == "" {
			return m.openPicker("contexts")
		}
		if m.options.Demo {
			if arg != "demo" {
				m.notice = "Demo mode has one sample context."
				return nil
			}
		} else {
			found := false
			for _, c := range m.loader.Contexts() {
				if c.Name == arg {
					found = true
				}
			}
			if !found {
				m.notice = "Unknown context: " + kube.Safe(arg)
				return nil
			}
		}
		return m.connect(arg)
	case "ns", "namespace":
		if arg == "" {
			return m.changeResource("namespaces", true)
		}
		all := arg == "-A" || arg == "all" || arg == "*"
		if !all && len(validation.IsDNS1123Label(arg)) > 0 {
			m.notice = "Invalid namespace: " + kube.Safe(arg)
			return nil
		}
		m.options.AllNamespaces = all
		m.options.Namespace = arg
		r := m.route
		r.namespace = arg
		if all {
			r.namespace = ""
			m.options.Namespace = ""
		}
		r.fields = ""
		r.labels = ""
		return m.navigate(r, false)
	case "theme", "themes":
		if arg == "" {
			return m.openPicker("themes")
		}
		t, ok := m.settings.Themes[arg]
		if !ok {
			m.notice = "Unknown theme: " + kube.Safe(arg)
			return nil
		}
		return m.selectTheme(t)
	case "resources", "api":
		return m.openPicker("resources")
	case "actions":
		return m.openPicker("actions")
	case "scale", "restart", "suspend", "resume", "run", "delete":
		return m.actionCommand(cmd, arg)
	case "copy":
		return m.manifestCommand(arg, true)
	case "export":
		return m.manifestCommand(arg, false)
	case "related", "relations":
		return m.openRelations()
	case "layout", "layouts":
		if arg == "" {
			return m.openPicker("layouts")
		}
		if strings.HasPrefix(arg, "save ") {
			m.saveLayout(strings.TrimSpace(strings.TrimPrefix(arg, "save ")))
		} else {
			m.selectLayout(arg)
		}
	case "write":
		m.saveLayout(arg)
	case "inspector":
		switch arg {
		case "json", "yaml":
			m.setInspectorFormat(arg)
		case "right":
			m.selectLayout("inspector")
		case "bottom":
			m.selectLayout("bottom")
		case "none":
			m.selectLayout("classic")
		default:
			m.notice = "Use :inspector json, yaml, right, bottom, or none"
		}
	case "sort":
		return m.sortCommand(arg)
	case "columns":
		cols := strings.FieldsFunc(arg, func(r rune) bool { return r == ',' || r == ' ' })
		if len(cols) == 0 || len(cols) > 32 {
			m.notice = "Use :columns name,status,age (up to 32 columns)"
			return nil
		}
		for _, c := range cols {
			if !validColumn(c) {
				m.notice = "Invalid column: " + kube.Safe(c)
				return nil
			}
		}
		m.saveColumns(viewConfig{Columns: cols, Sort: m.sortColumn, Descending: m.descending})
		m.horizontal = 0
	case "label", "labels":
		if _, err := labels.Parse(arg); err != nil {
			m.notice = kube.Safe(err.Error())
			return nil
		}
		r := m.route
		r.labels = arg
		return m.navigate(r, false)
	case "logs":
		return m.openLogs(arg == "previous")
	case "events":
		if arg == "selected" {
			return m.openEvents()
		}
		return m.changeResource("events", true)
	case "status":
		return m.openEvents()
	case "metrics":
		if arg == "on" {
			m.metricsEnabled = false
		} else if arg == "off" {
			m.metricsEnabled = true
		} else if arg != "" {
			m.notice = "Use :metrics on or off"
			return nil
		}
		return m.toggleMetrics()
	case "shell":
		return m.openShell()
	case "forward":
		if arg == "" {
			return m.promptForward()
		}
		return m.forwardCommand(arg)
	case "forwards":
		return m.openPicker("forwards")
	case "edit":
		return m.openEditor()
	case "refresh":
		if m.backend == nil {
			return m.connect(m.contextName)
		}
		return m.fetch()
	case "help":
		m.mode = "help"
		m.panelTitle = "Help"
		m.panel.SetContent(helpText)
		m.panel.GotoTop()
	default:
		return m.changeResource(cmd, true)
	}
	return nil
}
func (m *model) openPicker(kind string) tea.Cmd {
	m.pickerReturn = ""
	if kind == "containers" && m.mode == "logs" {
		m.pickerReturn = "logs"
	}
	m.previousTheme = m.theme
	m.pickerKind = kind
	m.mode = "picker"
	m.choices = nil
	m.pickerIndex = 0
	switch kind {
	case "actions":
		if o, ok := m.current(); ok {
			for _, action := range kube.Actions(o.GetKind()) {
				m.choices = append(m.choices, choice{action, "requires confirmation"})
			}
		}
	case "relations":
		for _, link := range m.relations {
			m.choices = append(m.choices, choice{link.Key(), link.Title() + " · " + link.Reason})
		}
	case "sort":
		for i, column := range m.columns() {
			description := "sort ascending"
			if column == m.sortColumn {
				direction, next := "ascending", "descending"
				if m.descending {
					direction, next = next, direction
				}
				description = direction + " · select to sort " + next
				m.pickerIndex = i
			}
			m.choices = append(m.choices, choice{column, description})
		}
	case "forwards":
		m.refreshForwardChoices()
	case "shell-containers":
		for _, c := range kube.RunningContainers(m.shellObject) {
			m.choices = append(m.choices, choice{c, "running pod container"})
		}
	case "themes":
		for _, name := range themeNames(m.settings.Themes) {
			m.choices = append(m.choices, choice{name, "terminal palette"})
		}
	case "layouts":
		var names []string
		for name := range m.settings.Layouts {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			p := m.settings.Layouts[name]
			description := "inspector: " + p.Inspector
			if name == m.layout {
				description += " · active"
			}
			m.choices = append(m.choices, choice{name, description})
		}
	case "contexts":
		if m.options.Demo {
			m.choices = []choice{{"demo", "offline sample cluster"}}
		} else {
			for _, c := range m.loader.Contexts() {
				m.choices = append(m.choices, choice{c.Name, c.Cluster + " · " + c.Namespace})
			}
		}
	case "resources":
		for _, r := range m.resources {
			scope := "cluster"
			if r.Namespaced {
				scope = "namespaced"
			}
			m.choices = append(m.choices, choice{r.ID(), r.Kind + " · " + scope})
		}
	case "containers":
		for _, c := range kube.Containers(m.logObject) {
			m.choices = append(m.choices, choice{c, "pod container"})
		}
		if len(kube.Containers(m.logObject)) > 1 {
			m.choices = append(m.choices, choice{"<all containers>", "combine all pod containers"})
		}
	}
	m.input.Prompt = "/ "
	m.input.SetValue("")
	m.resize()
	m.filterChoices()
	return m.input.Focus()
}
func (m *model) filterChoices() {
	m.matches = nil
	q := m.input.Value()
	type rankedChoice struct {
		choice choice
		score  int
	}
	var matches []rankedChoice
	for _, c := range m.choices {
		if score, ok := fuzzyScore(q, c.name+" "+c.description); ok {
			matches = append(matches, rankedChoice{c, score})
		}
	}
	if q != "" {
		slices.SortStableFunc(matches, func(a, b rankedChoice) int { return b.score - a.score })
	}
	for _, c := range matches {
		m.matches = append(m.matches, c.choice)
	}
	m.pickerIndex = min(m.pickerIndex, max(0, len(m.matches)-1))
}
func (m *model) pickerKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if m.pickerKind == "themes" {
			m.setTheme(m.previousTheme)
		}
		m.mode = m.pickerReturn
		m.input.Blur()
		return nil
	case "up", "ctrl+k":
		m.pickerIndex = max(0, m.pickerIndex-1)
	case "down", "ctrl+j":
		m.pickerIndex = min(max(0, len(m.matches)-1), m.pickerIndex+1)
	case "pgup":
		m.pickerIndex = max(0, m.pickerIndex-8)
	case "pgdown":
		m.pickerIndex = min(max(0, len(m.matches)-1), m.pickerIndex+8)
	case "x":
		if m.pickerKind == "forwards" && len(m.matches) > 0 {
			id, _ := strconv.ParseUint(m.matches[m.pickerIndex].name, 10, 64)
			m.stopForward(id)
			m.refreshForwardChoices()
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filterChoices()
		return cmd
	case "enter":
		if len(m.matches) == 0 {
			return nil
		}
		c := m.matches[m.pickerIndex]
		m.mode = ""
		m.input.Blur()
		switch m.pickerKind {
		case "actions":
			return m.actionCommand(c.name, "")
		case "relations":
			return m.selectRelation(c.name)
		case "sort":
			m.chooseOrder(c.name)
		case "shell-containers":
			m.shellContainer = c.name
			return m.runShell()
		case "forwards":
			id, _ := strconv.ParseUint(c.name, 10, 64)
			m.showForward(id)
			return nil
		case "themes":
			return m.selectTheme(m.settings.Themes[c.name])
		case "layouts":
			m.selectLayout(c.name)
		case "contexts":
			return m.connect(c.name)
		case "resources":
			return m.changeResource(c.name, true)
		case "containers":
			m.logContainer = c.name
			if c.name == "<all containers>" {
				m.logContainer = "*"
			}
			return m.loadLogs()
		}
		return nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.pickerIndex = 0
		m.filterChoices()
		return cmd
	}
	if m.pickerKind == "themes" && len(m.matches) > 0 {
		m.setTheme(m.settings.Themes[m.matches[m.pickerIndex].name])
	}
	return nil
}

const helpText = `kqt · A Kubernetes Query Tool

Navigation
  j/k, arrows        Move through resources
  g/G, Home/End      First / last row
  Ctrl+U/D, PgUp/Dn  Page rows or panels
  h/l, left/right    Scroll wide tables and manifests
  Shift+Left/Right   Sort previous / next column (also [ / ])
  O / R              Sort next column / reverse current order
  Mouse              Click column to sort, click row to select, wheel to scroll
  Enter              Workload/service/node -> pods; namespace -> scope
  Escape             Close, clear filter, or go back
  Tab                Focus inspector (outside a command)

Explore
  :                  Command prompt; Tab / Shift+Tab cycle completions
  Up / Down          Recall last 50 commands in the command prompt
  /                  Fuzzy search: web0003 matches web-7c9f6d8b5-0003
  n / :ns            Browse namespaces; Enter selects one
  :ns NAME / :ns -A   Set namespace / all namespaces (also 0)
  c / :ctx           Search kubeconfig contexts (session only)
  r / :resources     Search discovered APIs / CRDs
  b / :related       Browse owners, backends, pods and storage relationships
  a / :actions       Scale/restart workloads; suspend/resume/run CronJobs
  D / :delete        Confirm deletion of the selected resource
  :po :deploy :svc    Resource names, kinds, and aliases
  :labels app=web    Kubernetes label selector; :labels clears it
  d                  Describe resource with highlighted YAML
  C / o              Copy / export selected manifest (also in YAML/JSON)
  :copy json clean   Copy reusable JSON; :export yaml clean PATH saves YAML
  y / v, J           Open highlighted YAML / JSON
  Inspector: J / y   Switch focused inspector to JSON / YAML
  Details: J / y     Switch open manifest panel to JSON / YAML
  Secrets: x         Toggle decoded values; hidden by default
  L / P              Logs / previous logs: pod, deployment, statefulset
  S / :status        Status, conditions, containers, and live events (E also works)
  :events            Live namespace event table (0 = all namespaces)
  Events             Space pause, / filter, G follow latest, o save
  m / :metrics       Toggle pod/node CPU and memory columns
  s / :shell         Shell in a running pod/container; Ctrl+] disconnects
  f                  Forward a pod/service port (LOCAL:REMOTE; LOCAL 0 auto)
  x (pod/service)    Stop forwards for the selected resource
  F / :forwards      Manage forwards; x stops the selected forward
  Logs: c / p / w    Select container / previous instance / wrap; Ctrl+R reloads
  e / :edit          Edit fresh resource in configured editor or $EDITOR

Appearance and saved settings
  t / :themes        Fuzzy theme picker; Escape cancels previews
  :layouts           Search and apply saved layout profiles
  :layout NAME       Apply layout directly
  :write NAME        Save current arrangement and columns as a layout
  i                  Cycle right / bottom / classic inspector layouts
  :inspector json    Also accepts yaml, right, bottom, none
  w                  Wide fields
  :sort              Column picker; :sort COLUMN asc|desc sets an order
  :columns name,status,age   Configure columns for the active layout
  /spec/replicas     JSON Pointer column (also supported in config)
  Pickers            Type to search; arrows / Ctrl+J/K move; Enter selects

Refresh and exit
  Ctrl+R / :refresh  Refresh resources or retry connection
  ? / :help          This help
  q / :q / Ctrl+C    Quit

View and namespace are saved per context in ~/.config/kqt/config.yaml.
Themes, layouts and inspector format are saved when applied.
The theme name is shown for ten seconds after selection.
Kubeconfig is never modified. Secret values are hidden by default; x reveals
decoded values for the selected Secret. x, closing details, selection/context
changes, and editing hide them again. The reveal state is never saved.
Editing reads a fresh full object, applies a versioned patch on saved changes,
and retains the private file on errors/conflicts. Unchanged edits do not write.
Live logs: Space pauses, / searches, o saves, f switches live/snapshot.
Snapshots: last 500 lines / 1 MiB per source; aggregate at most 4 MiB.
Live logs retain 4 MiB / 10,000 lines and follow at most 64 containers.
Metrics require the cluster Metrics API. cpu%/memory% compare pod limits
or node allocatable capacity; missing limits/samples display a dash.
Tables refresh every 2s. Forwarding binds 127.0.0.1 and ends on context
switch or quit. :forward stop ID / all stops a forward without opening it.
`
