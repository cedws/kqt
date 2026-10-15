package tui

import "strings"

func (m *model) renderHelp() string {
	lines := strings.Split(helpText, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		// The static help table separates shortcuts and descriptions with at
		// least two spaces. Replace the first two with ": ", retaining alignment.
		if key, description, ok := strings.Cut(line[2:], "  "); ok {
			lines[i] = "  " + m.shortcuts("", shortcut{key, description})
		}
	}
	return strings.Join(lines, "\n")
}
