package tui

import "fmt"

func (m *model) inspectorPlacement() string {
	if position := m.settings.Config.InspectorPosition; position != "" {
		return position
	}
	if position := m.settings.Layouts[m.layout].Inspector; position == "right" || position == "bottom" {
		return position
	}
	return "right"
}

func (m *model) inspectorVisible() bool {
	if visible := m.settings.Config.InspectorVisible; visible != nil {
		return *visible
	}
	return m.settings.Layouts[m.layout].Inspector != "none"
}

func (m *model) inspectorProfile() layoutProfile {
	p := m.settings.Layouts[m.layout]
	p.Inspector = "none"
	if m.inspectorVisible() {
		p.Inspector = m.inspectorPlacement()
	}
	return p
}

func validateInspectorPreferences(c config) error {
	if c.InspectorPosition != "" && c.InspectorPosition != "right" && c.InspectorPosition != "bottom" {
		return fmt.Errorf("inspector_position must be right or bottom")
	}
	return nil
}

func (m *model) toggleInspector() {
	m.setInspectorVisibility(!m.inspectorVisible())
}

func (m *model) setInspectorVisibility(visible bool) {
	m.setInspector(m.inspectorPlacement(), visible)
}

func (m *model) placeInspector(position string) {
	m.setInspector(position, true)
}

func (m *model) setInspector(position string, visible bool) {
	m.settings.Config.InspectorPosition = position
	m.settings.Config.InspectorVisible = &visible
	if !visible {
		m.inspectorFocused = false
		m.clearSecret()
	}
	m.resize()
	m.updateInspector()
	m.notice = "Inspector hidden"
	if visible {
		m.notice = "Inspector shown at " + position
		if d := m.inspectorDimensions(); d.width == 0 {
			m.notice = "Inspector enabled · needs 120 columns or 24 rows"
		} else if (position == "bottom") != d.bottom {
			m.notice = "Inspector shown where space permits · preferred position: " + position
		}
	}
	m.savePreferences(map[string]any{"inspector_position": position, "inspector_visible": visible})
}
