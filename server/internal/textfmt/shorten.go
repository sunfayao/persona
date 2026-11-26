package textfmt

import (
	"strings"
)

// Shorten cuts s to at most n runes, with … when it cut.
func Shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
