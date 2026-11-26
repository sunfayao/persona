package orca

import "strings"

// orcaTypes: the key's bytes type text into the program (a printable character, alone or after alt's ESC), as
// opposed to a control or navigation key.
func orcaTypes(b string) bool {
	b = strings.TrimPrefix(b, "\x1b")
	if b == "" || strings.HasPrefix(b, "[") && len(b) > 1 || strings.HasPrefix(b, "O") && len(b) == 2 {
		return false // an escape sequence (CSI / SS3)
	}
	for _, r := range b {
		if r >= ' ' && r != 0x7f {
			return true
		}
	}
	return false
}
