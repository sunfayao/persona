package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistant/agents"
	"github.com/sunfayao/persona/server/internal/assistant/tooluse"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/channel"
	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The nodes' MCP tools (mcp.tools / mcp.call): an assistant conversation's or an agent's Persona tools. The manager
// lists and routes them: the connectors', Cua's and ego's
// (the extensions', in this process: ExtensionTools, internal/extmgr), and the assistant's own (here). The skills' gated commands' settings go to
// the assistant's machine only (assistant.lark).

// mcpTools are the tools of an assistant conversation or an agent: those of the connectors that
// are ok and give their tools to agents too (mail); for the assistant also its own, then Cua's, ego's and those of
// the connectors only it gets (Feishu, GitHub…). A group whose feature is off or unused is left out (toolGates).
func (s *Service) mcpTools(ctx context.Context, assistant bool) []proto.Tool {
	ctx = i18n.With(ctx, s.lang(ctx))
	var agents, assistantOnly []proto.Tool
	if s.ext != nil {
		agents, assistantOnly = s.ext.AgentTools(ctx, assistant)
	}
	tools := agents
	if !assistant {
		tools = append(tools, toolsNamed(resourceToolNames)...)
	}
	if !assistant {
		tools = append(tools, s.listedAgentTools()...) // report_to_supervisor, todo_decisions, todo_decision_add, todo_bind_thread
		tools = append(tools, s.dispatchTools(ctx)...) // dispatch_start, dispatch_read, when agents.dispatch is on (dispatch.go)
	}
	if assistant {
		tools = append(tools, s.Tools(ctx)...)
		tools = append(tools, assistantOnly...)
	}
	return localizedMCPTools(s.toolGatesNow(ctx).filter(tools), s.toolLanguage(ctx))
}

// agentRepositoryToolNames are the Agents' tools for their to-do's repository and issue/PR links. They still route
// (agentToolNames) but no Agent lists them: the hub links every issue and PR an Agent's reply names, its closeout's
// artifact lines included, by itself (advanceTaskReply).
var agentRepositoryToolNames = []string{"todo_repositories_get", "todo_repositories_set", "todo_repository_links_set", "todo_repository_links_add"}

// listedAgentTools are the Agents' own tools an Agent's list shows: agentToolNames but the repository ones.
func (s *Service) listedAgentTools() []proto.Tool {
	return slices.DeleteFunc(s.AgentTools(), func(t proto.Tool) bool { return slices.Contains(agentRepositoryToolNames, t.Name) })
}

// toolGates say which feature-bound groups of Persona tools are listed now: a group whose feature is off, or unused
// on this install, is in no conversation's list (the slim-down of 2026-11-28). The calls still route, so a
// conversation that saw a tool earlier is answered by what the feature says. Mail's connector leaves itself out while
// no account is added (extmgr mailConnector.Status); dispatch is listed only with agents.dispatch on (dispatchTools).
type toolGates struct {
	bare       bool // a Service without settings (registry tests): everything listed, nothing in the revision
	inboxTwin  bool // inbox_suggest, inbox_folders, inbox_move: the Inbox's AI twin is on (inbox.agent.enabled)
	inboxPost  bool // inbox_post: assistant.inbox_post, on by default
	resources  bool // resource_*: shared runners are registered (shared_resources)
	chatThread bool // todo_bind_thread: a chat channel is on (a bot in a chat app, or /agent in someone's chat)
}

var (
	inboxTwinToolNames = []string{"inbox_suggest", "inbox_folders", "inbox_move"}
	chatThreadTool     = "todo_bind_thread"
)

// toolGatesNow are the gates as the settings, the hub's inbox and channels and the shared runners say now. What
// cannot be read counts as off: a tool is listed only for a feature known to be on.
func (s *Service) toolGatesNow(ctx context.Context) toolGates {
	var g toolGates
	if s.store == nil || s.store() == nil { // a bare Service (registry tests) has no settings: everything stays listed
		return toolGates{bare: true, inboxTwin: true, inboxPost: true, resources: true, chatThread: true}
	}
	g.inboxTwin = s.inboxConfig(ctx).Agent.Enabled
	g.inboxPost = s.inboxPostOn(ctx)
	if defs, err := s.resourceDefinitions(ctx); err == nil {
		g.resources = len(defs) > 0
	}
	if chans, err := s.channelsNow(ctx); err == nil {
		g.chatThread = slices.ContainsFunc(chans, func(c channel.View) bool {
			return c.Enabled && (c.Kind == channel.KindIM || c.Kind == channel.KindMention)
		})
	}
	return g
}

// listed says whether gates g list the tool named name.
func (g toolGates) listed(name string) bool {
	switch {
	case slices.Contains(inboxTwinToolNames, name):
		return g.inboxTwin
	case name == "inbox_post":
		return g.inboxPost
	case slices.Contains(resourceToolNames, name):
		return g.resources
	case name == chatThreadTool:
		return g.chatThread
	}
	return true
}

