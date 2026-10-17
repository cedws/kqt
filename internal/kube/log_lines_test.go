package kube

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReadLogLineDrainsOversizedRecords(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", ""} {
		t.Run("ending="+ending, func(t *testing.T) {
			input := strings.Repeat("x", 3*maxLogLineBytes) + ending
			if ending != "" {
				input += "after\n\nlast"
			}
			reader := bufio.NewReader(strings.NewReader(input))
			line, truncated, err := readLogLine(reader)
			if len(line) != maxLogLineBytes || !truncated || (err != nil && err != io.EOF) {
				t.Fatalf("oversized line: length=%d truncated=%t err=%v", len(line), truncated, err)
			}
			if ending != "" {
				for _, want := range []string{"after", "", "last"} {
					got, cut, err := readLogLine(reader)
					if got != want || cut || err != nil {
						t.Fatalf("subsequent line = %q, %t, %v; want %q", got, cut, err, want)
					}
				}
			}
			if _, _, err := readLogLine(reader); err != io.EOF {
				t.Fatalf("expected EOF, got %v", err)
			}
		})
	}
}

type oversizedLogStream struct {
	*Demo
	calls int
}

func (b *oversizedLogStream) StreamLogs(_ context.Context, _, _, _ string, since *time.Time, _ ...LogOptions) (io.ReadCloser, error) {
	b.calls++
	if b.calls == 1 {
		return io.NopCloser(strings.NewReader("2026-10-17T10:00:00Z " + strings.Repeat("x", 2*maxLogLineBytes) + "\n2026-10-17T10:00:01Z after oversized\n")), nil
	}
	want := time.Date(2026, 10, 17, 10, 0, 1, 0, time.UTC)
	if since == nil || !since.Equal(want) {
		return nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(strings.NewReader("2026-10-17T10:00:01Z after oversized\n2026-10-17T10:00:02Z reconnected\n")), nil
}

func TestFollowLogsContinueAndCheckpointAfterOversizedLine(t *testing.T) {
	b := &oversizedLogStream{Demo: NewDemo()}
	r, _ := Resolve(Builtins(), "pods")
	pod, _ := b.Get(context.Background(), r, "default", "web-7c9f6d8b5-0000")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var lines []string
	followSource(ctx, b, b, logSource{string(pod.GetUID()), pod.GetNamespace(), pod.GetName(), "web"}, LogOptions{}, func(line string) {
		lines = append(lines, line)
		if strings.Contains(line, "reconnected") {
			cancel()
		}
	})
	if b.calls != 2 || len(lines) != 3 || !strings.Contains(lines[0], "line truncated") || !strings.Contains(lines[1], "after oversized") || !strings.Contains(lines[2], "reconnected") {
		t.Fatalf("lost output/checkpoint: calls=%d lines=%d", b.calls, len(lines))
	}
}
