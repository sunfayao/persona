package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// a2aTestProgram is a node's program with a space in its path: it reaches the Agent as JSON data, never shell code.
const a2aTestProgram = "/opt/persona tools/personad"

// a2aAnswerNodeInfo has the nodes name a2aTestProgram and counts how often they are asked.
func a2aAnswerNodeInfo(h *a2aTestHub) *atomic.Int32 {
	var asked atomic.Int32
	h.answerNode("node.info", func(string, json.RawMessage) (any, error) {
		asked.Add(1)
		return proto.NodeInfo{OS: "linux", Executable: a2aTestProgram}, nil
	})
	return &asked
}

func a2aTestExecutable() string {
	encoded, _ := json.Marshal(a2aTestProgram)
	return string(encoded)
}

// What a task message's recipient reads (#13893), in each language: a request and a question for its sender, naming the
// node's executable for a harness without the Persona MCP tools, and, with no executable, the text that says personad.
func TestGoldenA2AMessage(t *testing.T) {
	task := &a2aTask{A2ARow: db.A2ARow{ID: "task-0001", State: a2aSubmitted}}
	request := &a2aEvent{Seq: 1, Kind: "message", State: a2aSubmitted, Text: "Run the integration checks and return the failing cases."}
	question := &a2aEvent{Seq: 3, Kind: "status", State: a2aInput, Text: "Which target should I use?"}
	from := a2aRef{"mac", "caller-0001"}
	for _, lang := range []string{"en", "zh-Hans", "ja"} {
		golden(t, "a2a_message.request."+lang, a2aPrompt(task, request, from, lang, ""))
		golden(t, "a2a_message.request_executable."+lang, a2aPrompt(task, request, from, lang, a2aTestExecutable()))
		golden(t, "a2a_message.question_executable."+lang, a2aPrompt(task, question, from, lang, a2aTestExecutable()))
	}
}

// A task message names the executable to a recipient that calls its tools from a terminal without the Persona MCP
// tools (a Codex pane). One with the MCP tools (a Claude Code pane) and a runtime's keep the text that says personad,
// and their node is not asked.
func TestA2AMessageNamesTheExecutableOnlyForATerminalWithoutMCP(t *testing.T) {
	s, h, st, cfg, parent, runtime := a2aFixture(t)
	asked := a2aAnswerNodeInfo(h)
	h.mu.Lock()
	h.sessions["box"] = append(h.sessions["box"],
		proto.Session{SessionID: "codex-pane-0001", Status: proto.StatusIdle, Agent: "codex"},
		proto.Session{SessionID: "claude-pane-0001", Status: proto.StatusIdle, Agent: "claude"})
	h.mu.Unlock()
	for i, tc := range []struct {
		session string
		names   bool
	}{{"codex-pane-0001", true}, {"claude-pane-0001", false}, {runtime.Session, false}} {
		tk, err := s.a2aStart(t.Context(), st, cfg, parent, a2aRef{"box", tc.session}, fmt.Sprintf("request-%d", i), "", "Run the checks.", true, nil)
		if err != nil {
			t.Fatal(err)
		}
		a2aProcessOne(t, s, st, cfg, tk.ID)
		sends := h.sentCalls()
		if len(sends) != i+1 {
			t.Fatalf("%s: %d deliveries", tc.session, len(sends))
		}
		var p struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if err := json.Unmarshal(sends[i].Params, &p); err != nil || p.SessionID != tc.session {
			t.Fatalf("%s: delivered %+v %v", tc.session, p, err)
		}
		if strings.Contains(p.Text, a2aTestExecutable()) != tc.names || strings.Contains(p.Text, "personad tool") == tc.names ||
			prompts.RelayMessageID(p.Text) != a2aDeliveryID(tk.ID, 1) || !strings.Contains(p.Text, "Run the checks.") {
			t.Fatalf("%s (names the executable: %v):\n%s", tc.session, tc.names, p.Text)
		}
	}
	if n := asked.Load(); n != 1 {
		t.Fatalf("node.info asked %d times, want once (the Codex pane's delivery)", n)
	}
}

// The first message of an Agent an A2A task launches names the executable, unless its launcher runs Claude Code, which
// Persona starts with its MCP tools.
func TestA2ALaunchNamesTheExecutableUnlessClaudeCode(t *testing.T) {
	for _, tc := range []struct {
		launcher string
		names    bool
	}{{"codex", true}, {"claude", false}} {
		t.Run(tc.launcher, func(t *testing.T) {
			s, h, st, cfg, parent, _ := a2aFixture(t)
			h.fakeModel(t)
			a2aAnswerNodeInfo(h)
			h.startReply = func(args map[string]any, sess *proto.Session) *proto.StartInstead {
				sess.SessionID = args["session_id"].(string)
				return nil
			}
			tk, err := s.a2aStart(t.Context(), st, cfg, parent, a2aRef{Machine: "box"}, "launch", "", "Inspect the build.", true,
				&a2aLaunch{CWD: "/code/project", Launcher: tc.launcher, Pending: true})
			if err != nil {
				t.Fatal(err)
			}
			a2aProcessOne(t, s, st, cfg, tk.ID)
			waitFor(t, "the A2A launch", func() bool { return len(h.started()) == 1 })
			start := h.started()[0]
			prompt, _ := start["prompt"].(string)
			if start["launcher"] != tc.launcher || strings.Contains(prompt, a2aTestExecutable()) != tc.names ||
				prompts.RelayMessageID(prompt) != a2aDeliveryID(tk.ID, 1) || !strings.Contains(prompt, "Inspect the build.") {
				t.Fatalf("launch with %s (names the executable: %v): %v\n%s", tc.launcher, tc.names, start["launcher"], prompt)
			}
		})
	}
}