// filter is tools without those g does not list, in their order. A copy: tools may be shared.
func (g toolGates) filter(tools []proto.Tool) []proto.Tool {
	return slices.DeleteFunc(slices.Clone(tools), func(t proto.Tool) bool { return !g.listed(t.Name) })
}

// revision is g as a part of the tool-list revision (toolsCursor), so a gate that opens or closes reaches running
// conversations at their watch's next heartbeat.
func (g toolGates) revision() string {
	if g.bare {
		return ""
	}
	bit := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	return ":gates=" + bit(g.inboxTwin) + bit(g.inboxPost) + bit(g.resources) + bit(g.chatThread)
}

// sessionMCPTools are machine's conversation session's tools: a guest conversation's only its own (guest.go; the
// node saying it is one, the hub not knowing it or not able to tell: none), any other's mcpTools. The set the process
// asked for (mcp.tools tool_set, on ctx: toolSetOf): "supervise", a background supervisor's round, narrows an
// assistant conversation's list to the round's (roundTools, supervise_round.go); none, or one this manager does not
// know, is the whole list.
func (s *Service) sessionMCPTools(ctx context.Context, machine, session string, assistant, guestHint bool) []proto.Tool {
	tools, err := s.sessionMCPToolsChecked(ctx, machine, session, assistant, guestHint)
	if err != nil {
		s.logf("mcp tools: %v", err)
		return []proto.Tool{}
	}
	return tools
}

// Discovery with a cursor must distinguish a denied/empty list from a failed
// state read. Otherwise the failed list is cached until an unrelated link changes.
func (s *Service) sessionMCPToolsChecked(ctx context.Context, machine, session string, assistant, guestHint bool) (result []proto.Tool, resultErr error) {
	defer func() { result = localizedMCPTools(result, s.toolLanguage(ctx)) }()
	if assistant && !s.assistantDiscovery(ctx, machine, session) {
		s.logf("mcp tools: assistant discovery denied for machine %s session %s (tool_set=%q); falling back to agent tools", machine, short(session), toolSetOf(ctx))
		s.noteAssistantDenied(ctx, machine, session, "mcp.tools")
		assistant = false
	}
	allowed, guest, err := s.guestOf(ctx, machine, session)
	if err != nil {
		return nil, err
	}
	if !guest {
		if guestHint {
			return []proto.Tool{}, nil
		}
		if s.isWorkMachine(machine) {
			return s.toolGatesNow(ctx).filter(s.listedAgentTools()), nil
		}
		if !assistant && session != "" {
			if err := s.ensureTodosLoaded(ctx); err != nil {
				return nil, err
			}
		}
		tools := s.mcpTools(ctx, assistant)
		if assistant {
			tools = s.guardTools(ctx, machine, session, tools)
			tools = append(tools, s.spaceTools(ctx, machine, session)...) // space_state_update (track_space_md.go)
		}
		// A to-do's supervisor (a task leader) lists only its own tools beside the Agents' own: it reaches its workers
		// through them (task_leader.go), so neither task_spawn nor A2A is a side channel for it.
		leads := false
		if !assistant {
			own := s.leaderTools(ctx, machine, session)
			leads = len(own) > 0
			tools = append(tools, own...)
			if !leads {
				tools = append(tools, s.spawnTools(ctx, machine, session)...)
			}
		}
		// A2A is the Agents' channel to each other. An assistant conversation reaches the same sessions with its own
		// agents_list, agent_read and agent_send, and is answered without the tools (prompts/agents/a2a_message).
		if !assistant && !leads && s.agents.Discoverable(ctx, agents.Ref{Machine: s.canonical(machine), Session: session}) {
			tools = append(tools, toolsNamed(assistantwire.A2AToolNames)...)
		}
		if assistant && toolSetOf(ctx) == toolSetSupervise {
			tools = roundTools(tools)
		}
		return tools, nil
	}
	if s.isWorkMachine(machine) {
		return []proto.Tool{}, nil
	}
	return guestTools(s.mcpTools(ctx, assistant), allowed), nil
}

// guestRefuses refuses a guest conversation's call of a tool it was not given (and any call of one the hub cannot
// tell about, or that the node says is a guest the hub does not know).
func (s *Service) guestRefuses(ctx context.Context, machine, session, name string, guestHint bool) (proto.ToolResult, bool) {
	allowed, guest, err := s.guestOf(ctx, machine, session)
	switch {
	case err != nil:
		s.logf("mcp call %s: %v", name, err)
		return proto.TextResult("tool unavailable for the moment: "+name, true), true
	case !guest && guestHint:
		return proto.TextResult("unknown tool "+name, true), true
	case guest && !slices.Contains(allowed, name):
		return proto.TextResult("unknown tool "+name, true), true
	}
	return proto.ToolResult{}, false
}

