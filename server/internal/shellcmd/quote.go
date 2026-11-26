package shellcmd

import (
	"strings"
)

// Quote makes s one word that reads back as s in the shells a node runs commands in or types them into: the sh
// family and fish. Safe characters stay bare; anything else is single-quoted, with each quote and each backslash put
// outside the quotes, backslash-escaped. Inside single quotes fish still takes \' and \\ as escapes, so a backslash
// before a quote used to keep the quotes open there and run the rest of a first message as commands (#2867); outside
// quotes every one of these shells reads \' and \\ alike.
func Quote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + quoteEscapes.Replace(s) + "'"
}

// quoteEscapes closes Quote's quotes around each quote and backslash in the text (one pass, so the
// replacements are not escaped again): anyShellQuote's scheme (devices/deploy.go) for the sh family and fish, with
// csh's `!` left as it is.
var quoteEscapes = strings.NewReplacer(`'`, `'\''`, `\`, `'\\'`)
