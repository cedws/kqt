package tui

import "strings"

const commandHistoryLimit = 50

func boundedCommandHistory(values []string) []string {
	var history []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			history = append(history, value)
		}
	}
	return history[max(0, len(history)-commandHistoryLimit):]
}

func (m *model) rememberCommand(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	history := boundedCommandHistory(append(m.settings.Config.CommandHistory, value))
	m.settings.Config.CommandHistory = history
	m.commandHistoryIndex = len(history)
	m.savePreferences(map[string]any{"command_history": history})
}

func (m *model) recallCommand(older bool) {
	history := m.settings.Config.CommandHistory
	if len(history) == 0 {
		return
	}
	m.completion = completionState{}
	if older {
		if m.commandHistoryIndex == len(history) {
			m.commandDraft, m.commandDraftCursor = m.input.Value(), m.input.Position()
		}
		if m.commandHistoryIndex == 0 {
			return
		}
		m.commandHistoryIndex--
	} else {
		if m.commandHistoryIndex == len(history) {
			return
		}
		m.commandHistoryIndex++
	}
	if m.commandHistoryIndex == len(history) {
		m.input.SetValue(m.commandDraft)
		m.input.SetCursor(m.commandDraftCursor)
	} else {
		m.input.SetValue(history[m.commandHistoryIndex])
		m.input.CursorEnd()
	}
}
