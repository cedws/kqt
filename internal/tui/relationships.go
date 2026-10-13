package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
)

type relationsMsg struct {
	generation, token uint64
	source            string
	links             []kube.Relation
	err               error
}
type relationOpenedMsg struct {
	generation, token uint64
	link              kube.Relation
	err               error
}

func (m *model) cancelRelations() {
	m.relationToken++
	if m.relationCancel != nil {
		m.relationCancel()
		m.relationCancel = nil
	}
}
func (m *model) openRelations() tea.Cmd {
	o, ok := m.current()
	if !ok || m.backend == nil {
		return nil
	}
	m.cancelRelations()
	ctx, cancel := context.WithTimeout(m.session.ctx, 15*time.Second)
	m.relationCancel = cancel
	m.mode = "relations-loading"
	m.panelTitle = "Related resources"
	m.panel.SetContent("Loading related resources…")
	g, token, b, resources := m.generation, m.relationToken, m.backend, m.resources
	return func() tea.Msg {
		defer cancel()
		links, err := kube.Relationships(ctx, b, resources, o)
		return relationsMsg{g, token, kube.Identity(o), links, err}
	}
}
func (m *model) relationsReady(v relationsMsg) tea.Cmd {
	o, ok := m.current()
	if v.generation != m.generation || v.token != m.relationToken || m.mode != "relations-loading" {
		return nil
	}
	if !ok || kube.Identity(o) != v.source {
		m.mode = ""
		m.notice = "Selected resource changed; open relationships again"
		return nil
	}
	m.relations = v.links
	cmd := m.openPicker("relations")
	if v.err != nil {
		m.notice = "Some relationships unavailable: " + kube.Safe(v.err.Error())
	}
	if len(v.links) == 0 && v.err == nil {
		m.notice = "No related resources found"
	}
	return cmd
}
func (m *model) selectRelation(name string) tea.Cmd {
	for _, link := range m.relations {
		if link.Key() == name {
			ctx, cancel := context.WithTimeout(m.session.ctx, 15*time.Second)
			m.relationCancel = cancel
			m.mode = "relations-loading"
			m.panel.SetContent("Opening " + kube.Safe(link.Title()) + "…")
			g, token, reader := m.generation, m.relationToken, m.backend.(kube.ResourceGetter)
			return func() tea.Msg {
				defer cancel()
				fresh, err := reader.Get(ctx, link.Resource, link.Namespace, link.Name)
				if err == nil && fresh.GetUID() != link.UID {
					err = fmt.Errorf("related resource was replaced; reopen relationships")
				}
				return relationOpenedMsg{g, token, link, err}
			}
		}
	}
	return nil
}
func (m *model) relationOpened(v relationOpenedMsg) tea.Cmd {
	if v.generation != m.generation || v.token != m.relationToken || m.mode != "relations-loading" {
		return nil
	}
	m.mode = ""
	if v.err != nil {
		m.notice = kube.Safe(v.err.Error())
		return nil
	}
	link := v.link
	m.options.Namespace = link.Namespace
	m.options.AllNamespaces = link.Resource.Namespaced && link.Namespace == ""
	return m.navigate(route{resource: link.Resource, namespace: link.Namespace, fields: "metadata.name=" + link.Name, selection: string(link.UID)}, true)
}
