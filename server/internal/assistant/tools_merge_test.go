package assistant

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/tooluse"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A merged tool's call runs the older tool its action or parameters name, with the rest of its arguments; a shape
// that could mean two of them is refused rather than guessed (a preview with one agent's session_id would otherwise
// switch it at once).
func TestUnmergeTool(t *testing.T) {
	for _, tt := range []struct {
		name, args     string
		want, wantArgs string // wantArgs "": the arguments as given
		err            string
	}{
		{"view", `{"action":"show","view":{"a":1}}`, "view_show", `{"view":{"a":1}}`, ""},
		{"view", `{"action":"get","id":"v1"}`, "view_get", `{"id":"v1"}`, ""},
		{"view", `{"action":"delete","id":"v1"}`, "", "", "action must be one of get, show, update"},
		{"view", `{}`, "", "", "action must be one of"},
		{"view", `[1]`, "", "", "invalid arguments"},
		{"skill", `{"action":"save","name":"x","description":"d","body":"b"}`, "skill_save", `{"name":"x","description":"d","body":"b"}`, ""},
		{"model_profiles", `{"action":"list"}`, "model_profiles_list", `{}`, ""},
		{"repositories", `{"action":"add","url":"https://example.com/r"}`, "repository_add", `{"url":"https://example.com/r"}`, ""},
		{"todo_links", `{"action":"add_links","id":"t1","urls":["u"]}`, "todo_repository_links_add", `{"id":"t1","urls":["u"]}`, ""},
		{"task_start_repair", `{"action":"retry","todo_id":"t1","reason":"r"}`, "task_retry", `{"todo_id":"t1","reason":"r"}`, ""},
		{"agent_switch", `{"machine":"m","session_id":"s","launcher":"l"}`, "agent_switch", "", ""},
		{"agent_switch", `{"launcher":"l","from_launcher":"claude","preview":true}`, "agents_switch", "", ""},
		{"agent_switch", `{"machine":"m","session_id":"s","launcher":"l","preview":true}`, "", "", "preview is for many agents"},
		{"agent_switch", `{"session_id":"s","launcher":"l","agents":[{"session_id":"t"}]}`, "", "", "agents is for many agents"},
		{"agent_switch", `{"launcher":"l","text":"go on"}`, "", "", "text goes to one agent"},
		{"agent_archive", `{"machine":"m","session_id":"s","restore":true}`, "agent_unarchive", `{"machine":"m","session_id":"s"}`, ""},
		{"agent_archive", `{"machine":"m","session_id":"s","restore":false}`, "agent_archive", `{"machine":"m","session_id":"s"}`, ""},
		{"agent_archive", `{"machine":"m","session_id":"s","close":true}`, "agent_archive", "", ""},
		{"agent_archive", `{"machine":"m","session_id":"s","restore":true,"force":true}`, "", "", "force does not go with it"},
		{"todo_update", `{"id":"t1","title":"x"}`, "todo_update", "", ""},
		{"todo_update", `{"id":"t1","state":"done"}`, "todo_complete", `{"id":"t1"}`, ""},
		{"todo_update", `{"id":"t1","state":"done","report":"r"}`, "concern_report", `{"id":"t1","report":"r"}`, ""},
		{"todo_update", `{"id":"t1","state":"open"}`, "todo_reopen", `{"id":"t1"}`, ""},
		{"todo_update", `{"id":"t1","state":"deleted"}`, "todo_delete", `{"id":"t1"}`, ""},
		{"todo_update", `{"id":"t1","state":"done","title":"x"}`, "", "", "a state change goes alone"},
		{"todo_update", `{"id":"t1","state":"open","report":"r"}`, "", "", "report goes with state done"},
		{"todo_update", `{"id":"t1","report":"r"}`, "", "", "report goes with state done"},
		{"todo_update", `{"id":"t1","state":"archived"}`, "", "", "state must be done, open or deleted"},
		{"todo_complete", `{"id":"t1"}`, "todo_complete", "", ""}, // an older name runs as it is
		{"agent_read", `{"x":1}`, "agent_read", "", ""},
	} {
		name, args, err := unmergeTool(tt.name, json.RawMessage(tt.args))
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s %s: error %v, want %q", tt.name, tt.args, err, tt.err)
			}
			continue
		}
		if err != nil || name != tt.want {
			t.Errorf("%s %s: %s, %v; want %s", tt.name, tt.args, name, err, tt.want)
			continue
		}
		want := tt.wantArgs
		if want == "" {
			want = tt.args
		}
		var got, exp any
		_ = json.Unmarshal(args, &got)
		_ = json.Unmarshal([]byte(want), &exp)
		if !reflect.DeepEqual(got, exp) {
			t.Errorf("%s %s: arguments %s, want %s", tt.name, tt.args, args, want)
		}
	}
}

