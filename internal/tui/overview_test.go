package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestOverviewRenderingNavigationRefreshAndStaleReplies(t *testing.T) {
	m := demoModel(t)
	message := m.openOverview()().(overviewMsg)
	m.Update(message)
	content := ansi.Strip(m.panel.GetContent())
	for _, value := range []string{"Cluster overview", "Health", "Node capacity", "Usage", "Unhealthy resources", "Recent warning events", "CrashLoopBackOff"} {
		if !strings.Contains(content, value) {
			t.Fatalf("overview omitted %s", value)
		}
	}
	cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.pickerKind != "relations" {
		t.Fatal("overview issues not explorable")
	}
	key(m, "esc")
	m.Update(message)
	if m.mode != "" {
		t.Fatal("closed overview reopened from stale message")
	}
	m.openOverview()
	m.overviewReady(overviewMsg{generation: m.generation, token: m.overviewToken, snapshot: kube.Overview{Objects: map[string][]unstructured.Unstructured{}, Errors: map[string]string{"nodes": "forbidden"}}})
	if !strings.Contains(ansi.Strip(m.panel.GetContent()), "Unavailable: forbidden") {
		t.Fatal("missing section was shown as healthy zero")
	}
}
