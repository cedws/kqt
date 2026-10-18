package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type helpLayout struct {
	shortcuts, notes paneBounds
	split            bool
}

// These content bounds also drive mouse scrolling, focus and copy selection.
func (m *model) helpPanes() helpLayout {
	modal := m.modalBounds()
	content := paneBounds{modal.x + 1, modal.y + 2, max(1, modal.width-2), max(1, modal.height-3)}
	if content.width < 74 {
		return helpLayout{content, content, false}
	}
	right := min(52, max(28, (content.width-3)*2/5))
	left := content.width - right - 3
	return helpLayout{
		shortcuts: paneBounds{content.x, content.y, left, content.height},
		notes:     paneBounds{content.x + left + 3, content.y, right, content.height},
		split:     true,
	}
}

func (m *model) helpPaneHeading(label string, width int, active bool, pane *viewport.Model) string {
	marker := "  "
	style := m.theme.style("muted")
	if active {
		marker = "› "
		style = m.theme.style("focus").Bold(true).Underline(true)
	}
	text := marker + label
	if pane.TotalLineCount() > pane.Height() {
		text += fmt.Sprintf(" · %.0f%%", pane.ScrollPercent()*100)
	}
	return style.Render(fit(text, width))
}

func (m *model) helpView() string {
	panes := m.helpPanes()
	leftHeading := m.helpPaneHeading("Shortcuts", panes.shortcuts.width, !m.helpNotesFocused, &m.panel)
	rightHeading := m.helpPaneHeading("Notes", panes.notes.width, m.helpNotesFocused, &m.helpNotes)
	if !panes.split {
		if m.helpNotesFocused {
			return rightHeading + "\n" + m.helpNotes.View()
		}
		return leftHeading + "\n" + m.panel.View()
	}
	left := strings.Split(leftHeading+"\n"+m.panel.View(), "\n")
	right := strings.Split(rightHeading+"\n"+m.helpNotes.View(), "\n")
	divider := m.theme.style("border").Render(" │ ")
	rows := make([]string, panes.shortcuts.height+1)
	for i := range rows {
		a, b := "", ""
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		rows[i] = fit(a, panes.shortcuts.width) + divider + fit(b, panes.notes.width)
	}
	return strings.Join(rows, "\n")
}

func (m *model) helpPaneAt(x, y int) (paneBounds, *viewport.Model, bool) {
	panes := m.helpPanes()
	if !panes.split {
		if panes.shortcuts.contains(x, y) {
			return panes.shortcuts, m.helpActivePane(), true
		}
		return paneBounds{}, nil, false
	}
	if panes.shortcuts.contains(x, y) {
		return panes.shortcuts, &m.panel, true
	}
	if panes.notes.contains(x, y) {
		return panes.notes, &m.helpNotes, true
	}
	return paneBounds{}, nil, false
}

func (m *model) helpClick(v tea.MouseClickMsg) {
	panes := m.helpPanes()
	if !panes.split {
		return
	}
	// Pane headings are clickable as well as the scrolling content.
	panes.shortcuts.y--
	panes.shortcuts.height++
	panes.notes.y--
	panes.notes.height++
	if panes.shortcuts.contains(v.X, v.Y) {
		m.helpNotesFocused = false
	}
	if panes.notes.contains(v.X, v.Y) {
		m.helpNotesFocused = true
	}
}
