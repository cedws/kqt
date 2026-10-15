package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/z0mbix/kqt/internal/kube"
)

func expandDownloadPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("enter a file or directory path")
	}
	if kube.Safe(path) != path {
		return "", fmt.Errorf("path must not contain control characters")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("home shorthand must be ~ or ~/path")
	}
	return filepath.Clean(path), nil
}

func downloadDirectory(directory string) (string, error) {
	if directory == "" {
		return "", nil
	}
	path, err := expandDownloadPath(directory)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

// Relative filenames use the configured directory. An absolute or home-relative
// path overrides it. Suggestions become absolute when a directory is set, so
// submitting an unchanged prompt never applies the directory twice.
func downloadPath(directory, path string) (string, error) {
	path, err := expandDownloadPath(path)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(path) || directory == "" {
		return path, nil
	}
	directory, err = downloadDirectory(directory)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, path), nil
}

func saveDownload(directory, path, content string) (string, error) {
	destination, err := downloadPath(directory, path)
	if err != nil {
		return path, err
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return destination, err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return destination, err
	}
	_, err = file.WriteString(content)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(destination)
	}
	return destination, err
}

func (m *model) downloadDirectoryCommand(arg string) {
	if arg == "" {
		path, err := downloadDirectory(m.settings.Config.DownloadDir)
		if err != nil {
			m.notice = "Invalid download directory: " + kube.Safe(err.Error())
			return
		}
		if path == "" {
			path = "current directory"
		}
		m.notice = "Download directory: " + kube.Safe(path)
		return
	}
	value := ""
	if arg != "clear" {
		args, err := editorArgs(arg)
		if err != nil || len(args) != 1 {
			m.notice = "Use :download-dir PATH (quote spaces), or :download-dir clear"
			return
		}
		value = args[0]
	}
	if _, err := downloadDirectory(value); err != nil {
		m.notice = "Invalid download directory: " + kube.Safe(err.Error())
		return
	}
	if err := m.settings.savePatch(map[string]any{"download_dir": value}); err != nil {
		m.notice = "Unable to save download directory: " + kube.Safe(err.Error())
		return
	}
	m.settings.Config.DownloadDir = value
	m.downloadDirectoryCommand("")
}
