package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
)

func TestRelationshipsPickerNavigationAndStaleResults(t *testing.T) {
	m := demoModel(t)
	m.filter = "web0003"
	m.rebuild("")
	msg := m.openRelations()().(relationsMsg)
	m.Update(msg)
	if m.mode != "picker" || m.pickerKind != "relations" {
		t.Fatal("relationships picker missing")
	}
	for i, c := range m.matches {
		if strings.Contains(c.description, "Deployment") {
			m.pickerIndex = i
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("related resource did not open")
	}
	_, cmd = m.Update(cmd())
	m.Update(cmd())
	if len(m.visible) != 1 || m.visible[0].GetKind() != "Deployment" || m.visible[0].GetName() != "web" {
		t.Fatal("navigation lost resource identity")
	}
	if len(m.history) == 0 {
		t.Fatal("no return route")
	}
	msg = m.openRelations()().(relationsMsg)
	key(m, "esc")
	m.Update(msg)
	if m.mode != "" {
		t.Fatal("cancelled relationship result reopened picker")
	}
	m.Update(relationsMsg{generation: m.generation - 1, links: []kube.Relation{{Name: "stale"}}})
	if m.mode != "" {
		t.Fatal("old-context result reopened picker")
	}
}
