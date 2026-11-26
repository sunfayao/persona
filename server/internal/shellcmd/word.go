package shellcmd

import (
	"strings"
)

// A Codex App chat whose rollout is gone (codex_appdb.go) keeps its history in <CODEX_HOME>/thread_history_<n>.sqlite:
// thread_turns, and thread_items holding the app-server's ThreadItems (v2: userMessage, agentMessage, commandExecution…).
// Its transcript is that history written back as rollout lines — session_meta, then per finished turn task_started, an
// item_completed per item, and task_complete (with its error when it failed) or turn_aborted — so the transcript reader,
// the clients and every check on a rollout read it as any other (#3305). The file is
// <state dir>/codex-app-history/<home hash>-<thread>.jsonl. It grows as a rollout does: the turns finished since it was
// last written are appended (the last turn while it still runs is left for later; one that never finished but has turns
// after it is written as interrupted), so a reader goes on where it was. <file>.mark records what was written; a file
// unlike its mark, or whose last turn is no longer in the database as it was, is written again from the start (and
// removed when no turn is left finished). It is brought up to date when the transcript is read (file) and, while the chat
// is watched, about once a second (pumpWatch), never by the list; one writer at a time, across processes too
// (<file>.lock: the others wait for its write). The database is read while the App has it closed too (codexDBTarget).
// Without sqlite3 or the history database there is none, as before.

// shellWord unquotes s as one POSIX shell word: '…' as it is, "…" with \\ \" \$ \` and a backslashed newline, a
// backslash outside quotes escaping the next character. false when s is not one whole word.
func Word(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "", false
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case '"':
			for i++; ; i++ {
				if i >= len(s) {
					return "", false
				}
				if s[i] == '"' {
					break
				}
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\\\"$`\n", s[i+1]) >= 0 {
					i++
					if s[i] == '\n' {
						continue
					}
				}
				b.WriteByte(s[i])
			}
		case '\\':
			if i+1 >= len(s) {
				return "", false
			}
			i++
			b.WriteByte(s[i])
		case ' ', '\t', '\n':
			return "", false // a second word
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), true
}
