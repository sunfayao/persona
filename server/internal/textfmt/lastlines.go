package textfmt

import (
	"strings"
)

// LastLines returns the last n lines of s, excluding trailing newlines.
func LastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
