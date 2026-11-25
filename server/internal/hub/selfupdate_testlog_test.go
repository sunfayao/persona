package hub

import (
	"strings"
	"sync"
)

// syncLog collects the hub's log lines.
type syncLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, strings.TrimSpace(string(p)))
	return len(p), nil
}

func (l *syncLog) about(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			n++
		}
	}
	return n
}
