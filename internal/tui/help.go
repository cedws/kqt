package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
)

type helpMatch struct {
	section, line string
	score         int
	notes         bool
}

func matchingHelp(query string) []helpMatch {
	lines := strings.Split(helpText, "\n")
	var matches []helpMatch
	section := ""
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		if i+1 < len(lines) && !strings.HasPrefix(line, "  ") && strings.HasPrefix(lines[i+1], "  ") {
			section = line
			continue
		}
		group := section
		if !strings.HasPrefix(line, "  ") {
			group = "Notes"
		}
		score, ok := fuzzyScore(query, strings.TrimSpace(line))
		if groupScore, groupOK := fuzzyScore(query, group); groupOK {
			if !ok || groupScore > score {
				score = groupScore
			}
			ok = true
		}
		if ok {
			matches = append(matches, helpMatch{section: group, line: line, score: score})
		}
	}
	for _, block := range strings.Split(strings.TrimSpace(helpNotesText), "\n\n") {
		section, text, _ := strings.Cut(block, "\n")
		text = strings.Join(strings.Fields(text), " ")
		score, ok := fuzzyScore(query, text)
		for _, heading := range []string{section, "Notes"} {
			if headingScore, headingOK := fuzzyScore(query, heading); headingOK {
				if !ok || headingScore > score {
					score = headingScore
				}
				ok = true
			}
		}
		if ok {
			matches = append(matches, helpMatch{section: section, line: text, score: score, notes: true})
		}
	}
	slices.SortStableFunc(matches, func(a, b helpMatch) int { return b.score - a.score })
	return matches
}

func (m *model) helpLine(line string) string {
	if strings.HasPrefix(line, "  ") {
		// Retain the help table's padding and semantic key/description colours.
		if key, description, ok := strings.Cut(line[2:], "  "); ok {
			return "  " + m.shortcuts("", shortcut{key, description})
		}
	}
	return line
}

func (m *model) renderHelp() string {
	if strings.TrimSpace(m.helpQuery) != "" {
		matches := matchingHelp(strings.TrimSpace(m.helpQuery))
		if len(matches) == 0 {
			if m.mode == "help-search" {
				return "No help matches this search.\n\nKeep typing, or clear the search to show all help."
			}
			return "No help matches this search.\n\nPress / to change or clear the search."
		}
		// Keep related results together, with the best matching section first.
		var groups []string
		grouped := map[string][]string{}
		for _, match := range matches {
			if match.notes {
				continue
			}
			if _, ok := grouped[match.section]; !ok {
				groups = append(groups, match.section)
			}
			grouped[match.section] = append(grouped[match.section], m.helpLine(match.line))
		}
		if len(groups) == 0 {
			return "No shortcuts match this search.\n\nMatching information is in Notes."
		}
		var blocks []string
		for _, group := range groups {
			blocks = append(blocks, group+"\n"+strings.Join(grouped[group], "\n"))
		}
		return strings.Join(blocks, "\n\n")
	}
	lines := strings.Split(helpText, "\n")
	for i, line := range lines {
		lines[i] = m.helpLine(line)
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderHelpNotes() string {
	var blocks []string
	for _, match := range matchingHelp(strings.TrimSpace(m.helpQuery)) {
		if !match.notes {
			continue
		}
		paragraph := ansi.Wrap(match.line, max(1, m.helpNotes.Width()), "")
		blocks = append(blocks, m.theme.style("accent").Bold(true).Render(match.section)+"\n"+m.theme.style("muted").Render(paragraph))
	}
	if len(blocks) == 0 {
		return m.theme.style("muted").Render("No notes match this search.")
	}
	return strings.Join(blocks, "\n\n")
}

func (m *model) showingHelp() bool { return m.mode == "help" || m.mode == "help-search" }

func (m *model) openHelp() tea.Cmd {
	m.helpQuery, m.helpPreviousQuery = "", ""
	m.helpNotesFocused = false
	m.mode = "help"
	m.input.Blur()
	m.panel.SoftWrap = true
	m.resize()
	m.refreshHelp(true)
	return nil
}

func (m *model) refreshHelp(top bool) {
	m.panelTitle = "Help"
	if query := strings.TrimSpace(m.helpQuery); query != "" {
		m.panelTitle = fmt.Sprintf("Help · %d matches · /%s", len(matchingHelp(query)), kube.Safe(query))
	}
	offset := m.panel.YOffset()
	notesOffset := m.helpNotes.YOffset()
	m.panel.SetContent(m.renderHelp())
	m.helpNotes.SetContent(m.renderHelpNotes())
	if top {
		m.panel.GotoTop()
		m.helpNotes.GotoTop()
		if strings.TrimSpace(m.helpQuery) != "" {
			main, notes := 0, 0
			for _, match := range matchingHelp(strings.TrimSpace(m.helpQuery)) {
				if match.notes {
					notes++
				} else {
					main++
				}
			}
			if main == 0 && notes > 0 {
				m.helpNotesFocused = true
			} else if main > 0 && notes == 0 {
				m.helpNotesFocused = false
			}
		}
	} else {
		m.panel.SetYOffset(offset)
		m.helpNotes.SetYOffset(notesOffset)
	}
}

func (m *model) startHelpSearch() tea.Cmd {
	m.helpPreviousQuery = m.helpQuery
	m.helpPreviousOffset = m.panel.YOffset()
	m.helpPreviousNotesOffset = m.helpNotes.YOffset()
	m.helpPreviousNotesFocus = m.helpNotesFocused
	m.mode = "help-search"
	m.input.Prompt = "/ "
	m.input.CharLimit = 256
	m.input.SetValue(m.helpQuery)
	m.input.CursorEnd()
	m.resize()
	return m.input.Focus()
}

func (m *model) helpSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.helpQuery = m.helpPreviousQuery
		m.mode = "help"
		m.input.Blur()
		m.refreshHelp(false)
		m.panel.SetYOffset(m.helpPreviousOffset)
		m.helpNotes.SetYOffset(m.helpPreviousNotesOffset)
		m.helpNotesFocused = m.helpPreviousNotesFocus
		return nil
	case "enter":
		m.mode = "help"
		m.input.Blur()
		m.refreshHelp(false)
		return nil
	case "up", "down", "pgup", "pgdown":
		scroll(m.helpActivePane(), msg.String())
		return nil
	case "tab", "shift+tab":
		m.helpNotesFocused = !m.helpNotesFocused
		return nil
	}
	return m.updateHelpSearch(msg)
}

func (m *model) helpActivePane() *viewport.Model {
	if m.helpNotesFocused {
		return &m.helpNotes
	}
	return &m.panel
}

func (m *model) updateHelpSearch(msg tea.Msg) tea.Cmd {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.helpQuery = m.input.Value()
		m.refreshHelp(true)
	}
	return cmd
}
