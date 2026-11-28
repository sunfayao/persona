package agy

import (
	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/lazyre"
	"net/url"
	"regexp"
	"strings"
)

// Match the whitespace used by JavaScript's \s and String.trim in cleanUserText.
const agyJSWhitespace = `\x09-\x0D\x20\x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`
const agySpace = `[` + agyJSWhitespace + `]`

var agyUserRequest = lazyre.New(`(?s)<USER_REQUEST>` + agySpace + `*(.*?)` + agySpace + `*</USER_REQUEST>`)

// Go regexp has no backreferences. Use one alternative per context tag (compat.ContextNames: the current and older
// names, as the companion's SNAPSHOT_RE).
var agySnapshot = lazyre.New(`(?s)(?:` + agyContextBlocks() + `|<persona-view-errors\b[^>]*>.*?</persona-view-errors>)` + agySpace + `*`)

// Keep s/S ASCII-only as in the companion's case-insensitive regexp (Go's (?i) would also match ſ).
var agyContextEnd = lazyre.New(`^` + agySpace + `*(?i:</)(?:` + agyContextEndNames() + `)(?i:>)` + agySpace + `*`)

func agyContextBlocks() string {
	var alt []string
	for _, n := range compat.ContextNames() {
		q := regexp.QuoteMeta(n)
		alt = append(alt, `<`+q+`>.*?</`+q+`>`)
	}
	return strings.Join(alt, "|")
}

