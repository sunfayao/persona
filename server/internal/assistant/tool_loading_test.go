package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The tools that load up front, pinned with their order (their rank on Antigravity): adding, dropping or moving one is
// a reviewed change (#7317). Each is still a tool here under that name (a rename would silently defer it), but for the
// Lark extension's lark_*, which are not in this repository.
func TestCoreToolsPinned(t *testing.T) {
	want := []string{
		"agent_read", "agents_list", "todo_get", "persona_settings", "task_review", "supervision_finish", "task_plan",
		"agent_send", "machines_list", "schedule_state_set", "agent_archive", "task_scope", "todo_list", "todo_update",
		"todo_create", "computer_run", "computer_state", "machine_exec",
		"ask_user", "browser_look", "browser_run", "conversation_read", "conversation_send", "conversations_list",
		"lark_chats", "lark_messages", "lark_search", "larkcli_chats", "larkcli_messages", "larkcli_search", "notify_user",
		"schedule_followup",
	}
	if !slices.Equal(coreTools, want) {
		t.Fatalf("core tools changed:\n got %q\nwant %q", coreTools, want)
	}
	for _, name := range coreTools {
		if strings.HasPrefix(name, "lark_") {
			continue
		}
		func() {
			defer func() {
				if recover() != nil {
					t.Errorf("core tool %s is no MCP tool here: renamed or removed?", name)
				}
			}()
			prompts.Tool(name)
		}()
	}
}

// deferringExt is guestExt with every tool arriving deferred, ranked and marked to fill, as a connector program
// sending those fields would have it: the manager's rule alone decides.
type deferringExt struct{ *guestExt }

func (deferringExt) AgentTools(context.Context, bool) (agents, assistantOnly []proto.Tool) {
	return []proto.Tool{{Name: "conn_read", DeferLoading: true, CoreRank: 1, Fill: true}},
		[]proto.Tool{{Name: "computer_run", DeferLoading: true}, {Name: "lark_chats", DeferLoading: true}, {Name: "lark_agent_reply", DeferLoading: true}}
}

// An assistant conversation's tool list has every tool but the core ones deferred; a guest's (no ToolSearch, whether
// the node or the manager says so), an agent's and every one while assistant.load_all_tools is on have none deferred,
// whatever the tools arrived with. An extension's tool is core by its name alone (computer_run, lark_chats).
func TestToolLoadingMarksAssistantTools(t *testing.T) {
	ft := newFleetTest(t)
	ft.f.hello.Assistant.MachineID = "mac"
	s := ft.s
	s.SetExtensions(deferringExt{&guestExt{}}) // lark_chats and computer_run (core), conn_read and lark_agent_reply (not)
	ctx := context.Background()
	if err := s.guestSet(ctx, "mac", "g1", []string{"lark_agent_reply", "lark_chats"}); err != nil {
		t.Fatal(err)
	}
	got := mcpToolList(t, s, "o1", true, false)
	for name, deferred := range map[string]bool{"agent_read": false, "ask_user": false, "lark_chats": false, "task_plan": false,
		"computer_run": false, "secret_run": true, "view": true, "task_plan_withdraw": true, "lark_agent_reply": true,
		"conn_read": true} {
		if tool, ok := got[name]; !ok || tool.DeferLoading != deferred {
			t.Errorf("%s: listed %v, deferred %v; want deferred %v", name, ok, tool.DeferLoading, deferred)
		}
	}
	for name, tool := range got {
		if tool.DeferLoading == slices.Contains(coreTools, name) {
			t.Errorf("%s: deferred %v", name, tool.DeferLoading)
		}
	}
	none := func(what string, got map[string]proto.Tool) {
		t.Helper()
		if len(got) == 0 {
			t.Fatalf("%s: no tools", what)
		}
		for name, tool := range got {
			if tool.DeferLoading {
				t.Errorf("%s: %s deferred", what, name)
			}
		}
	}
	none("a guest", mcpToolList(t, s, "g1", true, true))
	none("a guest the node did not name", mcpToolList(t, s, "g1", true, false))
	none("an agent", mcpToolList(t, s, "a1", false, false))
	setSettings(t, s, func(v *Settings) { on := true; v.LoadAllTools = &on })
	none("load_all_tools on", mcpToolList(t, s, "o1", true, false))
}

