package kube

import "bufio"

const maxLogLineBytes = 1024 * 1024

// Keep a bounded prefix, but drain the whole record so an oversized line cannot
// block subsequent output or strand reconnects at the preceding timestamp.
func readLogLine(reader *bufio.Reader) (string, bool, error) {
	var line []byte
	truncated := false
	for {
		fragment, more, err := reader.ReadLine()
		if err != nil {
			return string(line), truncated, err
		}
		remaining := maxLogLineBytes - len(line)
		line = append(line, fragment[:min(len(fragment), remaining)]...)
		truncated = truncated || len(fragment) > remaining
		if !more {
			return string(line), truncated, nil
		}
	}
}
