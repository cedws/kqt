package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func metadataObject() unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "Widget", "metadata": map[string]interface{}{
			"name": "web", "namespace": "apps", "uid": "metadata-fixture",
			"labels":      map[string]interface{}{"z.example/owner": "platform", "app": "web", "empty": ""},
			"annotations": map[string]interface{}{"z.example/long": strings.Repeat("界", 400), "a.example/notes": "first line\nsecond line\n\nlast line"},
			"finalizers":  []interface{}{"z.example/cleanup", "a.example/storage"},
		},
	}}
}

func TestResourceMetadataRendersSortedKeysMultilineAndEmptyValues(t *testing.T) {
	m := demoModel(t)
	o := metadataObject()
	before := o.DeepCopy()
	labels := ansi.Strip(m.renderResourceMetadata(o, resourceLabelsTab))
	for _, want := range []string{"Widget · apps/web", "Labels (3)", "  app: web\n", "  z.example/owner: platform\n", "  empty: \"\"\n"} {
		if !strings.Contains(labels, want) {
			t.Fatalf("missing %q in labels:\n%s", want, labels)
		}
	}
	if strings.Index(labels, "app:") > strings.Index(labels, "z.example/owner:") || strings.Contains(labels, "\n\n  ") {
		t.Fatal("label keys are not sorted")
	}
	annotations := ansi.Strip(m.renderResourceMetadata(o, resourceAnnotationsTab))
	if strings.Index(annotations, "a.example/notes") > strings.Index(annotations, "z.example/long") || !strings.Contains(annotations, "  a.example/notes:\n    first line\n    second line\n    \n    last line") || !strings.Contains(annotations, "  z.example/long: "+strings.Repeat("界", 400)) {
		t.Fatal("annotations lost their order or multiline values")
	}
	finalizers := ansi.Strip(m.renderResourceMetadata(o, resourceFinalizersTab))
	if !strings.Contains(finalizers, "Finalizers (2)\n  - z.example/cleanup\n  - a.example/storage\n") {
		t.Fatal("finalizer entries or their API order were lost")
	}
	if !reflect.DeepEqual(o.Object, before.Object) {
		t.Fatal("metadata rendering modified the resource")
	}
	empty := unstructured.Unstructured{Object: map[string]interface{}{"kind": "Node", "metadata": map[string]interface{}{"name": "worker"}}}
	for _, tab := range []resourcePanelTab{resourceLabelsTab, resourceAnnotationsTab, resourceFinalizersTab} {
		text := ansi.Strip(m.renderResourceMetadata(empty, tab))
		if !strings.Contains(text, "Node · worker") || !strings.Contains(text, "No "+strings.ToLower(tab.title())+" on this resource.") {
			t.Fatal("empty metadata was not reported explicitly")
		}
	}
}

func TestResourceMetadataSanitizesControlsAndRetainsSecretRedaction(t *testing.T) {
	m := demoModel(t)
	o := metadataObject()
	o.SetLabels(map[string]string{"\x1b]52;c;payload\akey": "\x1b[2J\u202evalue"})
	o.SetAnnotations(map[string]string{"notes": "\x1b[31m\u202esecret", "kubectl.kubernetes.io/last-applied-configuration": `{"data":{"token":"HIDDEN_SECRET_COPY"}}`})
	o.SetFinalizers([]string{"\x1b[2J\u202econtroller"})
	o.SetKind("Secret")
	o.Object["data"] = map[string]interface{}{"token": "HIDDEN_SECRET_DATA"}
	before := o.DeepCopy()
	for _, tab := range []resourcePanelTab{resourceLabelsTab, resourceAnnotationsTab, resourceFinalizersTab} {
		text := ansi.Strip(m.renderResourceMetadata(o, tab))
		if strings.ContainsAny(text, "\x1b\a\u202e") || strings.Contains(text, "HIDDEN_SECRET") {
			t.Fatal("metadata issued terminal controls or exposed redacted Secret data")
		}
	}
	if !reflect.DeepEqual(o.Object, before.Object) {
		t.Fatal("metadata redaction modified the source")
	}
}

