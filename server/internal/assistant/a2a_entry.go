package assistant

// How an A2A message tells its recipient to reach Persona's tools without the Persona MCP tools (#13893): by its node's
// executable, named as the task handoff names it (task_handoff.go), since `personad` is often not on such an Agent's
// PATH. The A2A tools bind a terminal caller by its pane or Codex thread (node/clicall.go a2aCaller), whatever the
// program is called. A recipient with the Persona MCP tools, or one whose node names no executable, keeps the message's
// text as it was.

import (
	"context"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// a2aSessionExecutable is machine's node program, as a JSON string, for an A2A message to running session sess: a
// session that calls its tools from a terminal the node verifies (its pane, a Codex thread) and has no Persona MCP
// tools (sessionEntry). "" for one with the MCP tools, for a runtime's (its harness wires the A2A tools in, or it
// answers through chat), and when the node does not say.
func (s *Service) a2aSessionExecutable(ctx context.Context, machine string, sess proto.Session) string {
	if mcp, terminal := sessionEntry(sess); mcp || !terminal {
		return ""
	}
	return s.nodeExecutable(ctx, machine)
}

// a2aLaunchExecutable is the same for the first message of an Agent an A2A task starts with launcher on machine, whose
// harness is not known before it starts: named unless the launcher is known, without asking the computer, to run
// Claude Code, which Persona starts with its MCP tools (the node's --mcp-config). The sentence that names it applies
// only to a harness without those tools, so naming it for another harness changes nothing it does.
func (s *Service) a2aLaunchExecutable(ctx context.Context, machine, launcher string) string {
	if s.a2aLaunchesClaude(ctx, launcher) {
		return ""
	}
	return s.nodeExecutable(ctx, machine)
}

// a2aLaunchesClaude: launcher runs Claude Code by its own name or as a Persona-managed provider of that type; false when
// telling would take the computer's launcher list, which a launch does not wait for.
func (s *Service) a2aLaunchesClaude(ctx context.Context, launcher string) bool {
	if assistantKind(launcher) != "" {
		return launcher == "claude"
	}
	m, managed := s.managedNamed(ctx, launcher)
	return managed && m.AgentType() == "claude"
}

// a2aTaskMessage is event e of task t from from, in language, as prompts/agents/a2a_message takes it, without the
// recipient's executable: the caller adds that once it knows the recipient (a2aSessionExecutable, a2aLaunchExecutable).
func a2aTaskMessage(t *a2aTask, e *a2aEvent, from a2aRef, language string) prompts.A2AMessage {
	state := e.State
	if state == "" {
		state = t.State
	}
	return prompts.A2AMessage{ID: t.ID, Sender: a2aMessageSource(from).Title, State: state, Request: e.Kind == "message",
		Text: e.Text, Language: language}
}

// a2aRelay is m as its recipient receives it: rendered, then marked with its source and its delivery id.
func a2aRelay(m prompts.A2AMessage, from a2aRef, id string) string {
	return prompts.RelayMessageText(prompts.RenderA2AMessage(m), a2aMessageSource(from), id)
}
