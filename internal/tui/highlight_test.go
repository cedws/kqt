package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
)

func TestYAMLHighlightingPreservesTextAcrossThemes(t *testing.T) {
	inputs := []string{
		"",
		"\n\n  \n",
		"apiVersion: v1\nkind: Pod\nmetadata:\n  name: nginx\nspec:\n  replicas: 42\n  enabled: true\n  optional: null\n",
		"---\n# comment: true 42\n'quoted: key': \"escaped \\\" quotes, \\t tab, colon: #hash, 東京 🌲\"\nlist: [false, -1.25e+10, 0xFF, null, '42']\n...\n\n",
		"data:\n  script: |\n    # literal comment\n    echo 'key: true, number: 42'\n\n    next line  \n  folded: >-\n    first line\n    second line\n  single: 'it''s # not a comment: false'\n",
		"defaults: &defaults\n  enabled: true\ncopy: *defaults\ntagged: !!str 42\n",
		"value: \"unfinished",
	}
	for name, palette := range builtThemes() {
		t.Run(name, func(t *testing.T) {
			for _, plain := range inputs {
				got := palette.renderYAML(plain)
				if ansi.Strip(got) != plain {
					t.Fatalf("highlighting changed text\nwant %q\ngot  %q", plain, ansi.Strip(got))
				}
				if ansi.StringWidth(got) != ansi.StringWidth(plain) {
					t.Fatal("highlighting changed display width")
				}
			}
		})
	}
}

func TestYAMLSyntaxUsesSemanticThemeColours(t *testing.T) {
	plain := "name: nginx\nreplicas: 42\nenabled: true\noptional: null\nquoted: 'true: 123 # string'\n# comment\nscript: |\n  nested: true # literal\n"
	for name, palette := range builtThemes() {
		t.Run(name, func(t *testing.T) {
			got := palette.renderYAML(plain)
			for _, want := range []struct{ text, colour string }{
				{"name", "accent"}, {"nginx", "success"}, {"42", "warning"}, {"true", "focus"}, {"null", "muted"},
				{"'true: 123 # string'", "success"}, {"# comment", "muted"}, {":", "muted"}, {"nested: true # literal", "success"},
			} {
				if !strings.Contains(got, palette.style(want.colour).Render(want.text)) {
					t.Errorf("%q did not use %s", want.text, want.colour)
				}
			}
		})
	}
}

func TestYAMLViewsHighlightAndRedactSecrets(t *testing.T) {
	m := demoModel(t)
	o, _ := m.current()
	plain := kube.Manifest(o, false)
	styled := m.theme.renderYAML(plain)
	if styled == plain {
		t.Fatal("YAML was not highlighted")
	}
	for _, manifest := range []bool{false, true} {
		m.openDetails(manifest, false)
		if !strings.Contains(m.panel.View(), m.theme.style("accent").Render("apiVersion")) {
			t.Fatal("detail panel lost YAML key highlighting")
		}
	}
	if !strings.Contains(m.inspectorContent, styled) {
		t.Fatal("inspector lost YAML highlighting")
	}
	m.openDetails(false, true)
	if !strings.Contains(m.panel.View(), m.theme.style("accent").Render(`"apiVersion"`)) {
		t.Fatal("JSON key highlighting missing")
	}
	m.mode = ""
	cmd := m.changeResource("secrets", false)
	m.Update(cmd())
	o, _ = m.current()
	want := kube.Manifest(o, false)
	m.openDetails(true, false)
	if strings.Contains(m.panel.View(), "ZGVtby1vbmx5") || !strings.Contains(ansi.Strip(m.panel.View()), "<redacted>") {
		t.Fatal("highlighting bypassed secret redaction")
	}
	if !strings.Contains(m.inspectorContent, m.theme.renderYAML(want)) {
		t.Fatal("inspector did not highlight the redacted manifest")
	}
}

func TestYAMLInspectorTracksThemePreviewWithoutMovingScroll(t *testing.T) {
	m := demoModel(t)
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 24})
	m.inspector.SetYOffset(8)
	before, offset := m.inspectorContent, m.inspector.YOffset()
	m.openPicker("themes")
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.inspectorContent == before || ansi.Strip(m.inspectorContent) != ansi.Strip(before) {
		t.Fatal("theme preview did not change only the YAML colours")
	}
	if m.inspector.YOffset() != offset {
		t.Fatal("theme preview reset inspector scrolling")
	}
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.inspectorContent != before || m.inspector.YOffset() != offset {
		t.Fatal("theme cancel did not restore colours and scroll")
	}
	m.command("theme nord")
	if m.inspectorContent == before || ansi.Strip(m.inspectorContent) != ansi.Strip(before) || m.inspector.YOffset() != offset {
		t.Fatal("theme command did not recolour in place")
	}
}

func TestYAMLHighlightingSanitizesBeforeStyling(t *testing.T) {
	plain := "value: '\x1b]52;c;secret\a\u202e'\n"
	styled := builtThemes()["nord"].renderYAML(plain)
	if strings.Contains(styled, "\x1b]52") || strings.ContainsAny(styled, "\a\u202e") {
		t.Fatal("YAML injected terminal controls")
	}
	if ansi.Strip(styled) != kube.SafeLines(plain) {
		t.Fatal("highlighting changed sanitized text")
	}
}

func TestJSONHighlightingPreservesTextAndThemeAcrossPalettes(t *testing.T) {
	plain := "{\n  \"name\": \"web, braces {}[], escaped \\\" quotes 東京\",\n  \"number\": 18446744073709551615,\n  \"float\": -1.25e+10,\n  \"enabled\": true,\n  \"other\": false,\n  \"nil\": null\n}"
	for name, palette := range builtThemes() {
		t.Run(name, func(t *testing.T) {
			highlighted := palette.renderJSON(plain)
			if ansi.Strip(highlighted) != plain || ansi.StringWidth(highlighted) != ansi.StringWidth(plain) {
				t.Fatal("JSON text or width changed")
			}
			for _, tc := range []struct{ text, colour string }{{`"name"`, "accent"}, {"18446744073709551615", "warning"}, {"-1.25e+10", "warning"}, {"true", "focus"}, {"false", "focus"}, {"null", "muted"}} {
				if !strings.Contains(highlighted, palette.style(tc.colour).Render(tc.text)) {
					t.Fatalf("%s colour missing", tc.text)
				}
			}
		})
	}
}
