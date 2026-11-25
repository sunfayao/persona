package lazyre

import (
	"regexp"
	"strings"
	"testing"
)


// MayMatchLowered reads an ASCII text as strings.ToLower has it, without lowering it: false exactly when the lowered
// text fails the gate, and never false for a lowered text that matches. A text with other runes is left to the match.
func TestMayMatchLoweredIsTheGateOnTheLoweredText(t *testing.T) {
	texts := []string{"", "Should I go", "YOU SHOULD I", "No Decisions For You", "xAAB", "ABC", "CAT", "Bat", "Q", "ZZQ",
		"a `B?` c", "See HTTPS://e.x/?q", "Example result.", "İ", "Ünïcode NO", "你来定 OK", "\xffNO"}
	for _, src := range []string{`\bshould i\b|你(?:来定|定一下)|no (?:questions|decisions?) for you`, "`[^`\n]*`|https?://\\S+",
		`(c|b)at`, `q|bc|你好|de|fg|hi|jk|lm|no|pr`, `Abc|xy`, `a*b`} {
		r, want := NewGated(src), regexp.MustCompile(src)
		for _, s := range texts {
			lowered := strings.ToLower(s)
			may := r.MayMatchLowered(s)
			if !may && want.MatchString(lowered) {
				t.Errorf("%s: MayMatchLowered(%q) = false, but the lowered text matches", src, s)
			}
			ascii := strings.IndexFunc(s, func(c rune) bool { return c >= 0x80 }) < 0
			if gated := r.gate != nil; ascii && gated && may != r.gate.open(lowered) || (!ascii || !gated) && !may {
				t.Errorf("%s: MayMatchLowered(%q) = %v", src, s, may)
			}
		}
	}
}
