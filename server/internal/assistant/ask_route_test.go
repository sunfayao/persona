package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// assistantHome is where the node's sessions are the assistant's conversations.
const assistantHome = "/home/u/.persona/assistant"

// routeEnv is a manager with one task, "修登录崩溃", handed out by leader conversation lead1 (unless leaders is false: then
// the main conversation conv1 supervises it and no leader is configured), its agents s1 and s2 on mac.
type routeEnv struct {
	*fleetTest
	todo   Todo
	agents []proto.Session // mac's sessions
}

// raise asks ix as a hook would; the channel has its answer once it has one.
func (e *routeEnv) raise(ix proto.Interaction) <-chan proto.Resolution {
	out := make(chan proto.Resolution, 1)
	go func() {
		res, _ := e.s.waitInteraction(context.Background(), ix)
		out <- res
	}()
	e.waitPendingID(ix.ID)
	return out
}

func (e *routeEnv) waitPendingID(id string) {
	e.t.Helper()
	waitFor(e.t, "card "+id, func() bool { _, ok := e.s.pendingIx(id); return ok })
}

// pushesOf counts the pushes of the cards ids.
func (e *routeEnv) pushesOf(ids ...string) int {
	n := 0
	for _, p := range e.f.pushed() {
		for _, id := range ids {
			if p.Interaction != nil && p.Interaction.ID == id {
				n++
			}
		}
	}
	return n
}

// shown is the card id as the apps were told of it last, and whether they were told of it at all: a waitFor check
// asks this one, as a card not shown yet is "not yet" there, not a failure (#4407).
func (e *routeEnv) shown(id string) (proto.Interaction, bool) {
	df := &delegateFleet{fleetTest: e.fleetTest}
	list := df.shownAs(id)
	if len(list) == 0 {
		return proto.Interaction{}, false
	}
	return list[len(list)-1], true
}

// lastShown is the card id as the apps were told of it last, for a card that must have been shown by now.
func (e *routeEnv) lastShown(id string) proto.Interaction {
	e.t.Helper()
	ix, ok := e.shown(id)
	if !ok {
		e.t.Fatalf("card %s was never shown", id)
	}
	return ix
}

// noticeWith waits for a message into conversation to that says want, and answers it.
func (e *routeEnv) noticeWith(to, want string) string {
	e.t.Helper()
	var got string
	waitFor(e.t, to+" told "+want, func() bool {
		for _, n := range e.f.noticesTo(to) {
			if strings.Contains(n, want) {
				got = n
				return true
			}
		}
		return false
	})
	return got
}

// settleService waits until s's supervision messages due have gone out. Taking the digest lock for each observation
// covers restoration as well as delivery and closes the window where a finishing delivery arms its retry.
func settleService(t *testing.T, s *Service) {
	t.Helper()
	b := &s.supervise
	waitFor(t, "the supervision messages due to go out", func() bool {
		b.digestMu.Lock()
		defer b.digestMu.Unlock()
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.pending) == 0 && b.timer == nil
	})
}

// settle is settleService for e's own manager.
func (e *routeEnv) settle() {
	e.t.Helper()
	settleService(e.t, e.s)
}

// supervisorState says what the apps see the two assistant conversations in: the main conversation and the leader in
// their statuses (working while on a turn).
func (e *routeEnv) supervisorState(main, lead string) {
	convs := []proto.Session{
		{SessionID: "conv1", MachineID: "server", Cwd: assistantHome, Assistant: true, Status: main},
		{SessionID: "lead1", MachineID: "server", Cwd: assistantHome, Assistant: true, Status: lead}}
	e.sessions("server", convs...)
	e.f.setSessions("server", convs...)
}
