package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestLiveLogPauseSearchBoundsAndStaleMessages(t *testing.T) {
	m := demoModel(t)
	m.mode = "logs"
	m.logFollow = true
	m.logFeed = newLogFeed()
	m.logPaused = true
	m.panel.SetContent("frozen")
	m.logFeed.append("hello \x1b[31m red")
	m.logPulse(logPulseMsg{m.generation, m.logToken, m.logFeed})
	if m.panel.GetContent() != "frozen" {
		t.Fatal("pause moved the viewport")
	}
	m.logPaused = false
	m.logSearch = "hello"
	m.renderLogs()
	if !strings.Contains(m.panel.GetContent(), "hello") {
		t.Fatal("search hid a match")
	}
	token := m.logToken
	feed := m.logFeed
	m.closeLog()
	feed.append("stale data")
	m.logPulse(logPulseMsg{m.generation, token, feed})
	if strings.Contains(m.panel.GetContent(), "stale data") {
		t.Fatal("closed log stream updated panel")
	}
	f := newLogFeed()
	for i := 0; i < logBufferLines+20; i++ {
		f.append(strings.Repeat("x", 512))
	}
	lines, dropped, _, _ := f.drain()
	size := 0
	for _, line := range lines {
		size += len(line) + 1
	}
	if size > logBufferBytes || dropped == 0 || len(lines) > logBufferLines {
		t.Fatalf("unbounded log queue: %d %d %d", size, len(lines), dropped)
	}
}

func TestMetricsSortByActualUsageAndLeaveManifestUntouched(t *testing.T) {
	m := demoModel(t)
	m.metricsEnabled = true
	m.sortColumn = "memory"
	first, second := m.rows[0], m.rows[1]
	before := kube.Manifest(first, true)
	m.usage = map[string]kube.Usage{kube.UsageKey(first): {CPUMilli: 100, MemoryBytes: 1024 * 1024 * 1024}, kube.UsageKey(second): {CPUMilli: 20, MemoryBytes: 128 * 1024 * 1024}}
	m.rebuild("")
	if m.visible[0].GetUID() != second.GetUID() || m.column(first, "memory") != "1.0 GiB" {
		t.Fatal("metrics sort used display strings")
	}
	if kube.Manifest(first, true) != before {
		t.Fatal("metrics polluted original manifest")
	}
	m.descending = true
	m.rebuild("")
	if m.visible[0].GetUID() != first.GetUID() {
		t.Fatal("missing usage sorted ahead of known usage")
	}
}

func TestForwardParsingAndSessionCancellation(t *testing.T) {
	for _, value := range []string{"8080", "-1:80", "65536:80", "0:0", "0:70000", "0:80;cmd"} {
		if _, _, err := parsePorts(value); err == nil {
			t.Fatalf("accepted invalid ports: %s", value)
		}
	}
	local, remote, err := parsePorts("0:8080")
	if err != nil || local != 0 || remote != 8080 {
		t.Fatal("random local port unavailable")
	}
	m := demoModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.forwards = map[uint64]*forwardSession{1: {id: 1, state: "active", cancel: cancel}}
	m.connect("demo")
	if ctx.Err() == nil || m.forwards != nil {
		t.Fatal("context switch left forward running")
	}
	m.forwardUpdate(forwardMsg{generation: m.generation - 1, id: 1, local: 8080})
	if len(m.forwards) != 0 {
		t.Fatal("old-context forward leaked into new session")
	}
}

func TestScopedEventsRejectOldResponse(t *testing.T) {
	m := demoModel(t)
	cmd := m.openEvents()
	msg := cmd()
	m.closeEvents()
	m.mode = ""
	before := m.panel.GetContent()
	m.Update(msg)
	if m.panel.GetContent() != before {
		t.Fatal("closed events request updated panel")
	}
}

func TestLogExportDoesNotOverwriteExistingFile(t *testing.T) {
	m := demoModel(t)
	path := filepath.Join(t.TempDir(), "logs.txt")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	m.mode = "log-save"
	m.input.SetValue(path)
	m.logLines = []string{"replacement"}
	cmd := m.logsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	message := cmd().(logSavedMsg)
	if message.err == nil {
		t.Fatal("overwrote an existing file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "keep" {
		t.Fatal("existing export changed")
	}
}

func TestShellRequiresTTYAndRunningContainer(t *testing.T) {
	adapter := &shellCommand{ctx: context.Background(), in: strings.NewReader(""), target: unstructured.Unstructured{}}
	if err := adapter.Run(); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatal("accepted nonterminal shell")
	}
	m := demoModel(t)
	before := m.descending
	key(m, "s")
	if m.descending != before || !strings.Contains(m.notice, "live Kubernetes") {
		t.Fatal("shell key still changed sort")
	}
	key(m, "S")
	if m.descending == before {
		t.Fatal("reverse sort shortcut lost")
	}
}

func TestShellEscapeDisconnectsWithoutSendingFollowingInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := shellInput{reader: strings.NewReader("echo\x1dnot-remote-input"), disconnect: cancel}
	data := make([]byte, 64)
	n, err := input.Read(data)
	if err != nil || string(data[:n]) != "echo" || ctx.Err() == nil {
		t.Fatalf("disconnect was sent to the remote shell: %q %v", data[:n], err)
	}
}

func TestShellQueueConsumesDisconnectBeforeRemoteStartsReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := &shellQueue{ctx: ctx, notify: make(chan struct{}, 1)}
	if err := queue.pump(shellInput{reader: strings.NewReader("typed-before-connection\x1d"), disconnect: cancel}); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("startup disconnect was not consumed")
	}
	data := make([]byte, 64)
	n, err := queue.Read(data)
	if n != 0 || err == nil {
		t.Fatal("canceled input reached the remote session")
	}
}
