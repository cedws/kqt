package tui

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var sgrSequence = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

func hasExplicitColour(text string) bool {
	for _, match := range sgrSequence.FindAllStringSubmatch(text, -1) {
		for _, param := range strings.FieldsFunc(match[1], func(r rune) bool { return r == ';' || r == ':' }) {
			n, _ := strconv.Atoi(param)
			if n >= 30 && n <= 38 || n >= 40 && n <= 48 || n == 58 || n >= 90 && n <= 107 {
				return true
			}
		}
	}
	return false
}

func TestNoColorThemeUsesTerminalDefaultsThroughoutViews(t *testing.T) {
	m := demoModel(t)
	m.setTheme(m.settings.Themes["no-color"])
	check := func(name, view string) {
		t.Helper()
		if hasExplicitColour(view) {
			t.Fatalf("%s contains explicit colours: %q", name, view)
		}
	}
	check("resources and inspector", m.View().Content)
	check("help", m.renderHelp())
	for name, render := range map[string]func(string) string{
		"YAML": m.theme.renderYAML, "JSON": m.theme.renderJSON, "logs": m.theme.renderLogLine,
	} {
		check(name, render(`{"level":"error","name":"web","ready":true,"count":42}`))
	}
	m.startInput("command", "theme no-color")
	check("command input", m.View().Content)
	m.input.SetValue("")
	m.input.Placeholder = "Type a command"
	check("placeholder", m.input.View())
	m.input.Blur()
	check("blurred input", m.input.View())
	selection := m.theme.selectionStyle().Render("selected")
	if selection == "selected" || ansi.Strip(selection) != "selected" {
		t.Fatal("monochrome selection lost its visual emphasis or text")
	}
	check("selection", selection)
	query, err := compileLogFilter("include:ready")
	if err != nil {
		t.Fatal(err)
	}
	check("log matches", m.theme.highlightLogMatches("INFO ready", query.spans("INFO ready")))
}

func TestNoColorThemePreviewSelectionAndRestart(t *testing.T) {
	m := demoModel(t)
	original := m.theme.Name
	m.startInput("command", "theme no-c")
	m.completeCommand(false)
	if m.input.Value() != "theme no-color" {
		t.Fatal("no-color missing from completion")
	}
	m.openPicker("themes")
	m.input.SetValue("no-color")
	m.filterChoices()
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.theme.Name != "no-color" || hasExplicitColour(m.View().Content) {
		t.Fatal("no-color preview retained colours")
	}
	saved, err := LoadSettings(m.settings.Path)
	if err != nil || saved.Config.Theme != original {
		t.Fatalf("preview saved before confirmation: %v", err)
	}
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.theme.Name != original || !hasExplicitColour(m.input.View()) {
		t.Fatal("cancel did not restore the theme and input colours")
	}
	m.command("theme no-color")
	saved, err = LoadSettings(m.settings.Path)
	if err != nil || saved.Config.Theme != "no-color" {
		t.Fatalf("no-color selection was not saved: %v", err)
	}
	restarted, err := newModel(context.Background(), saved, Options{Demo: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.close)
	restarted.startInput("command", "theme nord")
	if restarted.theme.Name != "no-color" || hasExplicitColour(restarted.View().Content) {
		t.Fatal("restart did not retain no-color for the view and input")
	}
	m.command("theme nord")
	m.startInput("command", "theme no-color")
	if !hasExplicitColour(m.View().Content) || !hasExplicitColour(m.input.View()) {
		t.Fatal("switching back did not restore colours")
	}
}
