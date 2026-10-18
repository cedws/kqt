package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpNotesAreSeparateAndOnTheRight(t *testing.T) {
	m := demoModel(t)
	for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 32}, {180, 48}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.openHelp()
		panes := m.helpPanes()
		if strings.Contains(m.panel.GetContent(), "Kubeconfig is never modified") || !strings.Contains(m.helpNotes.GetContent(), "Preferences") {
			t.Fatal("notes remain below the shortcuts")
		}
		if panes.split != (size[0] >= 80) {
			t.Fatal("unexpected responsive help layout")
		}
		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Fatal("help exceeded terminal height")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) != size[0] {
				t.Fatal("help exceeded terminal width")
			}
		}
		if panes.split {
			header := lines[panes.shortcuts.y-1]
			if strings.Index(header, "Notes") <= strings.Index(header, "Shortcuts") || !strings.Contains(header, "Shortcuts") {
				t.Fatal("notes are not to the right of shortcuts")
			}
		} else {
			key(m, "tab")
			if !m.helpNotesFocused || !strings.Contains(ansi.Strip(m.View().Content), "Preferences") {
				t.Fatal("narrow help cannot show Notes")
			}
			key(m, "tab")
			if m.helpNotesFocused {
				t.Fatal("Tab did not return to shortcuts")
			}
		}
	}
}

func TestHelpPanesScrollIndependentlyAndSearchRestoresBoth(t *testing.T) {
	m := demoModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.openHelp()
	key(m, "tab")
	key(m, "G")
	if m.helpNotes.YOffset() == 0 || m.panel.YOffset() != 0 {
		t.Fatal("scrolling notes moved shortcuts")
	}
	notesOffset := m.helpNotes.YOffset()
	key(m, "tab")
	key(m, "G")
	mainOffset := m.panel.YOffset()
	if mainOffset == 0 || m.helpNotes.YOffset() != notesOffset {
		t.Fatal("scrolling shortcuts moved notes")
	}
	key(m, "tab")
	key(m, "/")
	typeHelpQuery(m, "osc52")
	if !m.helpNotesFocused || m.helpNotes.YOffset() != 0 || !strings.Contains(m.helpNotes.GetContent(), "OSC52") {
		t.Fatal("notes-only search was not shown")
	}
	key(m, "esc")
	if !m.helpNotesFocused || m.panel.YOffset() != mainOffset || m.helpNotes.YOffset() != notesOffset {
		t.Fatal("cancel did not restore both scroll positions and focus")
	}
	key(m, "esc")
	m.openHelp()
	if m.helpNotesFocused || m.helpNotes.YOffset() != 0 || m.panel.YOffset() != 0 {
		t.Fatal("reopening help retained pane state")
	}
}

func TestHelpNotesMouseGeometryAndCopyIsolation(t *testing.T) {
	m := demoModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m.openHelp()
	panes := m.helpPanes()
	right := panes.notes
	m.Update(tea.MouseWheelMsg{X: right.x + 1, Y: right.y + 1, Button: tea.MouseWheelDown})
	if m.helpNotes.YOffset() == 0 || m.panel.YOffset() != 0 || m.helpNotesFocused {
		t.Fatal("wheel did not scroll only the pane under the pointer")
	}
	m.Update(tea.MouseClickMsg{X: right.x + 1, Y: right.y - 1, Button: tea.MouseLeft})
	if !m.helpNotesFocused {
		t.Fatal("clicking Notes did not focus it")
	}
	m.Update(tea.MouseClickMsg{X: panes.shortcuts.x + 1, Y: panes.shortcuts.y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	if !m.helpNotesFocused {
		t.Fatal("modified click changed focus")
	}
	if _, _, ok := m.selectionPane(right.x-2, right.y); ok {
		t.Fatal("divider belongs to a selectable pane")
	}
	m.helpNotes.GotoTop()
	copy := dragText(m, textPoint{right.x, right.y}, textPoint{right.x + 10, right.y})
	if text := copiedText(t, copy); text != "Preferences" {
		t.Fatalf("copied content from another pane: %q", text)
	}
}

func TestNarrowHelpSearchFindsNotesAndPreservesFocusOnResize(t *testing.T) {
	m := demoModel(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m.openHelp()
	key(m, "/")
	typeHelpQuery(m, "osc52")
	if !m.helpNotesFocused || !strings.Contains(ansi.Strip(m.View().Content), "OSC52") {
		t.Fatal("narrow notes search is hidden")
	}
	for _, size := range [][2]int{{180, 48}, {80, 24}, {40, 12}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if !m.helpNotesFocused || m.helpQuery != "osc52" || !strings.Contains(ansi.Strip(m.View().Content), "OSC52") {
			t.Fatal("resize lost the visible note search")
		}
	}
	key(m, "tab")
	if m.helpNotesFocused || m.input.Value() != "osc52" {
		t.Fatal("Tab edited the search")
	}
}
