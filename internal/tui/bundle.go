package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type bundleSavedMsg struct {
	generation, token uint64
	path              string
	warnings          int
	err               error
}

func (m *model) cancelBundle() {
	m.bundleToken++
	if m.bundleCancel != nil {
		m.bundleCancel()
		m.bundleCancel = nil
	}
	m.bundleTarget = unstructured.Unstructured{}
	m.forgetHistoryExecution("bundle")
}
func (m *model) bundleCommand(arg string) tea.Cmd {
	args, err := editorArgs(arg)
	if err != nil && strings.TrimSpace(arg) != "" {
		m.notice = "Use :bundle [--logs] [PATH]"
		return nil
	}
	options := kube.BundleOptions{Context: m.contextName}
	path := ""
	for _, a := range args {
		if a == "--logs" {
			options.IncludeLogs = true
		} else if strings.HasPrefix(a, "-") || path != "" {
			m.notice = "Use :bundle [--logs] [PATH]"
			return nil
		} else {
			path = a
		}
	}
	target, ok := m.current()
	if !ok || m.backend == nil {
		m.notice = "Select a resource to export a diagnostic bundle"
		return nil
	}
	if target.GetKind() == "Secret" {
		m.notice = "Secret resources are excluded from diagnostic bundles"
		return nil
	}
	m.cancelBundle()
	m.bundleTarget = *target.DeepCopy()
	m.bundleOptions = options
	m.clearSecret()
	m.updateInspector()
	if path != "" {
		return m.saveBundle(path)
	}
	m.mode = "bundle-save"
	m.input.Prompt = "Save bundle to: "
	m.input.CharLimit = 4096
	name := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, target.GetName())
	path, _ = downloadPath(m.settings.Config.DownloadDir, fmt.Sprintf("kqt-%s-%s-diagnostics.tar.gz", name, time.Now().UTC().Format("20060102-150405")))
	m.input.SetValue(path)
	m.input.CursorEnd()
	m.resize()
	m.notice = "Bundle excludes Secret resources; logs are opt-in with --logs"
	return m.input.Focus()
}
func (m *model) saveBundle(path string) tea.Cmd {
	if _, err := downloadPath(m.settings.Config.DownloadDir, path); err != nil {
		m.notice = kube.Safe(err.Error())
		return nil
	}
	ctx, cancel := context.WithTimeout(m.session.ctx, 45*time.Second)
	m.bundleCancel = cancel
	g, token, b, resources, target, options, directory := m.generation, m.bundleToken, m.backend, m.resources, m.bundleTarget, m.bundleOptions, m.settings.Config.DownloadDir
	m.mode = "bundle-loading"
	m.input.Blur()
	m.panelTitle = "Collect diagnostic bundle"
	m.panel.SoftWrap = true
	m.resize()
	m.panel.SetContent("Collecting " + kube.Safe(target.GetKind()+" "+target.GetNamespace()+"/"+target.GetName()) + "…\n\nSecret resources are excluded.\nLogs included: " + fmt.Sprint(options.IncludeLogs) + "\n\nEsc cancels.")
	return func() tea.Msg {
		defer cancel()
		bundle, err := kube.CollectBundle(ctx, b, resources, target, options)
		if err != nil {
			return bundleSavedMsg{generation: g, token: token, err: err}
		}
		archive, err := bundle.Archive()
		if err != nil {
			return bundleSavedMsg{generation: g, token: token, err: err}
		}
		path, err := saveDownloadContext(ctx, directory, path, archive)
		return bundleSavedMsg{g, token, path, len(bundle.Warnings), err}
	}
}
func (m *model) bundleKey(v tea.KeyPressMsg) tea.Cmd {
	switch v.String() {
	case "esc":
		m.cancelBundle()
		m.mode = ""
		m.input.Blur()
		m.input.CharLimit = 256
		m.resize()
		m.notice = "Bundle export cancelled"
		return nil
	case "enter":
		if m.mode == "bundle-save" {
			return m.saveBundle(m.input.Value())
		}
	}
	if m.mode == "bundle-save" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(v)
		return cmd
	}
	return nil
}
func (m *model) bundleSaved(v bundleSavedMsg) tea.Cmd {
	if v.generation != m.generation || v.token != m.bundleToken || m.mode != "bundle-loading" {
		return nil
	}
	if m.bundleCancel != nil {
		m.bundleCancel()
		m.bundleCancel = nil
	}
	m.bundleTarget = unstructured.Unstructured{}
	m.input.CharLimit = 256
	m.mode = ""
	m.resize()
	if v.err != nil {
		m.notice = "Unable to save bundle: " + kube.Safe(v.err.Error())
	} else {
		m.notice = "Diagnostic bundle saved to " + kube.Safe(v.path)
		if v.warnings > 0 {
			m.notice += fmt.Sprintf(" · %d capture notes in README.txt", v.warnings)
		}
	}
	return nil
}
