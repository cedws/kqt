package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextAliasesSaveCompleteDisplayAndUseCanonicalState(t *testing.T) {
	m := demoModel(t)
	m.aliasCommand("prod", false)
	if m.settings.Config.ContextAliases["prod"] != "demo" || !strings.Contains(m.notice, "saved") {
		t.Fatal("alias was not saved", m.notice)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Context: prod") {
		t.Fatal("header did not use alias")
	}
	saved, err := LoadSettings(m.settings.Path)
	if err != nil || saved.Config.ContextAliases["prod"] != "demo" {
		t.Fatal("alias did not persist", err)
	}
	m.startInput("command", "ctx pr")
	m.completeCommand(false)
	if m.input.Value() != "ctx prod" {
		t.Fatal("alias did not complete")
	}
	key(m, "esc")
	m.openPicker("contexts")
	if m.matches[0].name != "prod" || !strings.Contains(m.matches[0].description, "demo") {
		t.Fatal("picker hid canonical context")
	}
	cmd := m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, load := m.Update(cmd())
	m.Update(load())
	if m.contextName != "demo" || m.settings.Config.LastContext != "demo" || m.settings.Config.Contexts["demo"].View == "" {
		t.Fatal("alias created a second context identity")
	}
	m.aliasCommand("prod", true)
	saved, err = LoadSettings(m.settings.Path)
	if err != nil || len(saved.Config.ContextAliases) != 0 || m.contextLabel("demo") != "demo" {
		t.Fatal("removed alias survived restart", err)
	}
}

func aliasLoader(t *testing.T) *kube.Loader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	data := "apiVersion: v1\nkind: Config\ncurrent-context: real-context\nclusters:\n- name: test\n  cluster:\n    server: https://example.invalid\ncontexts:\n- name: real-context\n  context:\n    cluster: test\n- name: another-context\n  context:\n    cluster: test\nusers: []\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	loader, err := kube.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return loader
}

func TestContextAliasCLIResolutionCollisionsAndStaleTargets(t *testing.T) {
	m := demoModel(t)
	m.settings.Config.ContextAliases = map[string]string{"prod": "real-context", "another-context": "real-context", "stale": "gone"}
	loader := aliasLoader(t)
	configured, err := newModel(context.Background(), m.settings, Options{Context: "prod"}, loader)
	if err != nil {
		t.Fatal(err)
	}
	defer configured.close()
	if configured.contextName != "real-context" || configured.contextLabel("real-context") != "prod" {
		t.Fatal("CLI alias did not resolve to canonical context")
	}
	if name, err := configured.resolveContext("another-context"); err != nil || name != "another-context" {
		t.Fatal("alias shadowed real context")
	}
	if _, err := configured.resolveContext("stale"); err == nil {
		t.Fatal("stale alias silently switched context")
	}
	configured.aliasCommand("another-context real-context", false)
	if !strings.Contains(configured.notice, "conflicts") {
		t.Fatal("saved alias over real context name")
	}
	before := configured.settings.Config.ContextAliases["prod"]
	configured.aliasCommand("prod gone", false)
	if configured.settings.Config.ContextAliases["prod"] != before {
		t.Fatal("unknown context replaced a valid alias")
	}
}

func TestContextAliasesYAMLAndTOMLRemovalAndValidation(t *testing.T) {
	for _, suffix := range []string{"yaml", "toml"} {
		t.Run(suffix, func(t *testing.T) {
			m := demoModel(t)
			m.settings.Path = filepath.Join(t.TempDir(), "config."+suffix)
			m.aliasCommand("prod demo", false)
			m.aliasCommand("dev demo", false)
			m.aliasCommand("prod", true)
			saved, err := LoadSettings(m.settings.Path)
			if err != nil || len(saved.Config.ContextAliases) != 1 || saved.Config.ContextAliases["dev"] != "demo" {
				t.Fatal("alias removal lost other aliases or retained removed entry", err)
			}
			if err := m.settings.savePatch(map[string]any{"context_aliases": map[string]string{"bad alias": "demo"}}); err == nil {
				t.Fatal("invalid alias was written")
			}
		})
	}
}