func agyContextEndNames() string {
	var alt []string
	for _, n := range compat.ContextNames() {
		var b strings.Builder
		for _, r := range n {
			switch {
			case r == 's' || r == 'S':
				b.WriteString(`[sS]`)
			case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
				b.WriteString(`(?i:` + string(r) + `)`)
			default:
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
		alt = append(alt, b.String())
	}
	return strings.Join(alt, "|")
}

// Antigravity writes embedded ACP resources into USER_INPUT.content. Require the complete
// leading Persona envelope so a quoted label or another URI scheme keeps its text.
var agyContextResource = lazyre.New(`(?s)^(?:Content of persona-context://[^\r\n]*:\r?\n<persona-context>.*?</persona-context>` + agySpace + `*)+`)

var agyTrimSpace = lazyre.New(`^` + agySpace + `+|` + agySpace + `+$`)

var agyInterruptedError = lazyre.New(`(?is)^(?:api error:\s*)?the stream was interrupted\.?(?:\s+please continue\b.*)?$`)

func isAgyInterruptedStep(step agyStep) bool {
	if step.Type != "ERROR_MESSAGE" {
		return false
	}
	text := step.Error
	if text == "" {
		text = step.Content
	}
	return agyInterruptedError.MatchString(strings.TrimSpace(text))
}

func UserText(text string) string {
	if m := agyUserRequest.FindStringSubmatch(text); m != nil {
		text = m[1]
	}
	for {
		clean := agyContextEnd.ReplaceAllString(agySnapshot.ReplaceAllString(agyContextResource.ReplaceAllString(text, ""), ""), "")
		if clean == text {
			return agyTrimSpace.ReplaceAllString(text, "")
		}
		text = clean
	}
}

var (
	agyFenceRe    = lazyre.New(`^((?:[ \t]*>[ \t]?)*)[ \t]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+)?[ \t]*(` + "`{3,}" + `|~{3,})(.*)$`)
	agyListItemRe = lazyre.New(`^(?:[ \t]*>)*[ \t]*(?:[-*+]|\d{1,9}[.)])[ \t]`)
	agyQuotePreRe = lazyre.New(`^(?:[ \t]*>[ \t]?)+`)
	agyRefDefRe   = lazyre.New(`^([ ]{0,3}\[[^\]\n]+\]:[ \t]+)(<([^>\r\n]+)>|(\S+))(.*)$`)
	agyLinkTailRe = lazyre.New(`^[ \t]*(?:(?:"(?:\\[\s\S]|[^"\\])*"|'(?:\\[\s\S]|[^'\\])*'|\((?:\\[\s\S]|[^)\\])*\))[ \t]*)?\)`)
	agyWinDriveRe = lazyre.New(`^/([A-Za-z])(?::|%3[Aa])/(.+)$`)
)

func agyLocalFileURLToPath(dest string) string {
	if len(dest) <= 8 || !strings.EqualFold(dest[:7], "file://") {
		return ""
	}
	for i := 0; i < len(dest); i++ {
		if dest[i] < 0x20 || dest[i] == 0x7f {
			return ""
		}
	}
	if strings.Contains(strings.ToLower(dest), "%00") {
		return ""
	}
	raw, frag := dest[7:], ""
	if hash := strings.IndexByte(raw, '#'); hash >= 0 {
		raw, frag = raw[:hash], raw[hash:]
	}
	if strings.ContainsRune(raw, '?') {
		return ""
	}
	var p string
	switch {
	case strings.HasPrefix(raw, "/"):
		p = raw
	case len(raw) >= 10 && strings.EqualFold(raw[:10], "localhost/"):
		p = raw[9:]
	default:
		return ""
	}
	if p == "/" || strings.HasPrefix(p, "//") {
		return ""
	}
	decoded := p
	if u, err := url.PathUnescape(p); err == nil {
		decoded = u
	}
	lower := strings.ToLower(decoded)
	if lower == "/api/v1" || strings.HasPrefix(lower, "/api/v1/") {
		return ""
	}
	if m := agyWinDriveRe.FindStringSubmatch(p); m != nil {
		return m[1] + ":/" + m[2] + frag
	}
	return p + frag
}

func agyUnescapedBracket(line string, idx int) bool {
	slashes := 0
	for i := idx - 1; i >= 0 && line[i] == '\\'; i-- {
		slashes++
	}
	return slashes%2 == 0
}

func agyRewriteFileLinksLine(line string) string {
	if !strings.Contains(strings.ToLower(line), "file://") {
		return line
	}
	if def := agyRefDefRe.FindStringSubmatch(line); def != nil {
		dest, angled := def[4], false
		if def[3] != "" {
			dest, angled = def[3], true
		}
		if next := agyLocalFileURLToPath(dest); next != "" {
			if angled {
				return def[1] + "<" + next + ">" + def[5]
			}
			return def[1] + next + def[5]
		}
	}
	var out strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '`' {
			n := 0
			for i+n < len(line) && line[i+n] == '`' {
				n++
			}
			j, closed := i+n, -1
			for j < len(line) {
				if line[j] == '`' {
					m := 0
					for j+m < len(line) && line[j+m] == '`' {
						m++
					}
					if m == n {
						closed = j + m
						break
					}
					j += m
				} else {
					j++
				}
			}
			end := i + n
			if closed >= 0 {
				end = closed
			}
			out.WriteString(line[i:end])
			i = end
			continue
		}
		if line[i] == ']' && i+1 < len(line) && line[i+1] == '(' && agyUnescapedBracket(line, i) {
			j := i + 2
			for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
				j++
			}
			if j < len(line) && line[j] == '<' {
				k := j + 1
				for k < len(line) && line[k] != '>' && line[k] != '<' {
					if line[k] == '\\' && k+1 < len(line) {
						k += 2
					} else {
						k++
					}
				}
				if k < len(line) && line[k] == '>' && agyLinkTailRe.MatchString(line[k+1:]) {
					if next := agyLocalFileURLToPath(line[j+1 : k]); next != "" {
						out.WriteString(line[i:j])
						out.WriteByte('<')
						out.WriteString(next)
						out.WriteByte('>')
						i = k + 1
						continue
					}
				}
			} else {
				k, depth := j, 0
				for k < len(line) {
					ch := line[k]
					if ch == '\\' && k+1 < len(line) {
						k += 2
						continue
					}
					if ch == ' ' || ch == '\t' || ch == '\r' {
						break
					}
					if ch == '(' {
						depth++
						k++
					} else if ch == ')' {
						if depth == 0 {
							break
						}
						depth--
						k++
					} else {
						k++
					}
				}
				if k > j && depth == 0 && agyLinkTailRe.MatchString(line[k:]) {
					if next := agyLocalFileURLToPath(line[j:k]); next != "" {
						out.WriteString(line[i:j])
						out.WriteString(next)
						i = k
						continue
					}
				}
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

// agyAssistantText mirrors cleanAssistantText in integrations/antigravity/lib/file-links.js:
// Antigravity assistant replies emit local file:// links and images, which Persona clients
// exclude at the generic Markdown boundary (#12102, #12296). Normalize local file:// links
// outside code blocks and inline code spans into plain machine paths.
func agyAssistantText(raw string) string {
	if !strings.Contains(strings.ToLower(raw), "file://") {
		return raw
	}
	lines := strings.Split(raw, "\n")
	var fenceCh byte
	fenceLen := 0
	blank, inList, inIndented := true, false, false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indented := strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
		if fenceLen > 0 {
			rest := strings.TrimSpace(agyQuotePreRe.ReplaceAllString(line, ""))
			if len(rest) >= fenceLen && rest[0] == fenceCh && strings.Trim(rest, string(fenceCh)) == "" {
				fenceLen = 0
			}
			blank = false
			continue
		}
		if inIndented {
			if trimmed == "" || indented {
				continue
			}
			inIndented = false
		}
		if trimmed != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inList = agyListItemRe.MatchString(line) || (inList && !blank)
		}
		if open := agyFenceRe.FindStringSubmatch(line); open != nil && !(open[2][0] == '`' && strings.ContainsRune(open[3], '`')) {
			fenceCh, fenceLen = open[2][0], len(open[2])
			blank = false
			continue
		}
		if blank && !inList && trimmed != "" && indented {
			inIndented = true
			continue
		}
		blank = trimmed == ""
		lines[i] = agyRewriteFileLinksLine(line)
	}
	return strings.Join(lines, "\n")
}
