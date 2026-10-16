package tui

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func themeContrast(a, b color.Color) float64 {
	luminance := func(c color.Color) float64 {
		r, g, b, _ := c.RGBA()
		linear := func(v uint32) float64 {
			x := float64(v) / 65535
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return .2126*linear(r) + .7152*linear(g) + .0722*linear(b)
	}
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + .05) / (math.Min(x, y) + .05)
}

func TestAccessibleThemeContrastIncludesInputsAndANSI256(t *testing.T) {
	for _, name := range []string{"accessible-dark", "accessible-light"} {
		palette := builtThemes()[name]
		for _, indexed := range []bool{false, true} {
			colour := func(c color.Color) color.Color {
				if indexed {
					return ansi.Convert256(c)
				}
				return c
			}
			check := func(label string, a, b color.Color, minimum float64) {
				t.Helper()
				if got := themeContrast(colour(a), colour(b)); got < minimum {
					t.Errorf("%s indexed=%v %s: %.3f:1, want >= %.1f:1", name, indexed, label, got, minimum)
				}
			}
			for _, background := range []string{"background", "surface"} {
				for _, text := range []string{"foreground", "muted", "accent", "focus", "success", "warning", "error"} {
					check(text+"/"+background, palette.color(text), palette.color(background), 4.5)
				}
				check("border/"+background, palette.color("border"), palette.color(background), 3)
				check("selection/"+background, palette.color("selection_background"), palette.color(background), 3)
				styles := palette.inputStyles()
				for _, state := range []struct{ text, prompt, placeholder, suggestion lipgloss.Style }{
					{styles.Focused.Text, styles.Focused.Prompt, styles.Focused.Placeholder, styles.Focused.Suggestion},
					{styles.Blurred.Text, styles.Blurred.Prompt, styles.Blurred.Placeholder, styles.Blurred.Suggestion},
				} {
					for _, s := range []lipgloss.Style{state.text, state.prompt, state.placeholder, state.suggestion} {
						check("input/"+background, s.GetForeground(), palette.color(background), 4.5)
					}
				}
				check("cursor/"+background, styles.Cursor.Color, palette.color(background), 4.5)
			}
			check("selected text", palette.color("selection_foreground"), palette.color("selection_background"), 4.5)
		}
	}
}

func TestFocusAndSearchRemainDistinctWithoutColour(t *testing.T) {
	m := demoModel(t)
	for _, name := range []string{"accessible-dark", "accessible-light", "no-color"} {
		m.setTheme(m.settings.Themes[name])
		focused := ansi.Strip(m.frame("Resources", "", 40, 4, true))
		inactive := ansi.Strip(m.frame("Resources", "", 40, 4, false))
		if !strings.HasPrefix(focused, "╭› Resources") || !strings.HasPrefix(inactive, "╭─ Resources") {
			t.Fatalf("%s: focus lacks a shape cue", name)
		}
		query, _ := compileLogFilter("include:retry")
		highlighted := m.theme.highlightLogMatches("WARN retry", query.spans("WARN retry"))
		if !strings.Contains(highlighted, ansi.NewStyle().Underline(true).String()) || ansi.Strip(highlighted) != "WARN retry" {
			t.Fatalf("%s: search lost emphasis or text", name)
		}
	}
}

func TestConditionInterpretationsDoNotDependOnColour(t *testing.T) {
	m := demoModel(t)
	m.setTheme(m.settings.Themes["no-color"])
	for _, tc := range []struct{ kind, name, state, want string }{
		{"Pod", "Ready", "True", "True · OK"}, {"Pod", "Ready", "False", "False · Attention"},
		{"Node", "DiskPressure", "False", "False · OK"}, {"Node", "DiskPressure", "True", "True · Problem"},
		{"Node", "MemoryPressure", "Unknown", "Unknown"}, {"Pod", "Ready", "", "Unknown"},
		{"Widget", "VendorPredicate", "True", "True"},
	} {
		object := unstructured.Unstructured{Object: map[string]interface{}{
			"kind": tc.kind, "metadata": map[string]interface{}{"name": "example"},
			"status": map[string]interface{}{"conditions": []interface{}{map[string]interface{}{"type": tc.name, "status": tc.state}}},
		}}
		view := m.renderResourceStatus(object, nil, nil)
		if hasExplicitColour(view) || !strings.Contains(view, tc.want) {
			t.Errorf("%s/%s=%s: missing monochrome interpretation %q: %s", tc.kind, tc.name, tc.state, tc.want, view)
		}
	}
}
