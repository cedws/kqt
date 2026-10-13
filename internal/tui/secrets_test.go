package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type secretReader struct {
	*kube.Demo
	read func(context.Context) (unstructured.Unstructured, error)
}

func (b *secretReader) Get(ctx context.Context, _ kube.Resource, _, _ string) (unstructured.Unstructured, error) {
	return b.read(ctx)
}

func secretModel(t *testing.T) *model {
	t.Helper()
	m := demoModel(t)
	m.Update(m.changeResource("secrets", false)())
	m.layout = "inspector"
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 48})
	return m
}

func pressSecretToggle(m *model) tea.Cmd {
	return m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
}

func revealSecret(t *testing.T, m *model) {
	t.Helper()
	cmd := pressSecretToggle(m)
	if cmd == nil {
		t.Fatal("x did not read the selected Secret")
	}
	m.Update(cmd())
	if m.secretObject == nil {
		t.Fatalf("Secret read failed: %s", m.notice)
	}
}

func assertSecretHidden(t *testing.T, m *model) {
	t.Helper()
	if m.secretTarget != "" || m.secretObject != nil || strings.Contains(m.inspectorContent, "demo-only") || strings.Contains(m.panel.GetContent(), "demo-only") {
		t.Fatal("hidden Secret remained in a view/cache")
	}
}

func TestSecretToggleInInspectorAndAllDetailFormats(t *testing.T) {
	for _, layout := range []string{"classic", "inspector", "bottom"} {
		for _, format := range []string{"inspector", "describe", "yaml", "json"} {
			t.Run(layout+"/"+format, func(t *testing.T) {
				m := secretModel(t)
				m.layout = layout
				m.resize()
				if format != "inspector" {
					m.openDetails(format == "yaml", format == "json")
				}
				if strings.Contains(m.inspectorContent+m.panel.GetContent(), "demo-only") {
					t.Fatal("Secret visible before x")
				}
				revealSecret(t, m)
				if !strings.Contains(ansi.Strip(m.inspectorContent), "demo-only") || !strings.Contains(ansi.Strip(m.View().Content), "values visible") {
					t.Fatal("decoded value or visible indicator missing")
				}
				if m.mode == "details" && !strings.Contains(ansi.Strip(m.panel.GetContent()), "demo-only") {
					t.Fatal("details did not reveal decoded values")
				}
				if strings.Contains(m.inspectorContent, "ZGVtby1vbmx5") || kube.Text(m.rows[0], "data", "token") != "<redacted>" {
					t.Fatal("reveal did not decode, or changed redacted list rows")
				}
				m.openDetails(false, true)
				if !strings.Contains(ansi.Strip(m.panel.GetContent()), `"token": "demo-only"`) {
					t.Fatal("JSON switch lost revealed values")
				}
				m.setTheme(builtThemes()["nord"])
				if !strings.Contains(m.panel.GetContent(), m.theme.style("success").Render(`"demo-only"`)) {
					t.Fatal("revealed values lost syntax/theme colours")
				}
				m.setInspectorFormat("json")
				if !strings.Contains(ansi.Strip(m.inspectorContent), `"token": "demo-only"`) {
					t.Fatal("inspector format change hid values unexpectedly")
				}
				if cmd := pressSecretToggle(m); cmd != nil {
					t.Fatal("hiding issued another Kubernetes read")
				}
				assertSecretHidden(t, m)
				if !strings.Contains(ansi.Strip(m.panel.GetContent()), "redacted") {
					t.Fatal("second x did not restore placeholders")
				}
			})
		}
	}
}

func TestSecretRevealResetsOnExitSelectionScopeContextAndEdit(t *testing.T) {
	for _, action := range []string{"close-details", "selection", "scope", "context", "quit", "edit"} {
		t.Run(action, func(t *testing.T) {
			m := secretModel(t)
			revealSecret(t, m)
			switch action {
			case "close-details":
				m.openDetails(true, false)
				key(m, "esc")
			case "selection":
				other := *m.rows[0].DeepCopy()
				other.SetUID(types.UID("other-secret"))
				other.SetName("zzz-other")
				m.rows = append(m.rows, other)
				m.rebuild("")
				m.move(1)
			case "scope":
				m.command("ns platform")
			case "context":
				m.connect("another")
			case "quit":
				m.close()
			case "edit":
				ready := m.openEditor()().(editReadyMsg)
				if ready.err != nil || kube.Text(ready.original, "data", "token") != "ZGVtby1vbmx5" {
					t.Fatal("editor fetched decoded/redacted data instead of API data")
				}
			}
			assertSecretHidden(t, m)
		})
	}
}

