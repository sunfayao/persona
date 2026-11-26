package screen

import (
	"strings"
	"unicode"
)

// StripSpinner drops the glyph a busy TUI puts before its title ("✳ fix the build", "⠸ fix the build").
func StripSpinner(title string) string {
	r := []rune(title)
	if len(r) >= 2 && r[1] == ' ' && !unicode.IsLetter(r[0]) && !unicode.IsDigit(r[0]) && r[0] > unicode.MaxASCII {
		return strings.TrimSpace(string(r[2:]))
	}
	return title
}
