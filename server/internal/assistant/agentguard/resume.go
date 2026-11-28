// Package agentguard holds the rules that decide what the assistant's agent tools may do to a coding Agent's session
// without the owner asking: resume it with no task, archive it. They are pure functions over plain values; the
// assistant root reads the facts and calls them.
package agentguard

import (
	"fmt"

	"github.com/sunfayao/persona/server/internal/proto"
)

// PlainResume says whether a resume with no task, of an Agent on no open task of the caller, may go: only the main
// conversation in a turn the owner's own message started, acting for the owner (the session is not supervised).
func PlainResume(isMain, ownerTurn bool) bool { return isMain && ownerTurn }

// PlainResumeReceipt is what the answer of such a resume adds: it was for the owner and nothing supervises it.
const PlainResumeReceipt = " Resumed for the owner; not supervised: no task was created, so nothing follows this Agent up. " +
	"Give task (title and done_when) with a send when its work should be supervised."

// NoTaskRefusal is why a resume with no task is refused for an Agent on no open task of the caller (label names it).
func NoTaskRefusal(label string, isMain bool) string {
	if !isMain {
		return fmt.Sprintf("%s is on no open task of yours: a resumed agent must be supervised, and only the main "+
			"conversation and a direction's lead supervise agents. Nothing was started or sent", label)
	}
	return fmt.Sprintf("%s is on no open task of yours: a resumed agent must be supervised, unless this turn is the owner's own "+
		"message asking you to wake it. Give task (a title and done_when for the follow-up) to make it your task as you resume it. "+
		"Nothing was started or sent", label)
}

// codexAppSpace is the space_kind of the Codex App's chats (node codex_desktop.go).
const codexAppSpace = "codex_app"

// Compactable: c runs on a runtime whose own compaction the node can run before a message (#11137): Claude Code in one
// of Persona's terminals, and Codex wherever Persona reaches it to resume or send: its terminal, a thread nothing runs
// that the node resumes there with codex resume (codex_history), or its app-server (codex_app_server). Not the Codex
// App's chats, a Claude outside Persona's terminals (claude_bg, claude_desktop, claude_cli: its inbox runs no command)
// or a runtime's session (Paseo and the like).
func Compactable(c proto.Session) bool {
	switch c.Agent {
	case "", "claude":
		return c.SourceKind == ""
	case "codex":
		return c.SpaceKind != codexAppSpace && (c.SourceKind == "" || c.SourceKind == "codex_history" || c.SourceKind == "codex_app_server")
	}
	return false
}

// NotCompactable says why c cannot be compacted from Persona, and what then.
func NotCompactable(c proto.Session) string {
	switch {
	case c.Agent == "codex" && c.SpaceKind == codexAppSpace:
		return "it is a Codex App chat, which takes no /compact from Persona (the App compacts on its own)"
	case c.SourceKind == proto.SourceClaudeBg || c.SourceKind == proto.SourceClaudeDesktop || c.SourceKind == proto.SourceClaudeTerminal:
		return "it is a Claude outside Persona's terminals, whose inbox runs no /compact"
	case c.SourceKind != "" && (c.Agent == "" || c.Agent == "claude" || c.Agent == "codex"):
		return "it is a " + c.SourceKind + " session, which Persona cannot compact"
	case c.SourceKind != "":
		return "it runs in " + c.SourceKind + ", which has no compaction Persona can run"
	}
	return c.Agent + " has no compaction Persona can run"
}

// ReopenWords is what the answer to a message with no task says when that message reopens the Agent's closed to-do id:
// the to-do is open again, and work that is new belongs on a task of its own.
func ReopenWords(id string) string {
	return fmt.Sprintf(" This plain message reopens to-do %s, which was closed for this Agent: it is open again and supervised again. "+
		"New work should come with task (a title and done_when), not as a plain message.", id)
}

// ExitedTerminal: c is a Persona terminal Agent that ended without an owner's Stop (told to quit, or on its own),
// which its node lets only the owner's own message start again (node stopped.go terminalExitLaunch: capabilities.send
// on a stopped row). A message of the assistant's is refused there, so for agent_send it is stopped all the same, and
// resume applies to it (the root's stoppedAgent leaves it out: the owner's own messages still go to it as before).
func ExitedTerminal(c proto.Session) bool {
	if c.Assistant || c.SourceKind != "" || c.IsSubagent || c.Capabilities == nil || !c.Capabilities.Send || !c.Capabilities.Terminal {
		return false
	}
	if c.Status != proto.StatusOffline && c.Status != proto.StatusInterrupted {
		return false
	}
	return c.Capabilities.StopCause == proto.StopUnknown || c.Capabilities.StopCause == proto.StopExit
}
