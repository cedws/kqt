package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
)

func TestInspectorToggleRemembersPositionWithoutChangingLayoutOrView(t *testing.T) {
	m := demoModel(t)
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 48})
	m.selectLayout("bottom")
	m.command("columns name,status,age")
	m.setOrder("age", true)
	m.saveLayout("custom")
	m.settings.Layouts["custom"] = layoutProfile{Inspector: "bottom", InspectorPercent: 55, Views: cloneViews(m.views)}
	m.selected = 4
	selected, _ := m.current()
	columns := slices.Clone(m.columns())
	m.inspectorFocused = true
	key(m, "i")
	if m.inspectorVisible() || m.inspectorDimensions().width != 0 || m.inspectorFocused || m.layout != "custom" {
		t.Fatal("i did not just hide the inspector")
	}
	key(m, "i")
	if !m.inspectorVisible() || !m.inspectorDimensions().bottom || m.inspectorPlacement() != "bottom" || m.inspectorDimensions().height != (m.height-6)*55/100 {
		t.Fatal("i did not restore the previous placement and size")
	}
	if o, _ := m.current(); o.GetUID() != selected.GetUID() || !slices.Equal(m.columns(), columns) || m.sortColumn != "age" || !m.descending {
		t.Fatal("inspector toggle changed resource selection, columns or sorting")
	}
	key(m, "i")
	key(m, "i")
	if !m.inspectorDimensions().bottom {
		t.Fatal("repeated toggles cycled positions")
	}
}

func TestInspectorPlacementChooserAndCancellation(t *testing.T) {
	m := demoModel(t)
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 48})
	m.selectLayout("classic")
	key(m, "I")
	if m.mode != "picker" || m.pickerKind != "inspector-position" || len(m.choices) != 2 {
		t.Fatal("I did not open the placement chooser")
	}
	key(m, "esc")
	if m.inspectorVisible() || m.mode != "" {
		t.Fatal("cancelling placement changed visibility")
	}
	key(m, "I")
	typeHelpQuery(m, "bottom")
	key(m, "enter")
	if !m.inspectorVisible() || !m.inspectorDimensions().bottom || m.layout != "classic" {
		t.Fatal("placement did not show the inspector without changing layout")
	}
	if cmd := m.command("inspector"); cmd == nil || m.pickerIndex != 1 {
		t.Fatal(":inspector did not select the saved placement")
	}
	key(m, "esc")
	m.command("inspector right")
	if m.inspectorDimensions().bottom || m.inspectorPlacement() != "right" {
		t.Fatal("direct placement did not move the inspector")
	}
	key(m, "i")
	key(m, "i")
	if m.inspectorPlacement() != "right" || m.inspectorDimensions().bottom {
		t.Fatal("toggle did not remember the new placement")
	}
	if cmd := m.key(tea.KeyPressMsg{Code: 'b', Text: "b"}); cmd == nil || m.mode != "relations-loading" {
		t.Fatal("b no longer opens relationships")
	}
}

