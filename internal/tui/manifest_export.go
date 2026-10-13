package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
)

type manifestSavedMsg struct {
	generation uint64
	path       string
	err        error
}

func (m *model) manifestFormat() string {
	if m.mode == "details" && m.detailFormat == "json" {
		return "json"
	}
	if m.mode == "details" {
		return "yaml"
	}
	return m.inspectorFormat
}
func (m *model) exportContent(format string, clean bool) (string, error) {
	o, ok := m.current()
	if !ok {
		return "", fmt.Errorf("select a resource first")
	}
	if o.GetKind() == "Secret" && m.secretObject != nil && kube.Identity(o) == m.secretTarget {
		o = *m.secretObject.DeepCopy()
	} else {
		o = kube.Redacted(o)
	}
	return kube.ExportManifest(o, format, clean)
}
func (m *model) copyManifest(format string, clean bool) tea.Cmd {
	content, err := m.exportContent(format, clean)
	if err != nil {
		m.notice = kube.Safe(err.Error())
		return nil
	}
	if len(content) > 1024*1024 {
		m.notice = "Manifest exceeds 1 MiB; export it to a file instead"
		return nil
	}
	m.notice = strings.ToUpper(format) + " sent to terminal clipboard (OSC52)"
	return tea.SetClipboard(content)
}
func writeManifest(path, content string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(content)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}
func (m *model) saveManifest(path, content string) tea.Cmd {
	g := m.generation
	return func() tea.Msg { return manifestSavedMsg{g, path, writeManifest(path, content)} }
}
func (m *model) promptManifestExport() tea.Cmd {
	o, ok := m.current()
	if !ok {
		return nil
	}
	format := m.manifestFormat()
	content, err := m.exportContent(format, false)
	if err != nil {
		m.notice = kube.Safe(err.Error())
		return nil
	}
	m.manifestContent = content
	m.manifestReturn = m.mode
	m.mode = "manifest-save"
	m.input.Prompt = "Save manifest to: "
	m.input.SetValue(o.GetName() + "." + format)
	m.input.CursorEnd()
	return m.input.Focus()
}
func (m *model) manifestInputKey(v tea.KeyPressMsg) tea.Cmd {
	if v.String() == "esc" {
		m.mode = m.manifestReturn
		m.manifestContent = ""
		m.input.Blur()
		return nil
	}
	if v.String() == "enter" {
		path, content := m.input.Value(), m.manifestContent
		m.mode = m.manifestReturn
		m.manifestContent = ""
		m.input.Blur()
		return m.saveManifest(path, content)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(v)
	return cmd
}
func (m *model) manifestCommand(arg string, copy bool) tea.Cmd {
	args, err := editorArgs(strings.TrimSpace(arg))
	if arg == "" {
		args = nil
		err = nil
	}
	if err != nil {
		m.notice = "Invalid manifest options: " + kube.Safe(err.Error())
		return nil
	}
	format, clean, path := m.manifestFormat(), false, ""
	for _, value := range args {
		switch value {
		case "yaml", "json":
			format = value
		case "clean":
			clean = true
		default:
			if path != "" || copy {
				m.notice = "Use :copy [yaml|json] [clean], or :export [yaml|json] [clean] PATH"
				return nil
			}
			path = value
		}
	}
	if copy {
		return m.copyManifest(format, clean)
	}
	if path == "" {
		m.notice = "Use :export [yaml|json] [clean] PATH (quote paths containing spaces)"
		return nil
	}
	content, err := m.exportContent(format, clean)
	if err != nil {
		m.notice = kube.Safe(err.Error())
		return nil
	}
	return m.saveManifest(path, content)
}
