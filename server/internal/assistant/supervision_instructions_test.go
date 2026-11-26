package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestGoldenSupervisionInstructions(t *testing.T) {
	text := prompts.RenderSupervisionInstructions()
	golden(t, "supervision_instructions", text)
	raw, err := os.ReadFile("../prompts/assistant/supervision_instructions.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "{{") || strings.TrimSuffix(string(raw), "\n") != text {
		t.Fatal("supervision policy is not the exact static template bytes")
	}
}

// A bullet must start on its own line. A dropped newline ("...rule.- `next`: ...") renders two rules as one bullet,
// and the size ratchet/golden do not catch it because they are regenerated from the glued text (#12648 review).
func TestSupervisionInstructionsBulletsAreSeparateLines(t *testing.T) {
	raw, err := os.ReadFile("../prompts/assistant/supervision_instructions.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, ".- ") {
			t.Fatalf("two bullets are glued on one line (missing newline): %q", line)
		}
	}
}

func TestSupervisionInstructionsFirstStartAndWarm(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	yes := true
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks = &yes })
	policy := prompts.RenderSupervisionInstructions()
	for _, tc := range []struct {
		name, purpose string
		main, want    bool
	}{
		{"main", "", true, true},
		{"side", "", false, false},
		{"purpose alone is not authority", purposeSupervise, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.startOn(ctx, "server", StartParams{Text: "Hello", Purpose: tc.purpose}, tc.main); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			got := f.sends[len(f.sends)-1]["supervision_instructions"]
			f.mu.Unlock()
			if (got == policy) != tc.want || !tc.want && got != "" {
				t.Fatal("first start has the wrong policy")
			}
		})
	}
	// The legacy preference no longer demotes main: it retains authority for its own tasks and lead escalations.
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks = nil })
	if got := mustSupervisionInstructions(t, s, ctx, "server", s.settings(ctx).Main); got != policy {
		t.Fatal("main lost its policy with the legacy preference unset")
	}
	no := false
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks = &no })
	if got := mustSupervisionInstructions(t, s, ctx, "server", s.settings(ctx).Main); got != policy {
		t.Fatal("main lost its policy with the legacy preference off")
	}
	s.noBackground = false
	setSettings(t, s, func(v *Settings) { v.BackgroundSupervisor, v.BackgroundSupervisorMachineID = "bg1", "server" })
	if got := mustSupervisionInstructions(t, s, ctx, "server", "bg1"); got != policy {
		t.Fatal("the existing legacy background supervisor lacks exact static policy")
	}
	setSettings(t, s, func(v *Settings) { v.BackgroundSupervisor, v.BackgroundSupervisorMachineID = "", "" })
	s.warm("")
	f.mu.Lock()
	defer f.mu.Unlock()
	found := false
	for _, call := range f.calls {
		if call.Method != DeviceAssistantWarm {
			continue
		}
		var p map[string]any
		if err := json.Unmarshal(call.Params, &p); err != nil {
			t.Fatal(err)
		}
		found = true
		if p["supervision_instructions"] != policy || p["is_main"] != true {
			t.Fatal("main warm did not use its startup role")
		}
	}
	if !found {
		t.Fatal("no warm dispatch")
	}
}

func TestRelayAcknowledgementAndSilentSupervisionInstructions(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	yes := true
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks = &yes })
	// #5732 does not start a new background supervisor. Use an existing legacy conversation to test its relay behavior.
	withBackgroundBG1(t, s, f)
	const acknowledgement = "`conversation_send` 中继只需确认时，在本对话写一行正文，不调用 `conversation_send` 回传"
	const relayHeader = "只是确认收到就不要回"
	const silent = "supervision_finish visibility=\"silent\""
	for name, id := range map[string]string{"main": "conv1", "background": "bg1"} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.prepare(ctx, "server"); err != nil {
				t.Fatal(err)
			}
			if md := f.prepared[len(f.prepared)-1]["claude_md"].(string); !strings.Contains(md, acknowledgement) {
				t.Fatal("shared assistant instructions lack the local-only relay acknowledgement rule")
			}

			relay := relayMsg{From: "sender", FromTitle: "Example conversation", Tool: "conversation_send", Text: "The report is ready for reference."}
			if err := s.deliverRelay(ctx, "server", id, relay); err != nil {
				t.Fatal(err)
			}
			send := f.sends[len(f.sends)-1]
			if send[holdPushesParam] == true || !strings.Contains(send["text"].(string), relayHeader) {
				t.Fatal("conversation relay lost its legacy header or was dispatched as supervision")
			}

			event := relayMsg{Tool: superviseTool, Text: "Synthetic supervision event."}
			if err := s.deliverRelay(ctx, "server", id, event); err != nil {
				t.Fatal(err)
			}
			send = f.sends[len(f.sends)-1]
			if send[holdPushesParam] != true || strings.Contains(send["text"].(string), relayHeader) ||
				!strings.Contains(send["supervision_instructions"].(string), silent) {
				t.Fatal("supervision event lost its silent disposition or gained the conversation relay header")
			}
		})
	}
}

func mustSupervisionInstructions(t *testing.T, s *Service, ctx context.Context, machine, id string) string {
	t.Helper()
	text, err := s.supervisionInstructions(ctx, machine, id)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

type supervisionUnavailableStore struct {
	Store
	todoStore
}

func (supervisionUnavailableStore) ListTodos(context.Context) ([]db.TodoRow, error) {
	return nil, errors.New("task ownership unavailable")
}

func TestSupervisionInstructionsUnknownOwnershipDoesNotDemote(t *testing.T) {
	f := newFakeHub().withMachine()
	s := newServiceOn(t, f, supervisionUnavailableStore{Store: &memStore{}})
	f.setSessions("server", proto.Session{SessionID: "side", Assistant: true})
	s.todos.mu.Lock()
	s.todos.loaded = false
	s.todos.mu.Unlock()
	p := map[string]any{"session_id": "side", "text": "New facts"}
	f.mu.Lock()
	before := len(f.sends)
	f.mu.Unlock()
	err := s.call(context.Background(), "server", DeviceAssistantSend, p, nil)
	if err == nil || !strings.Contains(err.Error(), "task ownership unavailable") {
		t.Fatalf("unknown ownership dispatched without supervisor policy: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != before {
		t.Fatal("an ownership lookup failure reached the node")
	}
}
