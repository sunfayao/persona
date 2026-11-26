package screen

import (
	"strings"
	"time"
)

// Bracketed paste markers enclose text without submitting each line.
const (
	PasteStart  = "\x1b[200~"
	PasteEnd    = "\x1b[201~"
	InertEscape = "<ESC>"
)

// PasteGap is the time a terminal agent gets to accept pasted text before submitting it.
func PasteGap(text string) time.Duration {
	return min(150*time.Millisecond+time.Duration(strings.Count(text, "\n"))*10*time.Millisecond, time.Second)
}
