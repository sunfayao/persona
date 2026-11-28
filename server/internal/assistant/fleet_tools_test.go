package assistant

import (
	"context"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A feature-bound group of tools is listed only while its feature is on: the Inbox twin's while the twin is on, the
// shared runners' while one is registered, todo_bind_thread while a chat channel is on; inbox_post follows
// assistant.inbox_post, on by default. The Agents never list their to-do's repository tools (the hub links the PRs
// their closeouts name) and an assistant conversation never lists A2A. Every gate is in the list's revision, so a
// running conversation learns of a gate that opened at its watch's next heartbeat.
func TestFeatureBoundToolsFollowTheirFeature(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	names := func(assistant bool) map[string]bool {
		out := map[string]bool{}
		for _, tool := range h.mcpTools(ctx, assistant) {
			out[tool.Name] = true
		}
		return out
	}
	main, agent := names(true), names(false)
	for _, name := range []string{"inbox_suggest", "inbox_folders", "inbox_move", "a2a_send"} {
		if main[name] {
			t.Errorf("%s listed for the main conversation with its feature off", name)
		}
	}
	if !main["inbox_post"] || !main["agent_read"] || !main["persona_settings"] {
		t.Errorf("the main conversation's own tools: %v", main)
	}
	for _, name := range []string{"resource_acquire", "resource_status", "todo_bind_thread", "todo_repositories_get", "todo_repository_links_add"} {
		if agent[name] {
			t.Errorf("%s listed for an Agent", name)
		}
	}
	if !agent["report_to_supervisor"] || !agent["todo_decisions"] {
		t.Errorf("an Agent's own tools: %v", agent)
	}
	closed := h.toolGatesNow(ctx).revision()
	h.setConfig(func(c *assistantwire.InboxConfig) { c.Agent.Enabled = true })
	if !names(true)["inbox_suggest"] || !names(true)["inbox_move"] {
		t.Error("the Inbox twin's tools are not listed while it is on")
	}
	if open := h.toolGatesNow(ctx).revision(); open == closed {
		t.Errorf("the revision did not move when the twin came on: %q", open)
	}
	gates := toolGates{inboxTwin: true, inboxPost: false, resources: true, chatThread: true}
	listed := gates.filter([]proto.Tool{{Name: "inbox_post"}, {Name: "resource_get"}, {Name: "todo_bind_thread"}, {Name: "task_plan"}})
	if len(listed) != 3 || listed[0].Name != "resource_get" || listed[1].Name != "todo_bind_thread" {
		t.Errorf("with inbox_post off: %v", listed)
	}
}
