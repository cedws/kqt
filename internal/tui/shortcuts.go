package tui

import (
	"strings"

	"github.com/z0mbix/kqt/internal/kube"
)

// Keep shortcut labels separate from resource names and other user text, so
// only actual key hints receive highlighting.
type shortcut struct {
	key, description string
}

func (m *model) shortcuts(separator string, hints ...shortcut) string {
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, m.theme.style("accent").Bold(true).Render(kube.Safe(hint.key))+
			m.theme.style("muted").Render(": "+kube.Safe(hint.description)))
	}
	return strings.Join(parts, m.theme.style("muted").Render(separator))
}

func (m *model) footerHelp(d dimensions) string {
	hints := []shortcut{{"j/k", "move"}, {"[ / ]", "sort column"}, {"O", "next sort"}, {"R", "reverse"}, {"S", "status"}, {":", "command"}, {"?", "help"}, {"q", "quit"}}
	if d.width > 0 {
		hints = []shortcut{{"j/k", "move"}, {"Tab", "inspector"}, {"O", "next sort"}, {"R", "reverse"}, {"S", "status"}, {":", "command"}, {"?", "help"}}
	}
	if m.inspectorFocused {
		hints = []shortcut{{"j/k", "scroll"}, {"Ctrl+U/D", "page"}, {"g/G", "ends"}, {"Tab/Esc", "list"}, {"?", "help"}, {"q", "quit"}}
	}
	if m.mode == "command" {
		hints = []shortcut{{"↑/↓", "history"}, {"Ctrl+R", "search"}, {"Tab/Shift+Tab", "complete"}, {"Enter", "run"}, {"Esc", "cancel"}}
	} else if m.mode == "history-search" {
		return m.historySearchPreview()
	} else if m.mode == "action-confirm" {
		return m.theme.style("muted").Render(" Type the resource name shown  ") + m.shortcuts("  ", shortcut{"Enter", "confirm"}, shortcut{"Esc", "cancel"})
	} else if m.mode == "" && len(m.resourceForwards()) > 0 {
		hints = []shortcut{{"x", "stop forwards for resource"}, {"F", "manage forwards"}, {":", "command"}, {"?", "help"}, {"q", "quit"}}
	}
	if o, ok := m.current(); ok && o.GetKind() == "Secret" && (m.mode == "" || m.mode == "details") {
		action := "reveal"
		if m.secretTarget != "" {
			action = "hide"
		}
		hints = []shortcut{{"x", action + " values"}, {"y", "YAML"}, {"J", "JSON"}, {"Tab", "inspector"}, {"?", "help"}, {"Esc", "close"}}
		if m.mode == "details" {
			hints = []shortcut{{"x", action + " values"}, {"y", "YAML"}, {"J", "JSON"}, {"hjkl", "scroll"}, {"Esc", "close"}}
		} else if m.inspectorFocused {
			hints = []shortcut{{"x", action + " values"}, {"y", "YAML"}, {"J", "JSON"}, {"j/k", "scroll"}, {"Tab/Esc", "list"}}
		}
	}
	if m.showingLogs() {
		hints = []shortcut{{"w", "wrap"}, {"s", "follow"}, {"f", "focus"}, {"Drag", "copy"}, {"Space", "pause"}, {"/", "search"}, {"o", "save"}, {"F", "live/snapshot"}, {"Esc", "close"}}
	}
	if m.showingEventPanel() {
		hints = []shortcut{{"1–4", "tabs"}, {"Tab", "cycle"}, {"Space", "pause"}, {"/", "filter"}, {"o", "save"}, {"G", "follow"}, {"Ctrl+R", "reload"}, {"Esc", "close"}}
		if m.resourcePanelTab != resourceStatusTab {
			hints = []shortcut{{"1–4", "tabs"}, {"Tab/Shift+Tab", "cycle"}, {"j/k", "scroll"}, {"o", "save"}, {"Ctrl+R", "reload"}, {"Esc", "close"}}
		}
	} else if m.eventGlobal && m.mode == "" && !m.inspectorFocused {
		hints = []shortcut{{"Space", "pause"}, {"/", "filter"}, {"o", "save"}, {"G", "follow"}, {"0", "all namespaces"}, {":", "command"}, {"q", "quit"}}
	}
	if m.mode == "overview" {
		hints = []shortcut{{"j/k", "scroll"}, {"G/g", "ends"}, {"Enter", "explore problems"}, {"0", "all namespaces"}, {"n", "namespaces"}, {"Ctrl+R", "reload"}, {"Esc", "close"}}
	}
	if m.mode == "rollout-details" {
		hints = []shortcut{{"d", "compare"}, {"y", "template"}, {"r", "rollback"}, {"Ctrl+R", "reload"}, {"Esc", "revisions"}}
	}
	return " " + m.shortcuts("  ", hints...)
}