// mcpToolList is a conversation's tool list as the node gets it (JSON), by name.
func mcpToolList(t *testing.T, s *Service, session string, assistant, guest bool) map[string]proto.Tool {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"assistant": assistant, "session_id": session, "guest": guest})
	out, handled, err := s.NodeRequest(context.Background(), "mac", "mcp.tools", raw)
	if err != nil || !handled {
		t.Fatal(err, handled)
	}
	b, _ := json.Marshal(out)
	var res struct {
		Tools []proto.Tool `json:"tools"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	got := map[string]proto.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	return got
}

// Antigravity has no ToolSearch: its proxy declares an assistant conversation's core tools by their rank, the most
// called first, whatever assistant.load_all_tools says, and with that setting on fills its budget with the tools
// marked to fill, every other one. A guest's and an agent's lists carry neither mark, and a mark a tool arrived with
// (a connector program's) is the manager's to set.
func TestToolLoadingRanksCoreToolsForRuntimesWithoutToolSearch(t *testing.T) {
	ft := newFleetTest(t)
	ft.f.hello.Assistant.MachineID = "mac"
	s := ft.s
	s.SetExtensions(deferringExt{&guestExt{}})
	ctx := context.Background()
	if err := s.guestSet(ctx, "mac", "g1", []string{"lark_agent_reply", "lark_chats"}); err != nil {
		t.Fatal(err)
	}
	check := func(what string, got map[string]proto.Tool, ranked, fill bool) {
		t.Helper()
		if len(got) == 0 {
			t.Fatalf("%s: no tools", what)
		}
		for name, tool := range got {
			rank := 0
			if ranked {
				rank = slices.Index(coreTools, name) + 1
			}
			if tool.CoreRank != rank || tool.Fill != (fill && rank == 0) {
				t.Errorf("%s: %s ranked %d, fill %v; want %d, %v", what, name, tool.CoreRank, tool.Fill, rank, fill && rank == 0)
			}
		}
	}
	listed := mcpToolList(t, s, "o1", true, false)
	if listed["agent_read"].CoreRank != 1 || listed["computer_run"].CoreRank == 0 || listed["conn_read"].CoreRank != 0 {
		t.Fatalf("ranks: agent_read %d, computer_run %d, conn_read %d", listed["agent_read"].CoreRank,
			listed["computer_run"].CoreRank, listed["conn_read"].CoreRank)
	}
	check("load_all_tools off", listed, true, false)
	check("a guest", mcpToolList(t, s, "g1", true, true), false, false)
	check("an agent", mcpToolList(t, s, "a1", false, false), false, false)
	setSettings(t, s, func(v *Settings) { on := true; v.LoadAllTools = &on })
	check("load_all_tools on", mcpToolList(t, s, "o1", true, false), true, true)
}

// assistant.load_all_tools through persona_settings: off by default, a change asks on the phone with its own card, and
// the approved value is what the tool list reads.
func TestLoadAllToolsSetting(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, newFakeHub().withMachine())
	tool := func(args string) (string, bool) {
		r := s.Tool(ctx, ToolCall{Name: "persona_settings", Arguments: json.RawMessage(args), MachineID: "server", SessionID: "c1"})
		return r.Content[0].Text, r.IsError
	}
	get := func() string { out, _ := tool(`{"action":"get","key":"assistant.load_all_tools"}`); return out }
	deferred := func() bool {
		return s.withToolLoading(ctx, "", []proto.Tool{{Name: "inbox_move"}}, true)[0].DeferLoading
	}
	if get() != "false" || !deferred() {
		t.Fatalf("default: %s, deferred %v", get(), deferred())
	}
	if out, isErr := tool(`{"action":"set","key":"assistant.load_all_tools","value":false}`); isErr || !strings.Contains(out, "is unchanged") {
		t.Fatalf("set to what it is: %s", out)
	}
	word := map[bool]string{true: i18n.T(i18n.Default, "assistant-settings_tool.on"), false: i18n.T(i18n.Default, "assistant-settings_tool.off")}
	card := i18n.T(i18n.Default, "assistant-settings_tool.change_load_all_tools", i18n.Args{"before": word[false], "after": word[true]})
	go func() {
		q, ok := awaitCard(s)
		if !ok || q.Summary != card {
			t.Errorf("card %q (%v), want %q", q.Summary, ok, card)
		}
		allow := true
		raw, _ := json.Marshal(proto.Resolution{Allow: &allow})
		if _, err := s.Action(ctx, Action{Op: "answer", InteractionID: q.ID, Answer: raw}); err != nil {
			t.Error(err)
		}
	}()
	if out, _ := tool(`{"action":"set","key":"assistant.load_all_tools","value":true}`); !strings.HasPrefix(out, "assistant.load_all_tools is true") {
		t.Fatalf("approved: %s", out)
	}
	if get() != fmt.Sprint(true) || deferred() {
		t.Fatalf("after the change: %s, deferred %v", get(), deferred())
	}
}

// Changing assistant.load_all_tools reaches a running conversation at once: the watch its personad mcp keeps (the
// revision it last listed with) is answered with the new list, without a task-link change or a process restart.
func TestLoadAllToolsReachesRunningConversations(t *testing.T) {
	ft := newFleetTest(t)
	ft.f.hello.Assistant.MachineID = "mac"
	s := ft.s
	ctx := context.Background()
	// poll is one call of the watch as personad mcp makes it: how many tools are listed and how many of them deferred,
	// and the revision to watch from next; listed is 0 when the call ran out of time unchanged.
	poll := func(revision string) (listed, deferred int, next string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		raw, _ := json.Marshal(map[string]any{"assistant": true, "session_id": "o1", "revision": revision})
		out, handled, err := s.NodeRequest(ctx, "mac", "mcp.tools", raw)
		if err != nil || !handled {
			t.Fatal(err, handled)
		}
		b, _ := json.Marshal(out)
		var res struct {
			Tools    []proto.Tool `json:"tools"`
			Revision string       `json:"revision"`
		}
		if err := json.Unmarshal(b, &res); err != nil {
			t.Fatal(err)
		}
		for _, tool := range res.Tools {
			if tool.DeferLoading {
				deferred++
			}
		}
		return len(res.Tools), deferred, res.Revision
	}
	set := func(on bool) {
		t.Helper()
		change, err := s.setLoadAllTools(ctx, json.RawMessage(fmt.Sprint(on)))
		if err != nil || change.apply == nil {
			t.Fatalf("set %v: %v, %+v", on, err, change)
		}
		if _, err := change.apply(ctx); err != nil {
			t.Fatal(err)
		}
	}
	listed, deferred, rev := poll("")
	if listed == 0 || deferred == 0 {
		t.Fatalf("first list: %d tools, %d deferred", listed, deferred)
	}
	set(true)
	if listed, deferred, rev = poll(rev); listed == 0 || deferred != 0 {
		t.Fatalf("load_all_tools on: %d tools, %d deferred (no tools: the running conversation was not told)", listed, deferred)
	}
	set(false)
	if listed, deferred, _ = poll(rev); listed == 0 || deferred == 0 {
		t.Fatalf("load_all_tools off: %d tools, %d deferred (no tools: the running conversation was not told)", listed, deferred)
	}
}

// withToolLoading copies: the list it was given, which may be an extension's cached one, keeps its flags.
func TestToolLoadingCopies(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	in := []proto.Tool{{Name: "ask_user", DeferLoading: true}, {Name: "inbox_move"}}
	out := s.withToolLoading(context.Background(), "", in, true)
	if out[0].DeferLoading || !out[1].DeferLoading {
		t.Fatalf("marked %+v", out)
	}
	if !in[0].DeferLoading || in[1].DeferLoading {
		t.Fatalf("the given list changed: %+v", in)
	}
}

func TestWorkGuideLoadingScopeAndScheduledGrant(t *testing.T) {
	s := newFleetTest(t).s
	set := func(on, all bool) { setSettings(t, s, func(v *Settings) { v.WorkGuide = &on; v.LoadAllTools = &all }) }
	ctx := t.Context()
	tools := []proto.Tool{{Name: "todo_get"}, {Name: "task_plan"}, {Name: "todo_extension"}, {Name: "ask_user"}}
	set(false, false)
	baseline := s.withToolLoading(ctx, "ordinary", tools, true)
	s.noteCleanRound("scheduled", "task-1")
	set(true, false)
	for _, tc := range []struct {
		name                  string
		ctx                   context.Context
		self                  string
		assistant, todo, plan bool
	}{
		{"assistant", ctx, "ordinary", true, true, true},
		{"agent or guest", ctx, "ordinary", false, false, false},
		{"background supervision", withToolSet(ctx, json.RawMessage(`{"tool_set":"supervise"}`)), "ordinary", true, false, false},
		{"scheduled first plan", ctx, "scheduled", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := s.withToolLoading(tc.ctx, tc.self, tools, tc.assistant)
			if got[0].WorkGuideTool != tc.todo || got[1].WorkGuideTool != tc.plan || got[2].WorkGuideTool || got[3].WorkGuideTool {
				t.Fatal(got)
			}
		})
	}
	set(true, true)
	if !s.withToolLoading(ctx, "ordinary", tools, true)[0].WorkGuideTool {
		t.Fatal("full-loading preference overrode lab")
	}
	set(false, true)
	for _, tool := range s.withToolLoading(ctx, "ordinary", tools, true) {
		if tool.WorkGuideTool || tool.DeferLoading {
			t.Fatalf("rollback with load_all_tools: %+v", tool)
		}
	}
	set(false, false)
	b, _ := json.Marshal(baseline)
	off, _ := json.Marshal(s.withToolLoading(ctx, "ordinary", tools, true))
	if string(b) != string(off) || tools[0].WorkGuideTool {
		t.Fatal("rollback or copy", string(off), string(b))
	}
}

func TestWorkGuideScheduledPreflightKeepsOriginalTools(t *testing.T) {
	s := newFleetTest(t).s
	ctx := t.Context()
	s.noteCleanRound("grant", "task")
	tools := []proto.Tool{{Name: "task_plan"}}
	for name := range grantSafeTools {
		tools = append(tools, proto.Tool{Name: name})
	}
	before := s.withToolLoading(ctx, "grant", tools, true)
	setSettings(t, s, func(v *Settings) { v.WorkGuide = ptr(true) })
	after := s.withToolLoading(ctx, "grant", tools, true)
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if string(b) != string(a) {
		t.Fatalf("grant preflight changed: %s -> %s", b, a)
	}
	if grantSafeTools["work_tools_search"] {
		t.Fatal("discovery should not authorize a scheduled dispatch")
	}
}
