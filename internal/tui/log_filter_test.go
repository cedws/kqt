package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestLogHighlightsManyDisjointMatches(t *testing.T) {
	line := strings.Repeat("a ", 8192)
	query, _ := compileLogFilter("include:a")
	styled := builtThemes()["nord"].highlightLogMatches(line, query.spans(line))
	if ansi.Strip(styled) != line || strings.Count(styled, "\x1b[4m") != 8192 {
		t.Fatal("long disjoint matches changed text or lost underlines")
	}
}

func TestLogHighlightsRestoreSyntaxAfterAndAcrossMatches(t *testing.T) {
	base := "\x1b[31mab\x1b[32m cd\x1b[0m tail"
	render := func(text string) uv.ScreenBuffer {
		screen := uv.NewScreenBuffer(40, 1)
		uv.NewStyledString(text).Draw(screen, uv.Rect(0, 0, 40, 1))
		return screen
	}
	original := render(base)
	for _, value := range []string{"include:b", `re:"b c"`} {
		query, _ := compileLogFilter(value)
		highlighted := builtThemes()["nord"].highlightLogMatches(base, query.spans(ansi.Strip(base)))
		cells := render(highlighted)
		if ansi.Strip(highlighted) != ansi.Strip(base) {
			t.Fatal("highlight changed styled text")
		}
		for _, i := range []int{0, 4, 5, 6, 7, 8} {
			if !cells.CellAt(i, 0).Style.Equal(&original.CellAt(i, 0).Style) {
				t.Fatalf("%s: syntax style leaked at cell %d", value, i)
			}
		}
	}
}

func BenchmarkLogHighlightDisjointMatches(b *testing.B) {
	for _, size := range []int{2048, 4096, 8192, 16384} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			line := strings.Repeat("a ", size/2)
			query, _ := compileLogFilter("include:a")
			spans, palette := query.spans(line), builtThemes()["nord"]
			b.SetBytes(int64(size))
			b.ResetTimer()
			for b.Loop() {
				palette.highlightLogMatches(line, spans)
			}
		})
	}
}

func TestLogQueriesSupportMixedFormatsAndExactJSONValues(t *testing.T) {
	jsonLine := `[apps/web-0003:web] 2026-10-15T12:00:00Z {"level":"warning","message":"no ERROR occurred","request_id":"abc","http":{"status":500},"id":18446744073709551615,"ready":false,"empty":"","nil":null}`
	for _, tc := range []struct {
		query, line string
		want        bool
	}{
		{"no ERROR occurred", jsonLine, true},
		{"ERROR no", jsonLine, false},
		{"include:ERROR !health", jsonLine, true},
		{"include:ERROR exclude:occurred", jsonLine, false},
		{`re:"web-\d+"`, jsonLine, true},
		{`re:warning !re:ERROR`, jsonLine, false},
		{`re:(?i)WARNING`, jsonLine, true},
		{"level=warn", jsonLine, true},
		{"level=error", jsonLine, false},
		{"level>=warn", jsonLine, true},
		{"severity=warn,error !level=fatal", jsonLine, true},
		{"level=error", "no error occurred", false},
		{"level=error", "time=now level=ERROR failed", true},
		{"level>=warn", "INFO ready", false},
		{"level>=warn", "[critical] failed", true},
		{"request_id=abc http.status=500", jsonLine, true},
		{"request_id=ABC", jsonLine, false},
		{"id=18446744073709551615", jsonLine, true},
		{"id=18446744073709551614", jsonLine, false},
		{"ready=false empty= nil=null", jsonLine, true},
		{"!missing=value", jsonLine, true},
		{"missing=value", jsonLine, false},
		{`message="no ERROR occurred"`, jsonLine, true},
		{"duration_ms=1200", "WARN duration_ms=1200", true},
		{"include:duration_ms=1200", `{"message":"duration_ms=1200"}`, true},
		{"level=error", `INFO: {"\u006cevel":"er\u0072or"}`, true},
		{"level=info", `INFO: {"message":"no ERROR occurred"}`, true},
		{"level=error", `INFO: {"message":"no ERROR occurred"}`, false},
		{"level=error", `{"message":"no ERROR occurred"}`, false},
		{"request_id=abc", `ordinary message {"request_id":"abc"}`, false},
	} {
		t.Run(tc.query+tc.line, func(t *testing.T) {
			query, err := compileLogFilter(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if got := query.matches(tc.line); got != tc.want {
					t.Fatalf("query %q matched=%v, want %v", tc.query, got, tc.want)
				}
			}
		})
	}
	for _, query := range []string{"re:[", "re:", "include:", "level=unknown", "level>=warn,error", `include:"unfinished`, "field>=1", strings.Repeat("a", 4097)} {
		if _, err := compileLogFilter(query); err == nil {
			t.Fatalf("accepted invalid query %q", query)
		}
	}
}

func TestLogMatchHighlightingPreservesGraphemesAndThemeSyntax(t *testing.T) {
	for name, palette := range builtThemes() {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct{ query, line string }{
				{"include:界 include:e", "INFO 界 e\u0301 👩‍💻 ready"},
				{"re:👩", "INFO 👩‍💻 ready"},
				{"request_id=abc", `{"level":"warn","request_id":"abc","id":18446744073709551615}`},
				{"level=warn", `{"level":"warn","message":"ready"}`},
			} {
				query, err := compileLogFilter(tc.query)
				if err != nil {
					t.Fatal(err)
				}
				base := palette.renderLogLine(tc.line)
				highlighted := palette.highlightLogMatches(base, query.spans(tc.line))
				if highlighted == base || ansi.Strip(highlighted) != tc.line || ansi.StringWidth(highlighted) != ansi.StringWidth(tc.line) {
					t.Fatalf("highlight changed text or failed: %q", highlighted)
				}
			}
		})
	}
}

func TestInvalidLogQueryRetainsPreviousFilterAndCanBeCancelled(t *testing.T) {
	m := demoModel(t)
	m.logFollow = false
	m.loadLogs()
	m.logLines = []string{"INFO ready", "WARN slow", "INFO health"}
	m.logSearch = "include:INFO !health"
	m.renderLogs()
	m.logsInput("log-search")
	m.input.SetValue("re:[")
	m.logsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != "log-search" || m.logSearch != "include:INFO !health" || m.logFilterError == "" || !strings.Contains(ansi.Strip(m.View().Content), "invalid regex") {
		t.Fatal("invalid query replaced the previous filter or had no visible error")
	}
	m.logsInputKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != "logs" || m.logFilterError != "" || m.logContent() != "INFO ready" {
		t.Fatal("cancelling query changed the previous filter")
	}
	m.logsInput("log-search")
	m.input.SetValue("level=warn")
	m.logsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != "logs" || m.logContent() != "WARN slow" || strings.ContainsRune(m.logContent(), '\x1b') {
		t.Fatal("filtered export contains hidden lines or styling")
	}
}
