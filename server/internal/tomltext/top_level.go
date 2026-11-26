package tomltext

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sunfayao/persona/server/internal/lazyre"
)

var (
	tomlTable = lazyre.New(`^\s*\[`)
	tomlKV    = lazyre.New(`^\s*([A-Za-z0-9_-]+)\s*=\s*(.*)$`)
)

// tomlTopLevel reads the top-level `key = value` lines (before the first table) whose value is a string or a bool.
func TopLevel(text string) map[string]any {
	out := map[string]any{}
	for _, line := range strings.Split(text, "\n") {
		if tomlTable.MatchString(line) {
			break
		}
		m := tomlKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if v, ok := Scalar(m[2]); ok {
			out[m[1]] = v
		}
	}
	return out
}

// tomlScalar parses a basic or literal string or a bool, with an optional trailing comment.
func Scalar(raw string) (any, bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, `"`):
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			c := raw[i]
			if c == '\\' && i+1 < len(raw) {
				i++
				switch raw[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(raw[i])
				}
				continue
			}
			if c == '"' {
				return b.String(), true
			}
			b.WriteByte(c)
		}
		return nil, false
	case strings.HasPrefix(raw, `'`):
		if j := strings.Index(raw[1:], `'`); j >= 0 {
			return raw[1 : 1+j], true
		}
		return nil, false
	}
	word := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
	if b, err := strconv.ParseBool(word); err == nil && (word == "true" || word == "false") {
		return b, true
	}
	return nil, false
}

func tomlValue(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case string:
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
		return `"` + r.Replace(x) + `"`
	}
	return fmt.Sprint(v)
}

// tomlSet sets (or, with nil / "", removes) one top-level key: its line is replaced in place, or a new line goes
// after the last top-level line, before the first table.
func Set(text, key string, value any) string {
	lines := strings.Split(text, "\n")
	end := len(lines) // first table header
	for i, l := range lines {
		if tomlTable.MatchString(l) {
			end = i
			break
		}
	}
	remove := value == nil || value == ""
	for i := 0; i < end; i++ {
		if m := tomlKV.FindStringSubmatch(lines[i]); m != nil && m[1] == key {
			if remove {
				return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
			}
			lines[i] = key + " = " + tomlValue(value)
			return strings.Join(lines, "\n")
		}
	}
	if remove {
		return text
	}
	at := end // after the last non-blank top-level line, so a blank line before the first table stays
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	line := key + " = " + tomlValue(value)
	if text == "" {
		return line + "\n"
	}
	out := append(append(append([]string{}, lines[:at]...), line), lines[at:]...)
	return strings.Join(out, "\n")
}

// IsTable reports whether line starts a table.
func IsTable(line string) bool { return tomlTable.MatchString(line) }

// SplitKeyValue separates a simple key from its unparsed value.
func SplitKeyValue(line string) (key, raw string, ok bool) {
	m := tomlKV.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
