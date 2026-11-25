package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

var startedConvRe = regexp.MustCompile(`Started conversation (\S+);`)

// spawnHarness is an assistant with conversation parent-1 on its machine, the hub's log kept.
type spawnHarness struct {
	s      *Service
	f      *fakeHub
	mu     sync.Mutex
	logs   []string
	parent proto.Session
}

func newSpawnHarness(t *testing.T) *spawnHarness {
	t.Helper()
	return newSpawnHarnessOn(t, &memStore{})
}

// newSpawnHarnessOn is newSpawnHarness with st as its database.
func newSpawnHarnessOn(t *testing.T, st Store) *spawnHarness {
	t.Helper()
	h := &spawnHarness{f: newFakeHub().withMachine()}
	h.f.settings = &Settings{MachineID: "server", Timezone: "Asia/Shanghai"}
	h.s = withSettings(t, h.f, New(Options{Hub: h.f, Store: func() Store { return st }, Logf: func(format string, args ...any) {
		h.mu.Lock()
		h.logs = append(h.logs, fmt.Sprintf(format, args...))
		h.mu.Unlock()
	}}))
	h.parent = proto.Session{SessionID: "parent-1", MachineID: "server", Assistant: true, Status: proto.StatusIdle}
	h.f.known["parent-1"] = true
	h.f.setSessions("server", h.parent)
	return h
}

func (h *spawnHarness) log() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.logs, "\n")
}

// spawn opens a conversation from parent-1 with conversation_new; its id.
func (h *spawnHarness) spawn(t *testing.T) string {
	t.Helper()
	res := h.s.Tool(context.Background(), ToolCall{MachineID: "server", SessionID: "parent-1", Name: "conversation_new",
		Arguments: json.RawMessage(`{"text":"去查一下竞品的定价"}`)})
	if res.IsError || len(res.Content) == 0 {
		t.Fatalf("conversation_new: %+v", res)
	}
	m := startedConvRe.FindStringSubmatch(res.Content[0].Text)
	if m == nil {
		t.Fatalf("no id in %q", res.Content[0].Text)
	}
	return m[1]
}

// ended lists child as its turn ended and has the assistant see the turn settle.
func (h *spawnHarness) ended(child proto.Session) {
	h.f.setSessions("server", h.parent, child)
	h.s.assistantTurnSettled(context.Background(), "server", child.SessionID)
}

func (h *spawnHarness) noticesToParent() []string {
	var out []string
	for _, n := range h.f.noticesTo("parent-1") {
		if strings.Contains(n, "你用 conversation_new 开的对话") {
			out = append(out, n)
		}
	}
	return out
}

// #4669: an assistant turn that failed (the node's turn error, or an answer that is only Claude Code's auth error line)
// is pushed as a failure (⚠️), however quick it was, as other failures are.
func TestFailedAssistantTurnPushedAsFailure(t *testing.T) {
	for _, c := range []struct{ name, turnError, lastText string }{
		{"the node's turn error", "Invalid API key · Fix external API key", ""},
		{"an answer that is only the error", "", "Invalid API key · Fix external API key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ft := newFleetTestNoStore(t)
			fastDone(ft)
			work := proto.Session{SessionID: "a1", Title: "竞品定价", Cwd: "/home/me/.persona/assistant", Assistant: true,
				Status: proto.StatusWorking, UpdatedAt: time.Now()}
			ft.sessions("mac", work)
			done := work
			done.Status, done.TurnError, done.LastText, done.UpdatedAt = proto.StatusDone, c.turnError, c.lastText, time.Now()
			ft.sessions("mac", done)
			got := ft.pushesOf(assistantwire.NotifyDone, 1, 2*time.Second)
			if len(got) != 1 || !strings.HasPrefix(got[0].Title, "⚠️") || !strings.Contains(got[0].Body, "Invalid API key") || got[0].LongMS != 0 {
				t.Fatalf("pushes %+v", got)
			}
		})
	}
}
