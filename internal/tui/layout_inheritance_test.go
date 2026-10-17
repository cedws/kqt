package tui

import "testing"

func TestCustomLayoutParentsAreIndependentOfMapIteration(t *testing.T) {
	for range 1000 {
		layouts, err := resolveLayouts(map[string]layoutProfile{
			"parent": {Extends: "classic", InspectorPercent: 45, Views: map[string]viewConfig{"pods": {Columns: []string{"name", "age"}}}},
			"child":  {Extends: "parent", Inspector: "bottom"},
			"leaf":   {Extends: "child"},
			"sibling": {Extends: "parent", Views: map[string]viewConfig{
				"pods": {Columns: []string{"name", "status"}},
			}},
		})
		if err != nil {
			t.Fatalf("valid custom inheritance rejected: %v", err)
		}
		leaf := layouts["leaf"]
		if leaf.Inspector != "bottom" || leaf.InspectorPercent != 45 || leaf.Views["pods"].Columns[1] != "age" {
			t.Fatalf("inherited settings lost: %+v", leaf)
		}
		if layouts["sibling"].Views["pods"].Columns[1] != "status" || layouts["parent"].Views["pods"].Columns[1] != "age" {
			t.Fatal("sibling overrides changed the parent")
		}
	}
}