// Every older name a merge stands for still answers, unlisted, for a conversation that learned it before; every merged
// tool is listed with exactly its actions; and a merged tool counts its older tools' calls where calls rank tools.
func TestMergedToolsKeepTheirOlderNames(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	for old, merged := range mergedInto {
		if !slices.Contains(retiredToolNames, old) || slices.Contains(toolNames, old) {
			t.Errorf("%s: not kept unlisted (retiredToolNames)", old)
		}
		if !s.isOwnTool(ctx, old) {
			t.Errorf("%s no longer routes", old)
		}
		if !slices.Contains(toolNames, merged) || mergedName(old) != merged {
			t.Errorf("%s: merged into %s, which is not listed", old, merged)
		}
	}
	for merged, actions := range mergedTools {
		var schema struct {
			Required   []string `json:"required"`
			Properties struct {
				Action struct {
					Enum []string `json:"enum"`
				} `json:"action"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(prompts.Tool(merged).InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(schema.Required, "action") || !slices.Equal(slices.Sorted(slices.Values(schema.Properties.Action.Enum)), slices.Sorted(maps.Keys(actions))) {
			t.Errorf("%s: actions %v, want %v", merged, schema.Properties.Action.Enum, slices.Sorted(maps.Keys(actions)))
		}
	}
	book := tooluse.New(tooluse.Host{Canonical: mergedName})
	book.Record(tooluse.Call{Tool: "task_retry", Runtime: "claude", OK: true})
	book.Record(tooluse.Call{Tool: "task_start_repair", Runtime: "claude", OK: true})
	if got := book.Usage("claude", time.Now().Add(-time.Hour))["task_start_repair"].Calls; got != 2 {
		t.Errorf("task_start_repair ranks with %d calls, want task_retry's and its own", got)
	}
}

// A clean round keeps the tools its grant allows directly available, a merged one for its safe actions, and its
// transcript check judges each call by the older tool it ran.
func TestGrantSafeMergedCalls(t *testing.T) {
	if !grantSafeListed("repositories") || !grantSafeListed("todo_links") || grantSafeListed("view") {
		t.Error("clean-round availability of the merged tools")
	}
	for input, safe := range map[string]bool{
		`{"action":"list","query":"app"}`: true, `{"action":"get","id":"r1"}`: true,
		`{"action":"add","url":"https://example.com/r"}`: false, `{}`: false,
	} {
		if got := grantSafeTool("mcp__persona__repositories", json.RawMessage(input)); got != safe {
			t.Errorf("repositories %s: grant-safe %v, want %v", input, got, safe)
		}
	}
	if !grantSafeTool("mcp__persona__repositories_list", nil) || grantSafeTool("mcp__persona__todo_update", json.RawMessage(`{"id":"t","state":"done"}`)) {
		t.Error("older and other tools")
	}
}

// Through the conversation's entry point: todo_update with a state is todo_complete, with todo_complete's card.
func TestTodoUpdateStateRunsTheOlderTool(t *testing.T) {
	e := newTodoEnv(t, nil)
	putTodo(e.s, Todo{ID: "todo_memo", Title: "Memo", Origin: "conv1", Supervision: &Supervision{Supervisor: "conv1"}})
	shown := make(chan proto.Interaction, 1)
	go answerArchiveCard(t, e.s, true, shown)
	res, err := e.tool("todo_update", map[string]any{"id": "todo_memo", "state": "done"})
	if err != nil || !strings.HasPrefix(res, "Completed: ") {
		t.Fatalf("todo_update state done: %s %v", res, err)
	}
	if card := <-shown; card.ToolName != "todo_complete" {
		t.Fatalf("card: %+v", card)
	}
	if got := todoNow(e.s, "todo_memo"); got.DoneAt == nil {
		t.Fatal("not done")
	}
	if _, err := e.tool("todo_update", map[string]any{"id": "todo_memo", "state": "done", "title": "x"}); err == nil {
		t.Fatal("a state change with other fields ran")
	}
}
