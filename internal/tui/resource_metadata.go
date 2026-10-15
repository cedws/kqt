package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type resourcePanelTab int

const (
	resourceStatusTab resourcePanelTab = iota
	resourceLabelsTab
	resourceAnnotationsTab
	resourceFinalizersTab
	resourcePanelTabCount
)

func (tab resourcePanelTab) title() string {
	switch tab {
	case resourceLabelsTab:
		return "Labels"
	case resourceAnnotationsTab:
		return "Annotations"
	case resourceFinalizersTab:
		return "Finalizers"
	default:
		return "Status & Events"
	}
}

// Keep the tab strip outside the viewport so navigation remains visible while
// scrolling through long annotations or following events.
func (m *model) resourcePanelTabs() string {
	names := []string{"Status", "Labels", "Annotations", "Finalizers"}
	if m.panel.Width() < 64 {
		names = []string{"Stat", "Labels", "Ann", "Fin"}
	}
	var tabs []string
	for i, name := range names {
		active := resourcePanelTab(i) == m.resourcePanelTab
		key := m.theme.style("accent").Bold(true).Underline(active)
		label := m.theme.style("muted").Bold(active).Underline(active)
		tabs = append(tabs, label.Render("[")+key.Render(fmt.Sprint(i+1))+label.Render(": "+name+"]"))
	}
	return strings.Join(tabs, " ")
}

func (m *model) renderResourceMetadata(o unstructured.Unstructured, tab resourcePanelTab) string {
	// Match inspection's Secret policy, including last-applied configurations
	// which can contain another copy of a Secret's data.
	o = kube.Redacted(o)
	w := &statusWriter{theme: m.theme}
	name := o.GetName()
	if o.GetNamespace() != "" {
		name = o.GetNamespace() + "/" + name
	}
	w.heading(kube.Safe(o.GetKind()) + " · " + kube.Safe(name))
	w.WriteByte('\n')
	if tab == resourceFinalizersTab {
		finalizers := o.GetFinalizers()
		w.heading(fmt.Sprintf("Finalizers (%d)", len(finalizers)))
		if len(finalizers) == 0 {
			w.note("No finalizers on this resource.")
		}
		for _, finalizer := range finalizers {
			w.WriteString("  " + m.theme.style("muted").Render("- ") + m.theme.style("focus").Bold(true).Render(kube.Safe(finalizer)) + "\n")
		}
		return w.String()
	}
	values := o.GetLabels()
	if tab == resourceAnnotationsTab {
		values = o.GetAnnotations()
	}
	w.heading(fmt.Sprintf("%s (%d)", tab.title(), len(values)))
	if len(values) == 0 {
		w.note("No " + strings.ToLower(tab.title()) + " on this resource.")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		label := "  " + m.theme.style("focus").Bold(true).Render(kube.Safe(key)) + m.theme.style("muted").Render(":")
		value := values[key]
		if value == "" {
			value = `""`
		}
		if tab == resourceAnnotationsTab && key == "kubectl.kubernetes.io/last-applied-configuration" {
			var pretty bytes.Buffer
			// Indent the original JSON without decoding numbers or reordering keys.
			if json.Indent(&pretty, []byte(strings.TrimSpace(value)), "    ", "  ") == nil {
				if strings.Contains(pretty.String(), "\n") {
					w.WriteString(label + "\n    " + m.theme.renderJSON(pretty.String()) + "\n")
				} else {
					w.WriteString(label + " " + m.theme.renderJSON(pretty.String()) + "\n")
				}
				continue
			}
		}
		if strings.Contains(value, "\n") {
			w.WriteString(label + "\n")
			w.note(value)
		} else {
			w.WriteString(label + " " + kube.SafeLines(value) + "\n")
		}
	}
	if tab == resourceAnnotationsTab && o.GetKind() == "Secret" {
		w.note("Secret last-applied configuration is hidden, as in the manifest view.")
	}
	return w.String()
}