func TestLastAppliedAnnotationPreservesJSONAndSource(t *testing.T) {
	m := demoModel(t)
	const key = "kubectl.kubernetes.io/last-applied-configuration"
	const source = `{"apiVersion":"v1","kind":"Pod","spec":{"containers":[{"name":"client","command":["sleep","3600"]}],"large":18446744073709551615,"exponent":1.23e+40},"text":"界\\nquoted \\\" value"}`
	o := metadataObject()
	o.SetAnnotations(map[string]string{key: source + "\n"})
	before := o.DeepCopy()
	styled := m.renderResourceMetadata(o, resourceAnnotationsTab)
	plain := ansi.Strip(styled)
	if !strings.Contains(plain, "\n    {\n      \"apiVersion\": \"v1\",") || !strings.Contains(plain, "\n            \"name\": \"client\",") {
		t.Fatal("last-applied JSON was not indented under its annotation key")
	}
	_, formatted, ok := strings.Cut(plain, "  "+key+":\n")
	var compact bytes.Buffer
	if !ok || json.Compact(&compact, []byte(formatted)) != nil || compact.String() != source {
		t.Fatalf("formatting changed JSON data, number precision or key order: %s", formatted)
	}
	if !strings.Contains(styled, m.theme.style("accent").Render(`"apiVersion"`)) || !reflect.DeepEqual(o.Object, before.Object) {
		t.Fatal("annotation highlighting was missing or changed the API object")
	}
	for _, value := range []string{`{"kind":`, "not JSON", "", "\x1b[2Junsafe\u202e"} {
		o.SetAnnotations(map[string]string{key: value})
		got := ansi.Strip(m.renderResourceMetadata(o, resourceAnnotationsTab))
		want := value
		prefix := "  " + key + ": "
		if want == "" {
			want = `""`
		}
		if !strings.Contains(got, prefix+kube.SafeLines(want)+"\n") || strings.ContainsAny(got, "\x1b\u202e") {
			t.Fatalf("invalid JSON did not retain safe plain text: %q", got)
		}
	}
}

func TestResourcePanelTabsKeepWatchAndSelectionAndDoNotFollowHiddenEvents(t *testing.T) {
	m, session := eventModel(t, false)
	feed, token, selected := m.eventFeed, m.eventToken, m.selected
	m.eventObject.SetLabels(map[string]string{"app": "web"})
	m.eventObject.SetAnnotations(map[string]string{"notes": "first\nsecond"})
	m.eventObject.SetFinalizers([]string{"example/cleanup"})
	for _, tc := range []struct {
		key string
		tab resourcePanelTab
	}{{"2", resourceLabelsTab}, {"tab", resourceAnnotationsTab}, {"tab", resourceFinalizersTab}, {"tab", resourceStatusTab}, {"shift+tab", resourceFinalizersTab}, {"1", resourceStatusTab}, {"3", resourceAnnotationsTab}, {"4", resourceFinalizersTab}, {"M", resourceLabelsTab}, {"S", resourceStatusTab}, {"E", resourceStatusTab}} {
		if tc.key == "shift+tab" {
			m.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		} else {
			key(m, tc.key)
		}
		if m.resourcePanelTab != tc.tab || m.mode != "events" || m.eventFeed != feed || m.eventToken != token || session.ctx.Err() != nil || m.selected != selected || m.panel.YOffset() != 0 {
			t.Fatalf("%s changed the session, selection or wrong tab", tc.key)
		}
		if !strings.Contains(ansi.Strip(m.resourcePanelTabs()), "[3: Annotations]") || !strings.Contains(m.panelTitle, tc.tab.title()) {
			t.Fatal("tab navigation is not visible")
		}
	}
	key(m, "2")
	key(m, "G")
	pulseEvent(m, liveEvent("retry", 110, 4), "ADDED")
	if m.eventTail || !strings.Contains(m.panel.GetContent(), "Labels (1)") || strings.Contains(m.panel.GetContent(), "retry web") {
		t.Fatal("a hidden event replaced metadata or enabled event following")
	}
	key(m, "/")
	if m.mode != "events" {
		t.Fatal("event search took over a metadata tab")
	}
	key(m, "1")
	if !strings.Contains(m.panel.GetContent(), "×4") {
		t.Fatal("switching back to status lost collected events")
	}
}

