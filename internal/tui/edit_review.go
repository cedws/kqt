package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aymanbagabas/go-udiff"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type editReview struct {
	patch         []byte
	before, after unstructured.Unstructured
}
type editReviewedMsg struct {
	generation, token uint64
	buffer            *editBuffer
	review            *editReview
	err               error
}

func reviewDiff(review *editReview) string {
	before, after := kube.Redacted(review.before), kube.Redacted(review.after)
	if before.GetKind() == "Secret" {
		key := "kubectl.kubernetes.io/last-applied-configuration"
		old, newValue := review.before.GetAnnotations(), review.after.GetAnnotations()
		if _, present := old[key]; present {
			annotations := before.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[key] = "<redacted>"
			before.SetAnnotations(annotations)
		}
		if value, present := newValue[key]; present {
			annotations := after.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[key] = "<redacted>"
			if value != old[key] {
				annotations[key] = "<redacted: changed>"
			}
			after.SetAnnotations(annotations)
		}
		for _, field := range []string{"data", "stringData"} {
			original, _, _ := unstructured.NestedMap(review.before.Object, field)
			changed, _, _ := unstructured.NestedMap(review.after.Object, field)
			values, _, _ := unstructured.NestedMap(after.Object, field)
			for key, value := range changed {
				if !reflect.DeepEqual(original[key], value) {
					values[key] = "<redacted: changed>"
				}
			}
			if values != nil {
				_ = unstructured.SetNestedMap(after.Object, values, field)
			}
		}
	}
	a, _ := yaml.Marshal(before.Object)
	b, _ := yaml.Marshal(after.Object)
	return udiff.Unified("current", "edited", kube.SafeLines(string(a)), kube.SafeLines(string(b)))
}

func (m *model) editReviewReady(v editReviewedMsg) tea.Cmd {
	if v.generation != m.generation || v.token != m.editToken || m.mode != "edit-loading" {
		return nil
	}
	if v.err != nil {
		m.editFailure("Cannot review edit: "+kube.Safe(v.err.Error()), v.buffer.path)
		return m.refreshResources()
	}
	if v.review == nil {
		return m.editFinished(editedMsg{generation: v.generation, token: v.token, buffer: v.buffer})
	}
	m.editBuffer, m.editReview = v.buffer, v.review
	m.mode, m.panelTitle = "edit-review", "Review changes · "+v.buffer.original.GetKind()+" "+v.buffer.original.GetName()
	m.resize()
	m.panel.SoftWrap = false
	m.panel.GotoTop()
	content := fmt.Sprintf("Context: %s\nResource: %s\n\n", kube.Safe(m.contextName), kube.Safe(strings.TrimPrefix(v.buffer.original.GetNamespace()+"/"+v.buffer.original.GetName(), "/")))
	if v.buffer.original.GetKind() == "Secret" {
		content += "Secret values remain hidden in this review.\n\n"
	}
	for _, line := range strings.Split(reviewDiff(v.review), "\n") {
		colour := "foreground"
		switch {
		case strings.HasPrefix(line, "+"):
			colour = "success"
		case strings.HasPrefix(line, "-"):
			colour = "error"
		case strings.HasPrefix(line, "@@"):
			colour = "accent"
		}
		content += m.theme.style(colour).Render(line) + "\n"
	}
	content += "\nEnter applies these changes · Escape discards them."
	m.panel.SetContent(content)
	return nil
}

func (m *model) editReviewKey(v tea.KeyPressMsg) tea.Cmd {
	switch v.String() {
	case "esc", "q":
		if m.editBuffer != nil {
			m.editBuffer.remove()
		}
		m.editBuffer = nil
		m.cancelEdit()
		m.mode = ""
		m.resize()
		m.notice = "Edit discarded"
		return m.refreshResources()
	case "enter":
		if !m.allowMutation("editing") || m.editReview == nil || m.editBuffer == nil {
			return nil
		}
		g, token, buffer, review := m.generation, m.editToken, m.editBuffer, m.editReview
		m.mode = "edit-loading"
		m.notice = "Applying reviewed changes…"
		ctx, cancel := context.WithTimeout(m.session.ctx, 15*time.Second)
		m.editCancel = cancel
		return func() tea.Msg {
			defer cancel()
			err := buffer.applyReview(ctx, review)
			return editedMsg{g, token, buffer, true, err}
		}
	default:
		scroll(&m.panel, v.String())
	}
	return nil
}
