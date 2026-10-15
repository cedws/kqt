package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDownloadPathsDefaultsHomeOverridesAndRelativeDirectories(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "downloads with spaces")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Abs("relative downloads")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ directory, path, want string }{
		{"", "resource.yaml", "resource.yaml"},
		{directory, "resource.yaml", filepath.Join(directory, "resource.yaml")},
		{directory, "nested/東京.json", filepath.Join(directory, "nested", "東京.json")},
		{directory, filepath.Join(directory, "resource.yaml"), filepath.Join(directory, "resource.yaml")},
		{directory, "~/Downloads/resource.yaml", filepath.Join(home, "Downloads", "resource.yaml")},
		{"~/Downloads/kqt", "resource.yaml", filepath.Join(home, "Downloads", "kqt", "resource.yaml")},
		{"relative downloads", "resource.yaml", filepath.Join(relative, "resource.yaml")},
	} {
		got, err := downloadPath(tc.directory, tc.path)
		if err != nil || got != tc.want {
			t.Fatalf("downloadPath(%q, %q) = %q, %v; want %q", tc.directory, tc.path, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", " ", "\x00file", "path\nfile", "\x1b[2Jfile", "\u202efile", "~another/file"} {
		if _, err := downloadPath(directory, bad); err == nil {
			t.Fatalf("accepted invalid file path %q", bad)
		}
	}
}

func TestDownloadDirectorySavedInYAMLAndTOMLWithoutCreatingIt(t *testing.T) {
	for _, extension := range []string{"yaml", "toml"} {
		t.Run(extension, func(t *testing.T) {
			m := demoModel(t)
			m.settings.Path = filepath.Join(t.TempDir(), "config."+extension)
			if extension == "yaml" {
				if err := os.WriteFile(m.settings.Path, []byte("version: 1\neditor: 'custom-editor --wait' # keep this\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			directory := filepath.Join(t.TempDir(), "new", "downloads with spaces")
			m.command("download-dir " + strconv.Quote(directory))
			loaded, err := LoadSettings(m.settings.Path)
			if err != nil || loaded.Config.DownloadDir != directory || m.settings.Config.DownloadDir != directory {
				t.Fatalf("directory was not saved: %v, %s", err, m.notice)
			}
			if _, err := os.Stat(directory); !os.IsNotExist(err) {
				t.Fatal("setting the default directory created it before a save")
			}
			if extension == "yaml" {
				data, _ := os.ReadFile(m.settings.Path)
				if !strings.Contains(string(data), "custom-editor --wait") || !strings.Contains(string(data), "# keep this") {
					t.Fatal("changing the directory lost unrelated preferences or comments")
				}
			}
			m.command("download-dir")
			if !strings.Contains(m.notice, directory) {
				t.Fatal("directory query did not report the active default")
			}
			m.command("download-dir clear")
			loaded, err = LoadSettings(m.settings.Path)
			if err != nil || loaded.Config.DownloadDir != "" || m.settings.Config.DownloadDir != "" || !strings.Contains(m.notice, "current directory") {
				t.Fatal("clear did not persist the original default")
			}
		})
	}
}

func TestInvalidDownloadSettingsAndFailedSaveDoNotChangePreference(t *testing.T) {
	m := demoModel(t)
	old := filepath.Join(t.TempDir(), "downloads")
	m.command("download-dir " + strconv.Quote(old))
	for _, command := range []string{"download-dir one two", "download-dir 'unfinished", "download-dir ~another/file"} {
		m.command(command)
		if m.settings.Config.DownloadDir != old {
			t.Fatal("invalid command changed the preference")
		}
	}
	broken := []byte("version: [\n")
	if err := os.WriteFile(m.settings.Path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	m.command("download-dir " + strconv.Quote(filepath.Join(t.TempDir(), "other")))
	after, _ := os.ReadFile(m.settings.Path)
	if m.settings.Config.DownloadDir != old || string(after) != string(broken) || !strings.Contains(m.notice, "Unable to save download directory") {
		t.Fatal("failed persistence changed the active directory or overwrote invalid config")
	}
	for _, bad := range []string{"\x00bad", "bad\npath", "~another/path"} {
		data := "version: 1\ndownload_dir: " + strconv.Quote(bad) + "\n"
		if err := os.WriteFile(m.settings.Path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSettings(m.settings.Path); err == nil {
			t.Fatal("invalid directory in config was accepted")
		}
	}
}

func assertPrivateDownload(t *testing.T, path, text string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), text) {
		t.Fatalf("wrong downloaded contents at %s: %q, %v", path, data, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("download file is not private")
	}
}

func TestAllSavePromptsAndManifestCommandUseDownloadDirectory(t *testing.T) {
	m := demoModel(t)
	directory := filepath.Join(t.TempDir(), "new", "downloads with spaces")
	m.settings.Config.DownloadDir = directory
	m.openDetails(false, true)
	o, _ := m.current()
	m.promptManifestExport()
	manifest := filepath.Join(directory, o.GetName()+".json")
	if m.input.Value() != manifest {
		t.Fatal("manifest prompt ignored the directory")
	}
	m.Update(m.manifestInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
	assertPrivateDownload(t, manifest, `"kind": "Pod"`)
	m.mode = ""
	m.Update(m.manifestCommand("yaml clean nested/reusable.yaml", false)())
	assertPrivateDownload(t, filepath.Join(directory, "nested", "reusable.yaml"), "kind: Pod")

	m.mode = "logs"
	m.logLines = []string{"download log marker"}
	m.logsInput("log-save")
	if m.input.Value() != filepath.Join(directory, "kqt-logs.log") {
		t.Fatal("logs prompt ignored the directory")
	}
	m.Update(m.logsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
	assertPrivateDownload(t, filepath.Join(directory, "kqt-logs.log"), "download log marker")
	m.mode = ""
	m.Update(m.openEvents()())
	for _, tc := range []struct{ key, filename, text string }{
		{"1", "kqt-events.log", "Containers"},
		{"2", "kqt-labels.txt", "Labels"},
		{"3", "kqt-annotations.txt", "Annotations"},
		{"4", "kqt-finalizers.txt", "Finalizers"},
	} {
		key(m, tc.key)
		m.eventsInput("event-save")
		path := filepath.Join(directory, tc.filename)
		if m.input.Value() != path {
			t.Fatalf("%s prompt ignored the directory", tc.text)
		}
		m.Update(m.eventsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
		assertPrivateDownload(t, path, tc.text)
	}
	key(m, "esc")
	m.command("events")
	pulseEvent(m, liveEvent("download-event", 100, 1), "ADDED")
	m.eventsInput("event-save")
	if m.input.Value() != filepath.Join(directory, "kqt-events.log") {
		t.Fatal("global event export ignored the directory")
	}
	// Use another relative name to avoid the selected status export above.
	m.input.SetValue("all-events.log")
	m.Update(m.eventsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
	assertPrivateDownload(t, filepath.Join(directory, "all-events.log"), "retry web")
	info, _ := os.Stat(directory)
	if info.Mode().Perm() != 0700 {
		t.Fatal("new download directory is not private")
	}
}

func TestDownloadOverridesNoOverwriteFailureAndCapturedDirectory(t *testing.T) {
	m := demoModel(t)
	directory := filepath.Join(t.TempDir(), "default")
	m.settings.Config.DownloadDir = directory
	outside := filepath.Join(t.TempDir(), "override.json")
	m.Update(m.saveManifest(outside, "outside")())
	assertPrivateDownload(t, outside, "outside")
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("an absolute override created the unused default directory")
	}
	cmd := m.saveManifest("captured.yaml", "captured directory")
	m.settings.Config.DownloadDir = filepath.Join(t.TempDir(), "other")
	m.Update(cmd())
	assertPrivateDownload(t, filepath.Join(directory, "captured.yaml"), "captured directory")
	if _, err := saveDownload(directory, "captured.yaml", "replace"); err == nil {
		t.Fatal("download overwrote an existing file")
	}
	assertPrivateDownload(t, filepath.Join(directory, "captured.yaml"), "captured directory")
	if _, err := saveDownload(outside, "child/file.txt", "bad"); err == nil {
		t.Fatal("a file was accepted as the download directory")
	}
	assertPrivateDownload(t, outside, "outside")
}

func TestDownloadPromptsKeepLongPathsAndCancellationCreatesNoFiles(t *testing.T) {
	m := demoModel(t)
	directory := filepath.Join(t.TempDir(), strings.Repeat("a", 140), strings.Repeat("b", 140))
	m.settings.Config.DownloadDir = directory
	for _, prompt := range []func(){
		func() { m.promptManifestExport() },
		func() { m.mode = "logs"; m.logsInput("log-save") },
		func() { m.mode = "events"; m.eventsInput("event-save") },
	} {
		prompt()
		if !strings.HasPrefix(m.input.Value(), directory+string(filepath.Separator)) {
			t.Fatal("a long default download path was truncated")
		}
		for _, width := range []int{40, 80, 120} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			visibleName := filepath.Base(m.input.Value())
			if width < 80 && len(visibleName) > 12 {
				visibleName = visibleName[len(visibleName)-12:]
			}
			if ansi.StringWidth(m.input.View()) > width || !strings.Contains(ansi.Strip(m.input.View()), visibleName) {
				t.Fatalf("a save prompt clipped the filename at width %d: %q", width, ansi.Strip(m.input.View()))
			}
		}
		key(m, "esc")
		if m.input.CharLimit != 256 {
			t.Fatal("leaving a save prompt changed other input limits")
		}
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("cancelled prompts created download directories")
	}
	m.mode = ""
	m.startInput("command", "download-d")
	m.completeCommand(false)
	if m.input.Value() != "download-dir" {
		t.Fatal("download directory command completion failed")
	}
}
