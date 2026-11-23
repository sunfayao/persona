package prompts

import (
	"strings"
	"testing"
)

func TestA2AMessageLanguage(t *testing.T) {
	for language, firstLine := range map[string]string{
		"en":      "Persona A2A task task-id from machine/session is submitted.",
		"zh-Hans": "Persona A2A 任务 task-id 来自 machine/session，状态为 submitted。",
		"ja":      "machine/session からの Persona A2A タスク task-id。現在の状態は submitted です。",
	} {
		req := A2AMessage{ID: "task-id", Sender: "machine/session", State: "submitted", Text: "Original input.", Language: language, Request: true}
		got := RenderA2AMessage(req)
		if !strings.HasPrefix(got, firstLine+"\n") || !strings.Contains(got, "a2a_report") || !strings.HasSuffix(got, "\n\n"+req.Text) {
			t.Fatalf("request in %s: %q", language, got)
		}
		req.Request = false
		got = RenderA2AMessage(req)
		if !strings.Contains(got, "a2a_send") || strings.Contains(got, "a2a_report") {
			t.Fatalf("reply in %s: %q", language, got)
		}
	}
}

// With the recipient node's executable (#13893), only the sentence for a harness without the Persona MCP tools changes:
// it names the executable as JSON data and the tool the message asks for, instead of personad.
func TestA2AMessageNamesTheExecutable(t *testing.T) {
	const executable = `"/opt/persona tools/personad"`
	for _, language := range []string{"en", "zh-Hans", "ja"} {
		for _, request := range []bool{true, false} {
			m := A2AMessage{ID: "task-id", Sender: "machine/session", State: "input_required", Text: "Original input.", Language: language, Request: request}
			plain := RenderA2AMessage(m)
			m.Executable = executable
			named := RenderA2AMessage(m)
			tool := map[bool]string{true: "a2a_report", false: "a2a_send"}[request]
			if !strings.Contains(plain, "personad tool") || strings.Contains(plain, executable) || strings.Contains(named, "personad tool") ||
				!strings.Contains(named, executable) || !strings.Contains(named, "`tool`") || !strings.Contains(named, tool) {
				t.Fatalf("%s, request %v:\n%s\n---\n%s", language, request, plain, named)
			}
			plainLines, namedLines := strings.Split(plain, "\n"), strings.Split(named, "\n")
			if len(plainLines) != len(namedLines) || !strings.HasSuffix(named, "\n\nOriginal input.") {
				t.Fatalf("%s, request %v: the message's shape changed:\n%s", language, request, named)
			}
			for i := range plainLines {
				if plainLines[i] != namedLines[i] && !strings.Contains(namedLines[i], executable) {
					t.Fatalf("%s, request %v: line %d changed besides the terminal sentence: %q", language, request, i, namedLines[i])
				}
			}
		}
	}
}
