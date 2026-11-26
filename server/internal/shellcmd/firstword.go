package shellcmd

import (
	"regexp"
	"strings"
)

// FirstWord is the program a command line written with Quote runs: its first word past the VAR=value ones
// in front (rootAssign). A word is bare, or single-quoted with each embedded quote or backslash closed,
// backslash-escaped and reopened. Other tools' hooks in the user's settings.json (hookProgram) may double-quote a
// word instead: read the same way, nothing expanded inside. Quote never writes a bare ", so its lines read as before.
func FirstWord(cmd string) string {
	for rest := strings.TrimSpace(cmd); rest != ""; {
		var w strings.Builder
		i := 0
		for i < len(rest) && rest[i] != ' ' && rest[i] != '\t' {
			switch q := rest[i]; {
			case q == '\'' || q == '"':
				j := strings.IndexByte(rest[i+1:], q)
				if j < 0 {
					return "" // not a command line Quote writes
				}
				w.WriteString(rest[i+1 : i+1+j])
				i += j + 2
			case rest[i] == '\\' && i+1 < len(rest):
				w.WriteByte(rest[i+1])
				i += 2
			default:
				w.WriteByte(rest[i])
				i++
			}
		}
		if matched, _ := regexp.MatchString(assignmentPattern, rest[:i]); !matched {
			return w.String()
		}
		rest = strings.TrimLeft(rest[i:], " \t")
	}
	return ""
}

// assignmentRe is a shell word that sets a variable for the command after it.
const assignmentPattern = `^[A-Za-z_][A-Za-z0-9_]*=`