func TestSecretRevealRejectsErrorsReplacementAndLateReplies(t *testing.T) {
	for _, action := range []string{"denied", "replacement", "hide-before-reply", "navigate-before-reply", "context-before-reply"} {
		t.Run(action, func(t *testing.T) {
			m := secretModel(t)
			original := m.backend.(*kube.Demo)
			fresh, _ := original.Get(context.Background(), m.route.resource, "default", "demo-credentials")
			m.backend = &secretReader{Demo: original, read: func(ctx context.Context) (unstructured.Unstructured, error) {
				if action == "denied" {
					return unstructured.Unstructured{}, errors.New("get secrets is forbidden")
				}
				if action == "replacement" {
					fresh.SetUID(types.UID("replacement"))
				}
				return fresh, ctx.Err()
			}}
			cmd := pressSecretToggle(m)
			response := cmd()
			switch action {
			case "hide-before-reply":
				pressSecretToggle(m)
			case "navigate-before-reply":
				m.changeResource("pods", false)
			case "context-before-reply":
				m.connect("another")
			}
			m.Update(response)
			assertSecretHidden(t, m)
			if (action == "denied" || action == "replacement") && !strings.Contains(m.notice, "Unable to reveal Secret") {
				t.Fatal("Secret read failure was not visible")
			}
		})
	}
	m := secretModel(t)
	cmd := pressSecretToggle(m)
	pressSecretToggle(m)
	response := cmd().(secretReadyMsg)
	if !errors.Is(response.err, context.Canceled) {
		t.Fatal("second x did not cancel the pending read")
	}
	m.Update(response)
	assertSecretHidden(t, m)
}

func TestSecretRefreshUpdatesOnlyRevealedObjectAndHidesReplacement(t *testing.T) {
	m := secretModel(t)
	revealSecret(t, m)
	m.openDetails(true, false)
	fresh := *m.secretObject.DeepCopy()
	fresh.Object["data"].(map[string]interface{})["token"] = base64.StdEncoding.EncodeToString([]byte("updated-value"))
	fresh.SetResourceVersion("2")
	m.Update(snapshotMsg{generation: m.generation, request: m.request, rows: []unstructured.Unstructured{fresh}})
	if !strings.Contains(m.inspectorContent, "updated-value") || !strings.Contains(m.panel.GetContent(), "updated-value") || kube.Text(m.rows[0], "data", "token") != "<redacted>" {
		t.Fatal("refresh did not update revealed views while keeping the list redacted")
	}
	fresh.SetUID(types.UID("new-incarnation"))
	m.Update(snapshotMsg{generation: m.generation, request: m.request, rows: []unstructured.Unstructured{fresh}})
	assertSecretHidden(t, m)
	if strings.Contains(m.inspectorContent+m.panel.GetContent(), "updated-value") {
		t.Fatal("UID replacement retained decoded data")
	}
}

func TestSecretRevealNeverPersistsAndXIsTextInInputs(t *testing.T) {
	m := secretModel(t)
	before, err := os.ReadFile(m.settings.Path)
	if err != nil {
		t.Fatal(err)
	}
	m.startInput("command", "")
	key(m, "x")
	if m.input.Value() != "x" || m.secretTarget != "" {
		t.Fatal("x in command input revealed a Secret")
	}
	m.mode = ""
	m.openPicker("resources")
	key(m, "x")
	if m.secretTarget != "" {
		t.Fatal("x in a picker revealed a Secret")
	}
	m.mode = ""
	m.inspectorFocused = true
	revealSecret(t, m)
	if m.mode != "" {
		t.Fatal("focused inspector toggle opened a modal")
	}
	after, err := os.ReadFile(m.settings.Path)
	if err != nil || string(before) != string(after) {
		t.Fatal("revealing a Secret changed saved configuration")
	}
	m.views[m.route.resource.ID()] = viewConfig{Columns: []string{"name", "/data/token"}}
	if table := ansi.Strip(m.table(100, 20)); strings.Contains(table, "demo-only") || !strings.Contains(table, "redacted") {
		t.Fatal("a custom table column exposed revealed data")
	}
}

func TestSecretTogglePreservesScrollAndTerminalDimensions(t *testing.T) {
	m := secretModel(t)
	backend := m.backend.(*kube.Demo)
	fresh, _ := backend.Get(context.Background(), m.route.resource, "default", "demo-credentials")
	for i := 0; i < 25; i++ {
		fresh.Object["data"].(map[string]interface{})[fmt.Sprintf("field-%02d", i)] = base64.StdEncoding.EncodeToString([]byte("readable-value"))
	}
	m.backend = &secretReader{Demo: backend, read: func(context.Context) (unstructured.Unstructured, error) { return fresh, nil }}
	m.Update(snapshotMsg{generation: m.generation, request: m.request, rows: []unstructured.Unstructured{fresh}})
	for _, size := range [][2]int{{180, 24}, {80, 24}, {40, 12}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.openDetails(true, false)
		m.panel.SetYOffset(3)
		m.inspector.SetYOffset(4)
		panelOffset, inspectorOffset := m.panel.YOffset(), m.inspector.YOffset()
		revealSecret(t, m)
		for _, visible := range []bool{true, false} {
			if !visible {
				pressSecretToggle(m)
			}
			if m.panel.YOffset() != panelOffset || m.inspector.YOffset() != inspectorOffset {
				t.Fatal("x changed the scroll position")
			}
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Fatalf("Secret view overflowed %dx%d", size[0], size[1])
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != size[0] {
					t.Fatalf("Secret view width differs from %d", size[0])
				}
			}
		}
	}
}
