package assistant

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

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
