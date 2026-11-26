package nodekit

import (
	"github.com/nwverse/persona/server/internal/textfmt"
)

// FirstLine is the first line of s, cut to 200 bytes on a whole character.
func FirstLine(s string) string { return textfmt.CompactFirstLine(s) }
