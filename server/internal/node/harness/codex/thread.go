package codex

import (
	"encoding/json"
	"strings"
	"time"
)

// The Codex App keeps its threads in <CODEX_HOME>/state_<n>.sqlite (table threads). Since it moved their history into
// thread_history_1.sqlite (history_mode "paginated") it deletes their rollouts, so most chats in its sidebar have none and
// rollouts lists none of them (#3305: 12 of a sidebar's 21 chats were missing from Agents). codexAppThreads reads the rows of
// the chats the sidebar lists (codexAppState.sidebar): one the database has, unarchived, is listed as the sidebar lists it —
// through its rollout while the file is there, whatever its age, else from its row alone (title, directory, last update).
// An id in the sidebar's lists that the database lacks is a chat deleted in the App, which the sidebar does not show either.
//
// The database is read with the system sqlite3, read-only, as the Messages and Cindy readers are (cindybridge.go), also
// while the App has it closed (codexDBTarget). Without sqlite3, without the database, or with a schema these columns are
// not in, nothing more is listed than before.

type AppThread struct {
	ID          string `json:"id"`
	RolloutPath string `json:"rolloutPath"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	Source      string `json:"source"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
	Paginated   bool   `json:"-"`
	// Model and Effort are what the App runs the chat with (its model and reasoning_effort columns, #6501): its
	// history in the App's database says neither. Empty in a schema without them.
	Model         string `json:"model"`
	Effort        string `json:"effort"`
	ModelProvider string `json:"modelProvider"`
	// Terminal CLIs have no app-server stream: include the stored items of their running turn too.
	IncludeRunning bool
}

func (t AppThread) Updated() time.Time { return time.UnixMilli(t.UpdatedAtMs) }

// origin is where the thread came from as its database row says, for a row whose rollout is gone (codexThreadOrigin):
// its source is the rollout's session_meta source, a plain string ("vscode", "exec") or a subagent's object
// (codexSourceObject), and every row read is the App's (readCodexAppThreads). It names no root and no start time.
func (t AppThread) Origin() Origin {
	o := Origin{Desktop: true}
	source := strings.TrimSpace(t.Source)
	if !strings.HasPrefix(source, "{") {
		o.Exec, o.Subagent = source == "exec", source == "subagent"
		return o
	}
	o.Parent, o.Guardian, o.Subagent = SourceObject(json.RawMessage(source))
	return o
}
