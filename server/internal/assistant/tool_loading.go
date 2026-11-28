package assistant

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Which Persona tools an assistant conversation loads up front (#7317). Claude Code sends a tool personad mcp marks
// alwaysLoad in full with every request; any other it lists by name only, and the model loads it with ToolSearch when
// it needs it (the assistant's --tools has ToolSearch, node/assistant.go). Every Persona tool used to be marked, about
// 22K tokens a request for tools most conversations never call; now only coreTools are, unless assistant.load_all_tools
// is on. Antigravity has no ToolSearch and a budget of its own: its MCP proxy declares the core tools by their rank and
// leaves the rest to persona_call (integrations/antigravity/src/mcp-proxy.mjs). Changing the setting wakes every
// running personad mcp's tool watch (mcp_tools_changes.go), so a running conversation is sent its new list at once; one
// whose personad mcp is older than tool refresh lists its tools only when its process starts, and keeps them until it
// restarts.

// coreTools are the tools assistant conversations call often, the most called first: the list follows this install's
// measured main-conversation use, calls through persona_call included. An extension's tool (computer_*, browser_*) is
// in a conversation's list only while its extension is on with a computer, and is matched here by name; the Lark tools
// are here under both of their names, the Lark extension's and the lark-cli connector's (connector/larkcli). Any other
// tool, a new one too, is deferred until it is added here. The order is each tool's CoreRank, the order in which a
// runtime that declares only what fits its budget (Antigravity's MCP proxy) takes them.
//
// This is the default: once the main chat's runtime has enough use, its own ranking by calls replaces it, cut to the
// runtime's budget (coreRanks, assistant/tooluse core.go), and a tool added here keeps at least its place for 30 days.
// tool_note and skill_save are deliberate tools, called now and then, and are not here on purpose: they join the core
// tools only by use.
var coreTools = []string{
	// 30 days of the main conversation on Antigravity to 2026-11-27, every tool called at least 4 times, by calls.
	"agent_read", "agents_list", "todo_get", "persona_settings", "task_review", "supervision_finish", "task_plan",
	"agent_send", "machines_list", "schedule_state_set", "agent_archive", "task_scope", "todo_list", "todo_update",
	"todo_create", "computer_run", "computer_state",
	// New and small, ranked so Antigravity declares it rather than leaving it to persona_call.
	"machine_exec",
	// Alphabetical: the ones an earlier count kept that eight days of this install still called (2026-11-20 to 11-28),
	// ask_user for the decisions it carries, and the browser's two, which do computer_*'s work in a browser. Left out
	// for use too rare to declare in every request: agent_interrupt, agent_start, agent_supervise, cancel_followup,
	// interaction_answer and schedule_routine (none or one call), list_followups (three) and model_profiles
	// (model_profiles_list's six): under the five a week a declared tool needs, they load on demand like any other.
	"ask_user", "browser_look", "browser_run", "conversation_read", "conversation_send", "conversations_list",
	"lark_chats", "lark_messages", "lark_search", "larkcli_chats", "larkcli_messages", "larkcli_search", "notify_user",
	"schedule_followup",
}

// The operations taught by persona-work. Match built-in names exactly: an
// extension sharing a prefix must keep its own loading policy.
var workGuideTools = []string{
	"machines_list", "agents_list", "agent_read", "agent_send", "agent_start", "agent_interrupt",
	"agent_switch", "agent_archive", "agent_supervise", "task_plan", "task_plan_withdraw", "task_scope",
	"task_start_repair", "task_review", "task_decided", "todo_list", "todo_get", "todo_create", "todo_update",
	"conversations_list", "conversation_read", "conversation_send", "repositories", "todo_links",
}

// withToolLoading is a conversation's tools with DeferLoading, CoreRank and Fill set by the manager alone, whatever the
// list came with (a connector program's tools are decoded as it sent them): when deferring (an assistant conversation
// that has ToolSearch) all but the core tools (coreRanks) are deferred, unless assistant.load_all_tools is on;
// otherwise (a guest, an agent) none is. The core tools of such a conversation are ranked whatever that setting says,
// and with it on the others are marked to fill: a runtime with no ToolSearch whose conversation gets this list
// (Antigravity's) has only these marks to choose what to declare. A copy: the list it is given may be shared.
func (s *Service) withToolLoading(ctx context.Context, self string, tools []proto.Tool, deferring bool) []proto.Tool {
	settings := s.settings(ctx)
	guide := deferring && settings.workGuideOn() && toolSetOf(ctx) == ""
	cleanRound := guide && s.cleanRoundSession(self)
	ranking, loadAll := deferring, settings.LoadAllTools != nil && *settings.LoadAllTools
	deferring = deferring && !loadAll
	out := slices.Clone(tools)
	var ranks map[string]int
	if ranking {
		ranks = s.coreRanks(ctx, out)
	}
	for i := range out {
		// Keep planning and its allowed preflight reads directly available in a granted round.
		// Discovery would add a separate tool call to the grant's transcript check.
		out[i].WorkGuideTool = guide && slices.Contains(workGuideTools, out[i].Name) &&
			!(cleanRound && (out[i].Name == "task_plan" || grantSafeListed(out[i].Name)))
		rank := ranks[out[i].Name]
		out[i].DeferLoading = deferring && rank == 0
		out[i].CoreRank, out[i].Fill = 0, false
		if ranking {
			out[i].CoreRank, out[i].Fill = rank, loadAll && rank == 0
		}
	}
	return out
}

// loadAllTools: every Persona tool loads up front (assistant.load_all_tools, off by default).
func (s *Service) loadAllTools(ctx context.Context) bool {
	v := s.settings(ctx).LoadAllTools
	return v != nil && *v
}

func (s *Service) setLoadAllTools(ctx context.Context, v json.RawMessage) (*ownChange, error) {
	change, err := s.setBoolSetting(ctx, v, s.loadAllTools, "assistant.load_all_tools is unchanged", func(l i18n.Lang, before, after string) string {
		return i18n.T(l, "assistant-settings_tool.change_load_all_tools", i18n.Args{"before": before, "after": after})
	}, func(v *Settings) **bool { return &v.LoadAllTools },
		"assistant.load_all_tools is %v: running conversations are sent their new tool list now, and new ones start with it "+
			"(a conversation on a computer whose Persona is older than tool refresh keeps its tools until its process restarts). "+
			"On Antigravity, which has no ToolSearch and declares only what fits its tool budget, the often-used tools come "+
			"first either way; true fills the budget they leave with the others, false declares only them. A tool Antigravity "+
			"was not given is called through persona_call.")
	if err != nil || change.apply == nil {
		return change, err
	}
	apply := change.apply
	change.apply = func(ctx context.Context) (string, error) {
		out, err := apply(ctx)
		if err == nil {
			// Saved first, so every watch this wakes lists its tools with the new setting.
			s.mcpChanges.notify()
		}
		return out, err
	}
	return change, nil
}