func TestInspectorPreferencesPersistYAMLAndTOML(t *testing.T) {
	for _, ext := range []string{"yaml", "toml"} {
		t.Run(ext, func(t *testing.T) {
			m := demoModel(t)
			m.settings.Path = filepath.Join(t.TempDir(), "config."+ext)
			m.placeInspector("bottom")
			m.setInspectorVisibility(false)
			saved, err := LoadSettings(m.settings.Path)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Config.InspectorVisible == nil || *saved.Config.InspectorVisible || saved.Config.InspectorPosition != "bottom" {
				t.Fatal("hidden state and remembered position were not saved")
			}
			restarted, err := newModel(context.Background(), saved, Options{Demo: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.close()
			if restarted.inspectorVisible() {
				t.Fatal("restart showed a hidden inspector")
			}
			key(restarted, "i")
			if !restarted.inspectorVisible() || restarted.inspectorPlacement() != "bottom" {
				t.Fatal("restart lost remembered placement")
			}
		})
	}
}

func TestInspectorSavedLayoutsAndLegacyDefaults(t *testing.T) {
	m := demoModel(t)
	for _, name := range []string{"classic", "inspector", "bottom"} {
		m.selectLayout(name)
		if m.inspectorVisible() != (name != "classic") {
			t.Fatal("legacy layout visibility changed")
		}
		if name != "classic" && m.inspectorPlacement() != m.settings.Layouts[name].Inspector {
			t.Fatal("selected layout did not apply its position")
		}
	}
	m.selectLayout("inspector")
	m.placeInspector("bottom")
	m.saveLayout("placed")
	m.setInspectorVisibility(false)
	m.saveLayout("hidden")
	m.selectLayout("placed")
	if !m.inspectorVisible() || m.inspectorPlacement() != "bottom" {
		t.Fatal("named layout did not capture placement")
	}
	m.selectLayout("hidden")
	if m.inspectorVisible() {
		t.Fatal("named layout did not capture visibility")
	}
	before := m.settings.Config
	m.selectLayout("does-not-exist")
	if m.settings.Config.InspectorPosition != before.InspectorPosition || m.settings.Config.InspectorVisible != before.InspectorVisible {
		t.Fatal("invalid layout changed inspector preferences")
	}
}

func TestInspectorResponsiveFallbackDoesNotChangePreference(t *testing.T) {
	m := demoModel(t)
	m.placeInspector("right")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.inspectorDimensions().bottom || m.inspectorPlacement() != "right" {
		t.Fatal("right inspector did not temporarily fit below the list")
	}
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 48})
	if m.inspectorDimensions().bottom || m.inspectorDimensions().width == 0 || m.inspectorPlacement() != "right" {
		t.Fatal("widening did not restore right placement")
	}
	m.placeInspector("bottom")
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 20})
	if m.inspectorDimensions().bottom || m.inspectorDimensions().width == 0 || m.inspectorPlacement() != "bottom" {
		t.Fatal("short terminal did not temporarily fit the inspector on the right")
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if m.inspectorDimensions().width != 0 {
		t.Fatal("inspector squeezed an unusable terminal")
	}
	m.setInspectorVisibility(false)
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 48})
	if m.inspectorDimensions().width != 0 {
		t.Fatal("resize showed an explicitly hidden inspector")
	}
}

func TestInspectorConfigValidationAndSecretHide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, body := range []string{"inspector_position: left\n", "inspector_visible: maybe\n"} {
		if err := os.WriteFile(path, []byte("version: 1\n"+body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSettings(path); err == nil {
			t.Fatal("invalid inspector preference accepted")
		}
	}
	m := demoModel(t)
	r, _ := kube.Resolve(m.resources, "secrets")
	m.Update(m.navigate(route{resource: r, namespace: "default"}, false)())
	m.placeInspector("bottom")
	m.Update(m.toggleSecret()())
	if m.secretTarget == "" {
		t.Fatal("secret fixture was not revealed")
	}
	key(m, "i")
	key(m, "i")
	if m.secretTarget != "" || strings.Contains(m.inspectorContent, "demo-only") {
		t.Fatal("hidden inspector retained revealed values")
	}
}

func TestInspectorCommandsRecordOnlySuccess(t *testing.T) {
	m := demoModel(t)
	for _, command := range []string{"inspector off", "inspector bottom", "inspector toggle", "inspector on"} {
		runSubmitted(m, command)
	}
	if len(m.settings.Config.CommandHistory) != 4 {
		t.Fatal("successful inspector commands were not remembered")
	}
	runSubmitted(m, "inspector typo")
	if len(m.settings.Config.CommandHistory) != 4 {
		t.Fatal("invalid inspector command was remembered")
	}
	m.startInput("command", "inspector pos")
	m.completeCommand(false)
	if m.input.Value() != "inspector position" {
		t.Fatal("placement completion is unavailable")
	}
}
