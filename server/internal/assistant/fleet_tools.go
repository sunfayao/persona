package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistant/agents"
	"github.com/sunfayao/persona/server/internal/assistantwire"
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
// the connectors only it gets (Feishu, GitHub…).
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
		tools = append(tools, s.AgentTools()...)       // report_to_supervisor, todo_decisions, todo_decision_add, todo_bind_thread (agentToolNames)
		tools = append(tools, s.dispatchTools(ctx)...) // dispatch_start, dispatch_read, when agents.dispatch is on (dispatch.go)
	}
	if assistant {
		tools = append(tools, s.Tools(ctx)...)
		tools = append(tools, assistantOnly...)
	}
	return localizedMCPTools(tools, s.toolLanguage(ctx))
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
			return s.AgentTools(), nil
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
		if !assistant {
			tools = append(tools, s.leaderTools(ctx, machine, session)...)
			tools = append(tools, s.spawnTools(ctx, machine, session)...)
		}
		if s.agents.Discoverable(ctx, agents.Ref{Machine: s.canonical(machine), Session: session}) {
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
// the manager's origin book is written from the returned session afterwards.
func (s *Service) mcpCallWithOrigin(ctx context.Context, machine, session, name string, args json.RawMessage, assistant bool, origin string) proto.ToolResult {
	if s.isWorkMachine(machine) {
		if isAgentTool(name) {
			return s.agentTool(ctx, machine, session, name, args)
		}
		return proto.TextResult(i18n.T(s.lang(ctx), "assistant-privacy.tool_denied"), true)
	}
	if assistant && !s.assistantCaller(ctx, machine, session) {
		s.noteAssistantDenied(ctx, machine, session, name)
		return proto.TextResult(i18n.T(s.lang(ctx), "assistant-privacy.assistant_denied"), true)
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

// isOwnTool: name is one of the assistant's own tools.
func (s *Service) isOwnTool(ctx context.Context, name string) bool {
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
