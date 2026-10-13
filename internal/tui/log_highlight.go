package tui

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/z0mbix/kqt/internal/kube"
)

// Lowercase levels are recognized only in explicit fields or brackets, so
// ordinary prose such as "no error occurred" does not look like an error entry.
var logLevels = regexp.MustCompile(`(?i:\b(?:level|severity|lvl)[=:][ \t]*["']?(TRACE|DEBUG|INFO|NOTICE|WARN|WARNING|ERROR|ERR|FATAL|PANIC|CRITICAL|CRIT)\b["']?)|\b(TRACE|DEBUG|INFO|NOTICE|WARN|WARNING|ERROR|ERR|FATAL|PANIC|CRITICAL|CRIT)\b|\[(?i:(TRACE|DEBUG|INFO|NOTICE|WARN|WARNING|ERROR|ERR|FATAL|PANIC|CRITICAL|CRIT))\]`)
var logSource = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*/[a-z0-9][a-z0-9.-]*:[a-z0-9][a-z0-9.-]*$`)

func logLevelColour(level string) string {
	switch strings.ToUpper(level) {
	case "TRACE", "DEBUG":
		return "muted"
	case "INFO", "NOTICE":
		return "success"
	case "WARN", "WARNING":
		return "warning"
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL", "CRIT":
		return "error"
	default:
		return ""
	}
}

func isLogLevelKey(quoted string) bool {
	var key string
	if json.Unmarshal([]byte(quoted), &key) != nil {
		return false
	}
	switch strings.ToLower(key) {
	case "level", "severity", "lvl":
		return true
	default:
		return false
	}
}

func jsonLogLevelColour(quoted string) string {
	var value string
	if json.Unmarshal([]byte(quoted), &value) != nil {
		return ""
	}
	return logLevelColour(value)
}

func levelSpan(match []int) (int, int) {
	for i := 2; i < len(match); i += 2 {
		if match[i] >= 0 {
			return match[i], match[i+1]
		}
	}
	return 0, 0
}

func (t theme) renderLogLevels(text string) string {
	var out strings.Builder
	end := 0
	for _, match := range logLevels.FindAllStringSubmatchIndex(text, -1) {
		start, stop := levelSpan(match)
		out.WriteString(text[end:start])
		out.WriteString(t.style(logLevelColour(text[start:stop])).Render(text[start:stop]))
		end = stop
	}
	out.WriteString(text[end:])
	return out.String()
}

// Recognize the prefixes kqt/Kubernetes add without guessing at braces inside
// an ordinary message. Complete JSON objects and arrays are coloured in place:
// no decoding/re-encoding of payloads, lost number precision, or extra lines.
func (t theme) renderLogLine(line string) string {
	line = kube.SafeLogLines(line)
	var out strings.Builder
	for line != "" {
		body := strings.TrimLeft(line, " \t")
		out.WriteString(line[:len(line)-len(body)])
		line = body
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "[") {
			if end := strings.IndexByte(line, ']'); end >= 0 {
				source := line[1:end]
				if source == "kqt" || logSource.MatchString(source) {
					out.WriteString(t.style("muted").Render(line[:end+1]))
					line = line[end+1:]
					continue
				}
			}
		}
		token := line
		if end := strings.IndexAny(line, " \t"); end >= 0 {
			token = line[:end]
		}
		if len(token) >= 20 && token[4] == '-' && token[10] == 'T' {
			if _, err := time.Parse(time.RFC3339Nano, token); err == nil {
				out.WriteString(t.style("muted").Render(token))
				line = line[len(token):]
				continue
			}
		}
		if match := logLevels.FindStringSubmatchIndex(line); match != nil && match[0] == 0 {
			out.WriteString(t.renderLogLevels(line[:match[1]]))
			line = line[match[1]:]
			if strings.HasPrefix(line, ":") {
				out.WriteByte(':')
				line = line[1:]
			}
			continue
		}
		break
	}
	if len(line) > 0 && (line[0] == '{' || line[0] == '[') && json.Valid([]byte(line)) {
		out.WriteString(t.renderJSONTokens(line, true))
	} else {
		out.WriteString(t.renderLogLevels(line))
	}
	return out.String()
}