func TestMetadataRefreshReloadExportAndStaleResults(t *testing.T) {
	m, session := eventModel(t, false)
	key(m, "3")
	fresh := *m.eventObject.DeepCopy()
	fresh.SetAnnotations(map[string]string{"notes": "new annotation", "kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"v1","kind":"Pod"}`})
	m.Update(eventStatusMsg{generation: m.generation, token: m.eventToken, object: fresh})
	if !strings.Contains(m.panel.GetContent(), "new annotation") || strings.Contains(m.panel.GetContent(), "Containers") {
		t.Fatal("refresh did not update the active metadata tab")
	}
	m.Update(eventStatusMsg{generation: m.generation, token: m.eventToken, err: errors.New("read denied")})
	if !strings.Contains(m.panel.GetContent(), "STALE RESOURCE METADATA") || !strings.Contains(m.panel.GetContent(), "new annotation") {
		t.Fatal("failed refresh hid usable metadata or lacked a stale warning")
	}
	path := filepath.Join(t.TempDir(), "annotations.txt")
	m.eventsInput("event-save")
	if m.input.Value() != "kqt-annotations.txt" {
		t.Fatal("metadata export has the wrong suggested filename")
	}
	m.input.SetValue(path)
	m.Update(m.eventsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
	data, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 || !strings.Contains(string(data), "new annotation") || strings.Contains(string(data), "Containers") || !strings.Contains(m.notice, "Metadata saved to") {
		t.Fatal("metadata export did not match the active tab or was not private")
	}
	if !strings.Contains(string(data), "\n      \"apiVersion\": \"v1\",") || strings.ContainsRune(string(data), '\x1b') {
		t.Fatal("metadata export lost pretty JSON or retained colour codes")
	}
	m.Update(m.eventsKey("ctrl+r")())
	if session.ctx.Err() == nil || m.resourcePanelTab != resourceAnnotationsTab || !strings.Contains(m.panel.GetContent(), "Annotations") {
		t.Fatal("reload changed the tab or retained the old watch")
	}
	before := m.panel.GetContent()
	m.Update(eventStatusMsg{generation: m.generation, token: m.eventToken - 1, object: fresh})
	if m.panel.GetContent() != before {
		t.Fatal("late metadata response changed a reloaded view")
	}
	key(m, "esc")
	if m.resourcePanelTab != resourceStatusTab || m.eventFeed != nil || m.mode != "" {
		t.Fatal("closing metadata leaked its panel or watch")
	}
}

func TestMetadataEntryCommandsFreshReadsAndDimensions(t *testing.T) {
	m := demoModel(t)
	o, _ := m.current()
	fresh := *o.DeepCopy()
	fresh.SetLabels(map[string]string{"fresh": "new label"})
	m.backend = &secretReader{Demo: m.backend.(*kube.Demo), read: func(context.Context) (unstructured.Unstructured, error) { return fresh, nil }}
	m.Update(m.key(tea.KeyPressMsg{Code: 'M', Text: "M"})())
	if m.resourcePanelTab != resourceLabelsTab || !strings.Contains(m.panel.GetContent(), "new label") {
		t.Fatal("M did not fetch and show fresh labels")
	}
	for _, size := range [][2]int{{40, 12}, {80, 24}, {180, 48}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.eventObject.SetAnnotations(map[string]string{"notes": strings.Repeat("wide 界 annotation\n", 100)})
		key(m, "3")
		key(m, "G")
		view := m.View().Content
		if len(strings.Split(view, "\n")) != size[1] || !strings.Contains(ansi.Strip(view), "[2: Labels]") {
			t.Fatal("metadata or pinned tabs exceeded the terminal height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) != size[0] {
				t.Fatal("metadata exceeded the terminal width")
			}
		}
	}
	key(m, "esc")
	for _, tc := range []struct {
		command string
		tab     resourcePanelTab
	}{{"metadata", resourceLabelsTab}, {"metadata annotations", resourceAnnotationsTab}, {"metadata finalizers", resourceFinalizersTab}, {"metadata status", resourceStatusTab}, {"status", resourceStatusTab}} {
		m.Update(m.command(tc.command)())
		if m.resourcePanelTab != tc.tab {
			t.Fatalf("%s opened the wrong tab", tc.command)
		}
		key(m, "esc")
	}
	if m.command("metadata unknown") != nil || !strings.Contains(m.notice, "Use :metadata") {
		t.Fatal("invalid metadata command was accepted")
	}
	m.startInput("command", "metadata ann")
	m.completeCommand(false)
	if m.input.Value() != "metadata annotations" {
		t.Fatal("metadata tab completion failed")
	}
}

func TestMetadataUsableWhenEventAccessDeniedAndSecretRevealIsCleared(t *testing.T) {
	m := demoModel(t)
	m.backend = &eventsDeniedBackend{m.backend.(*kube.Demo)}
	m.Update(m.openResourcePanel(resourceLabelsTab)())
	if !strings.Contains(m.panel.GetContent(), "Labels") || strings.Contains(m.panel.GetContent(), "events is forbidden") {
		t.Fatal("unavailable events hid metadata")
	}
	key(m, "1")
	if !strings.Contains(m.panel.GetContent(), "events is forbidden") {
		t.Fatal("event failure was lost when returning to status")
	}
	s := secretModel(t)
	revealSecret(t, s)
	s.Update(s.openResourcePanel(resourceAnnotationsTab)())
	assertSecretHidden(t, s)
}
