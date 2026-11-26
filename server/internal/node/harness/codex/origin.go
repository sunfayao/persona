package codex

import (
	"bufio"
	"cmp"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Origin is where a rollout's thread came from, from its first line (session_meta): the Codex App writes
// originator "Codex Desktop" or "codex_work_desktop" (source "vscode" for its chats, "exec" for its background runs, a thread_spawn object for
// subagents), the Codex CLI "codex-tui" / "cli", `codex exec` and the SDKs over it "codex_exec" / "codex_sdk_*" with
// source "exec" (their subagents keep the originator). Only the App's threads go through its IPC bus.
type Origin struct {
	Desktop bool
	// persona: a chat Persona started in the App (originator codexAppClient, codexapp_new.go), which the App lists
	// without a record in its state; desktop too, so its subagents are the App's as well.
	Persona bool
	CLI     bool
	// exec: a one-shot run a program started (`codex exec`, an SDK, the App's background runs) or a subagent of one:
	// nobody's conversation, not listed in Agents (programRun).
	Exec     bool
	Subagent bool
	// guardian: an approval review, a subagent of source {"subagent": {"other": "guardian"}} that Codex runs on a tool
	// call before the call runs. Its rollout holds only the review's request and assessment: not listed (#5486).
	Guardian bool
	Root     string // session_id identifies the shared root on newer Codex versions
	Parent   string // the thread that spawned this subagent
	// started holds when the thread began, the session_meta line's time (Session.StartedAt), made once for every row
	// of the thread: a hierarchy is never written once set, so the rows share it. Nil when the line has no time.
	Started *proto.SessionHierarchy
}

// SourceObject reads a thread's source when it is an object, a subagent's ({"subagent": …}): the thread that
// spawned it (thread_spawn's parent_thread_id), or an approval review ({"other": "guardian"}). A rollout's session_meta
// and the App's database row hold the same object.
func SourceObject(raw json.RawMessage) (parent string, guardian, sub bool) {
	var spawn struct {
		Subagent struct {
			ThreadSpawn struct {
				Parent string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
			Other string `json:"other"`
		} `json:"subagent"`
	}
	if json.Unmarshal(raw, &spawn) == nil {
		parent, guardian = spawn.Subagent.ThreadSpawn.Parent, spawn.Subagent.Other == "guardian"
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil {
		_, sub = fields["subagent"]
	}
	return parent, guardian, sub || parent != ""
}

// DesktopClientName is the client name Codex writes as the originator of Persona's chats.
const DesktopClientName = "persona_codex_app"

// ReadOrigin reads the first line of a rollout. An unwritten header is retried by the caller.
func ReadOrigin(path string) (Origin, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Origin{}, false
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 64<<10).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Origin{}, false
	}
	var m struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			Originator   string          `json:"originator"`
			Parent       string          `json:"parent_thread_id"`
			SessionID    string          `json:"session_id"`
			ThreadSource string          `json:"thread_source"`
			Source       json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &m) != nil || m.Type != "session_meta" {
		return Origin{}, false // not written yet: read again next time
	}
	o := Origin{Desktop: m.Payload.Originator == "Codex Desktop" || m.Payload.Originator == "codex_work_desktop" || m.Payload.Originator == DesktopClientName,
		Persona: m.Payload.Originator == DesktopClientName, CLI: m.Payload.Originator == "codex-tui" || m.Payload.Originator == "codex_cli_rs"}
	if at, err := time.Parse(time.RFC3339Nano, m.Timestamp); err == nil && !at.IsZero() {
		o.Started = &proto.SessionHierarchy{StartedAt: at.UTC()}
	}
	var source string
	if json.Unmarshal(m.Payload.Source, &source) != nil {
		source = "" // an object: a subagent
	}
	o.Exec = source == "exec" || m.Payload.Originator == "codex_exec" || strings.HasPrefix(m.Payload.Originator, "codex_sdk")
	var spawned string
	spawned, o.Guardian, o.Subagent = SourceObject(m.Payload.Source)
	o.Parent = cmp.Or(m.Payload.Parent, spawned)
	o.Subagent = o.Subagent || o.Parent != "" || source == "subagent" || m.Payload.ThreadSource == "subagent"
	if o.Subagent {
		o.Root = m.Payload.SessionID
	}
	return o, true
}