// mcpCall runs one: a connector's, Cua's or ego's at the hub (it says whether it is one of them), else the
// assistant's own (for its conversations).
func (s *Service) mcpCall(ctx context.Context, machine, session, name string, args json.RawMessage, assistant bool) proto.ToolResult {
	return s.mcpCallWithOrigin(ctx, machine, session, name, args, assistant, "")
}

// mcpCallWithOrigin also takes the node's attested conversation origin. It is needed before assistant.send returns:
// the manager's origin book is written from the returned session afterwards. An assistant conversation's call is
// recorded (tool_use.go).
func (s *Service) mcpCallWithOrigin(ctx context.Context, machine, session, name string, args json.RawMessage, assistant bool, origin string) proto.ToolResult {
	if !assistant { // an Agent's call: counted, no more (its runtime is tooluse.WorkerRuntime)
		res := s.mcpCallRun(ctx, machine, session, name, args, assistant, origin)
		if s.toolUse != nil {
			s.toolUse.Record(tooluse.Call{Tool: compat.Tool(name), Runtime: tooluse.WorkerRuntime, OK: !res.IsError})
		}
		return res
	}
	r := &callRecord{}
	res := s.mcpCallRun(withCallRecord(ctx, r), machine, session, name, args, assistant, origin)
	s.recordToolCall(ctx, machine, session, compat.Tool(name), args, r, res)
	return res
}

func (s *Service) mcpCallRun(ctx context.Context, machine, session, name string, args json.RawMessage, assistant bool, origin string) proto.ToolResult {
	if s.isWorkMachine(machine) {
		if isAgentTool(name) {
			return s.agentTool(ctx, machine, session, name, args)
		}
		noteCallCode(ctx, "tool_denied")
		return proto.TextResult(i18n.T(s.lang(ctx), "assistant-privacy.tool_denied"), true)
	}
	if assistant && !s.assistantCaller(ctx, machine, session) {
		s.noteAssistantDenied(ctx, machine, session, name)
		noteCallCode(ctx, "assistant_denied")
		return proto.TextResult(i18n.T(s.lang(ctx), "assistant-privacy.assistant_denied"), true)
	}
	if assistant { // a merged tool's call is the older tool's from here on, for every check by name (unmergeTool)
		unmerged, unmergedArgs, err := unmergeTool(compat.Tool(name), args)
		if err != nil {
			noteCallError(ctx, err)
			return proto.TextResult(err.Error(), true)
		}
		name, args = unmerged, unmergedArgs
	}
	if res, refused := s.inboxModelRefusal(ctx, machine, session, origin, name); refused {
		return res
	}
	if slices.Contains(resourceToolNames, name) {
		return s.resourceTool(ctx, machine, session, name, args)
	}
	if name == "secret_store" {
		return s.toolSecretStore(ctx, machine, session, args)
	}
	if assistantwire.IsA2ATool(name) {
		if !assistant {
			if _, _, leads := s.leaderTask(ctx, machine, session); leads { // as its list says: not a supervisor's channel
				return proto.TextResult("unknown tool "+name, true)
			}
		}
		return s.agents.Tool(ctx, agents.Ref{Machine: s.canonical(machine), Session: session}, name, args)
	}
	if s.ext != nil { // the connectors', Cua's, ego's: ctx is the node's call (its caller going away withdraws a card)
		if s.ext.ConnectorTool(name) { // a connector is told where the call comes from (connector_context.go)
			ctx = connector.WithCallContext(ctx, s.connectorCallContext(ctx, session))
		}
		if res, ok := s.ext.CallTool(ctx, machine, session, name, args, assistant); ok {
			return res
		}
	}
	if !assistant && isAgentTool(name) {
		return s.agentTool(ctx, machine, session, name, args)
	}
	if !assistant && name == "task_spawn" {
		text, err := s.taskSpawn(ctx, machine, session, args)
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		return proto.TextResult(text, false)
	}
	if !assistant && name == "task_plan" { // only a task leader's agent has it (task_leader.go)
		return s.leaderTaskPlan(ctx, machine, session, args)
	}
	if !assistant && name == "task_review" {
		return s.leaderTaskReview(ctx, machine, session, args)
	}
	if !assistant && isLeaderTool(name) { // agent_read, agent_send, task_scope: a task leader's own (task_leader.go)
		return s.leaderScopedTool(ctx, machine, session, name, args)
	}
	if !assistant && isDispatchTool(name) {
		return s.dispatchTool(ctx, machine, session, name, args)
	}
	if assistant && name == spaceStateTool { // listed per conversation (spaceTools), so not in Tools: its handler checks
		return s.Tool(ctx, ToolCall{Name: name, Arguments: args, MachineID: machine, SessionID: session})
	}
	if assistant && s.isOwnTool(ctx, name) {
		return s.Tool(ctx, ToolCall{Name: name, Arguments: args, MachineID: machine, SessionID: session})
	}
	return proto.TextResult("unknown tool "+name, true)
}

