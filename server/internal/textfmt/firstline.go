package textfmt

import (
	"strings"
)

// CompactFirstLine is the first line of s, cut to 200 bytes on a whole character.
func CompactFirstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return Cut(s, 200)
}
