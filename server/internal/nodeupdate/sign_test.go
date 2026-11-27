package nodeupdate

import (
	"fmt"
	"strings"
	"sync"
)

// signLog collects what the manager logs about signing.
type signLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *signLog) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *signLog) has(part string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, part) {
			return true
		}
	}
	return false
}
