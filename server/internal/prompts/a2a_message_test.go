package prompts

import (
	"strings"
	"testing"
)

// One English note whatever the language, then the sender's text. A request says the final reply is the result and how to
// ask first; an update names the state and, only when the task waits for input, how to answer.
func TestA2AMessageLanguage(t *testing.T) {
	const head = "(Persona: the Agent session machine/session sent you this message; the user did not type it. It is A2A task task-id."
	for _, language := range []string{"", "en", "zh-Hans", "ja"} {
		req := A2AMessage{ID: "task-id", Sender: "machine/session", State: "submitted", Text: "Original input.", Language: language, Request: true}
		got := RenderA2AMessage(req)
		if !strings.HasPrefix(got, head) || !strings.Contains(got, "a2a_report") || !strings.HasSuffix(got, "\n\n"+req.Text) ||
			strings.Count(got, "\n") != 2 {
			t.Fatalf("request in %s: %q", language, got)
		}
		req.Request = false
		got = RenderA2AMessage(req)
		if strings.Contains(got, "a2a_send") || strings.Contains(got, "a2a_report") || !strings.Contains(got, "submitted") ||
			!strings.HasSuffix(got, "\n\n"+req.Text) {
			t.Fatalf("update in %s: %q", language, got)
		}
		req.State = "input_required"
		got = RenderA2AMessage(req)
		if !strings.Contains(got, "a2a_send") || strings.Contains(got, "a2a_report") || !strings.Contains(got, "A2A task-id answer") {
			t.Fatalf("question in %s: %q", language, got)
		}
	}
}

// What the note says about reaching the tools follows what is known of the recipient: with the Persona MCP tools, the
// tool alone; with its node's executable (#13893), the executable as JSON data and the reply line that stands in for
// it; with neither, the tool and that reply line.
func TestA2AMessageFollowsTheRecipientsEntry(t *testing.T) {
	const executable = `"/opt/persona tools/personad"`
	for _, language := range []string{"en", "zh-Hans", "ja"} {
		for _, request := range []bool{true, false} {
			m := A2AMessage{ID: "task-id", Sender: "machine/session", State: "input_required", Text: "Original input.", Language: language, Request: request}
			tool := map[bool]string{true: "a2a_report", false: "a2a_send"}[request]
			line := map[bool]string{true: "A2A task-id input_required", false: "A2A task-id answer"}[request]
			unknown := RenderA2AMessage(m)
			m.MCP = true
			mcp := RenderA2AMessage(m)
			m.MCP, m.Executable = false, executable
			named := RenderA2AMessage(m)
			for name, got := range map[string]string{"unknown": unknown, "mcp": mcp, "named": named} {
				if !strings.Contains(got, tool) || !strings.HasSuffix(got, "\n\nOriginal input.") || strings.Count(got, "\n") != 2 ||
					strings.Contains(got, "personad tool") {
					t.Fatalf("%s, request %v, %s:\n%s", language, request, name, got)
				}
			}
			if strings.Contains(mcp, line) || strings.Contains(mcp, "`tool`") || strings.Contains(mcp, executable) {
				t.Fatalf("%s, request %v: a recipient with the MCP tools is told more than the tool:\n%s", language, request, mcp)
			}
			if !strings.Contains(named, executable) || !strings.Contains(named, "`tool`") || !strings.Contains(named, line) ||
				!strings.Contains(named, `"task_id":"task-id"`) {
				t.Fatalf("%s, request %v: the terminal entry:\n%s", language, request, named)
			}
			if !strings.Contains(unknown, line) || strings.Contains(unknown, "`tool`") || strings.Contains(unknown, executable) {
				t.Fatalf("%s, request %v: an unknown entry:\n%s", language, request, unknown)
			}
			if len(mcp) >= len(unknown) || len(unknown) >= len(named) {
				t.Fatalf("%s, request %v: sizes mcp %d, unknown %d, named %d", language, request, len(mcp), len(unknown), len(named))
			}
		}
	}
}