// mailAttachment answers a node's mail.attachment: the bytes of the attachment mail_save_attachment saves on that
// machine (node/mailattachment.go), for a caller that could call the tool through mcp.call: not a work machine, an
// assistant conversation only when it is one, a guest only when it was given the tool; then the extensions decide
// whether the mail connector offers it to that caller (extmgr MailAttachment).
func (s *Service) mailAttachment(ctx context.Context, machine string, req proto.MailAttachmentRequest) (proto.MailAttachment, error) {
	// Not named "name": the i18n key check resolves constants by name across the package, and a constant called
	// name would turn every "prefix."+name text wrapper into one whole key.
	const saveAttachmentTool = "mail_save_attachment"
	if s.isWorkMachine(machine) {
		return proto.MailAttachment{}, errors.New(i18n.T(s.lang(ctx), "assistant-privacy.tool_denied"))
	}
	if req.Assistant && !s.assistantCaller(ctx, machine, req.SessionID) {
		return proto.MailAttachment{}, errors.New(i18n.T(s.lang(ctx), "assistant-privacy.assistant_denied"))
	}
	res, refused := s.guestRefuses(ctx, machine, req.SessionID, saveAttachmentTool, req.Guest)
	if refused {
		var why strings.Builder
		for _, c := range res.Content {
			why.WriteString(c.Text)
		}
		return proto.MailAttachment{}, errors.New(why.String())
	}
	if s.ext == nil {
		return proto.MailAttachment{}, errors.New("mail is not available")
	}
	return s.ext.MailAttachment(ctx, req)
}

// isOwnTool: name is one of the assistant's own tools, a retired one too (retiredToolNames).
func (s *Service) isOwnTool(ctx context.Context, name string) bool {
	if slices.Contains(retiredToolNames, name) {
		return true
	}
	for _, t := range s.Tools(ctx) {
		if t.Name == name {
			return true
		}
	}
	return false
}

// assistantLark is what the assistant's machine gets for its skills' commands (proto.AssistantLark): lark-cli's
// settings and rules, and the plugins' command gates (the hub's, app.call hub). The credentials go to the assistant's
// machine only (its process runs them): another personal machine gets otherMachineLark, a work machine nothing.
func (s *Service) assistantLark(ctx context.Context, machine string) (proto.AssistantLark, error) {
	if s.isWorkMachine(machine) || s.ext == nil {
		return proto.AssistantLark{}, nil
	}
	cfg, err := s.ext.AssistantLark(ctx)
	if err != nil || s.sameMachine(machine, s.settings(ctx).MachineID) {
		return cfg, err
	}
	return otherMachineLark(cfg), nil
}

// otherMachineLark is what another personal machine's gate gets for the assistant conversations the manager starts
// there (a Lark /agent placed on it, #5479): the rules and lark-cli's name, so its lark-cli calls go by lark-cli's own
// risk levels instead of all being confirmed. Not the command gates (their credentials) nor the extensions' MCP
// servers, and not the profile, which names one in the manager computer's lark-cli: that machine uses its lark-cli's
// default profile, and an absolute bin is looked up there by its base name.
func otherMachineLark(cfg proto.AssistantLark) proto.AssistantLark {
	out := proto.AssistantLark{Rules: cfg.Rules}
	if cfg.CLI != nil {
		out.CLI = &proto.LarkCLI{Bin: cfg.CLI.Bin}
		if out.CLI.Bin != "" {
			out.CLI.Bin = filepath.Base(out.CLI.Bin)
		}
	}
	return out
}

// assistantSummary is the text a gated command's card shows, as its plugin names what the ids are; false: none.
func (s *Service) assistantSummary(ctx context.Context, tool, summary string) (string, bool) {
	if tool == "" {
		return summary, false
	}
	if s.ext == nil {
		return summary, false
	}
	return s.ext.AssistantSummary(ctx, tool, summary)
}

// Localize only fixed built-in definitions, after discovery/permission filtering.
// The source slices and third-party tool contracts are never rewritten.
func localizedMCPTools(tools []proto.Tool, lang string) []proto.Tool {
	out := slices.Clone(tools)
	for i, t := range out {
		localized := prompts.LocalizeTool(prompts.MCPTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}, lang)
		out[i].Description, out[i].InputSchema = localized.Description, localized.InputSchema
	}
	return out
}

// A zero Service can enumerate its static tools without a configured hub.
func (s *Service) toolLanguage(ctx context.Context) string {
	if s.hub == nil {
		return ""
	}
	return string(s.lang(ctx))
}
