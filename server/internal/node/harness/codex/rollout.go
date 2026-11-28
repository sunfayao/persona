package codex

import (
	"bufio"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/sunfayao/persona/server/internal/transcript"
)

// Codex support: herdr reports Codex panes as agent "codex"; their conversations are rollouts under
// ~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session id>.jsonl (parsed by transcript/codex.go). Compatible Codex
// variants use the same layout in their own data directories. herdr's Codex integration reports the session id when
// Codex starts; a Codex started before that integration has none. Codex puts
// the thread's name in the terminal title ("<name> | <directory>"). Until a herdr pane identifies its thread, it
// has its own waiting row (codex_waiting.go). Its working directory can contain rollouts from many earlier panes.
//
// Subagents write rollouts of their own next to the conversation, in the same directory: Codex's thread_spawn helpers
// and the compatible CLI's guardian, which reviews each tool call before it runs. A pane never shows one: they are skipped when
// guessing, and a subagent's id maps to the conversation it belongs to (rolloutMeta).

// rolloutMeta is what a rollout's first line (session_meta) says about its thread.
type RolloutMeta struct {
	ThreadID string
	Cwd      string
	Subagent bool   // a subagent: thread_source "subagent", or a source of {"subagent": …}
	Root     string // the conversation a subagent belongs to (session_id, else parent_thread_id); "" when not given
}

var (
	rolloutExts = []string{".jsonl", ".jsonl.zst"}
	// filepathGlob is what GlobRolloutPaths uses for its internal globs; tests patch it.
	filepathGlob = filepath.Glob
)

// stripRolloutSuffix removes one of the rollout extensions from path's base, returning the stem and whether one was present.
func stripRolloutSuffix(name string) (string, bool) {
	for _, ext := range rolloutExts {
		if strings.HasSuffix(name, ext) {
			return name[:len(name)-len(ext)], true
		}
	}
	return name, false
}

// rolloutID is a rollout filename's thread UUID, before any _<segment UUID> suffix, or empty when absent:
// what rolloutIDRe captures, without a regular expression. rollouts reads it for every rollout on disk once a minute,
// where rolloutIDRe took most of each refresh's CPU time.
func RolloutID(path string) string {
	base := filepath.Base(path)
	stem, ok := stripRolloutSuffix(base)
	if !ok {
		return ""
	}
	const idLen = 36
	if len(stem) < idLen {
		return ""
	}
	end := len(stem)
	start := end - idLen
	if !IsUUID(stem[start:end]) {
		return ""
	}
	for start >= idLen+1 && stem[start-1] == '_' && IsUUID(stem[start-idLen-1:start-1]) {
		end, start = start-1, start-idLen-1
	}
	return stem[start:end]
}

func IsUUID(id string) bool {
	const idLen = 36
	if len(id) != idLen {
		return false
	}
	for i := range idLen {
		c := id[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// GlobRolloutPaths returns both plain and compressed rollouts matching pattern, where pattern ends with ".jsonl".
func GlobRolloutPaths(pattern string) []string {
	var out []string
	if strings.HasSuffix(pattern, ".jsonl") {
		for _, ext := range rolloutExts {
			p := pattern[:len(pattern)-len(".jsonl")] + ext
			found, _ := filepathGlob(p)
			out = append(out, found...)
		}
		return out
	}
	found, _ := filepathGlob(pattern)
	return found
}

// AllRolloutPaths is every rollout file of a home on disk, whenever it was written, archived ones too: what a token
// usage backfill reaching further back than rollouts' two weeks reads.
func AllRolloutPaths(home string) []string {
	var paths []string
	for _, ext := range rolloutExts {
		for _, sub := range []string{"sessions", "archived_sessions"} {
			var glob string
			if sub == "sessions" {
				glob = filepath.Join(home, sub, "*", "*", "*", "rollout-*"+ext)
			} else {
				glob = filepath.Join(home, sub, "rollout-*"+ext)
			}
			found, _ := filepath.Glob(glob)
			paths = append(paths, found...)
		}
	}
	return paths
}

// ReadRolloutMeta reads a rollout's first line (session_meta, tens of KB of instructions); whole says that line was all
// written, and only then is what it says worth keeping. Codex 0.167 creates the file empty and writes the line 8–19 ms
// later: a meta read in between and kept left the thread without a directory for good, and a start in that directory
// never found its thread (#2351).
func ReadRolloutMeta(path string) (m RolloutMeta, whole bool) {
	f, err := transcript.OpenStream(path)
	if err != nil {
		return RolloutMeta{}, false
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256<<10)
	line, err := r.ReadBytes('\n')
	whole = err == nil
	var l struct {
		Type    string `json:"type"`
		Payload struct {
			ID           string          `json:"id"`
			Cwd          string          `json:"cwd"`
			SessionID    string          `json:"session_id"`
			ParentThread string          `json:"parent_thread_id"`
			ThreadSource string          `json:"thread_source"`
			Source       json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "session_meta" {
		return RolloutMeta{}, whole
	}
	p := l.Payload
	m = RolloutMeta{Cwd: p.Cwd}
	if IsUUID(p.ID) {
		m.ThreadID = p.ID
	}
	var source map[string]json.RawMessage
	m.Subagent = p.ThreadSource == "subagent" || (json.Unmarshal(p.Source, &source) == nil && source["subagent"] != nil)
	if m.Subagent {
		for _, id := range []string{p.SessionID, p.ParentThread} {
			if id != "" && id != p.ID {
				m.Root = id
				break
			}
		}
	}
	return m, whole
}
