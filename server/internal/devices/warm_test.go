package devices

import (
	"errors"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Shown says whether a machine's list is its node's, and until when it is shown as before while its node is away
// (the assistant's direction sessions read it, #11458): no list yet on a connection is not an empty list, and the
// reconnect grace says when it ends.
func TestShownListedAndUntil(t *testing.T) {
	h := newTestDevices(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	m.reconnectGrace = time.Hour // this machine's own; the test ends long before it
	h.addEntry(m)
	m.mu.Lock()
	m.online = true
	m.mu.Unlock()
	if s, ok := h.Shown("box"); !ok || s.Listed || !s.Until.IsZero() {
		t.Fatalf("connected with no list yet: %+v, %v", s, ok)
	}
	m.mu.Lock()
	m.sessions, m.sessionsListed = []proto.Session{}, true
	m.mu.Unlock()
	if s, _ := h.Shown("box"); !s.Listed || !s.Until.IsZero() {
		t.Fatalf("an empty list given: %+v", s)
	}
	m.detach(errors.New("node exited"), false)
	if s, _ := h.Shown("box"); !s.Listed || !s.Until.After(time.Now()) || !m.snapshot().Online {
		t.Fatalf("within the reconnect grace: %+v", s)
	}
	if _, ok := h.Shown("nobody"); ok {
		t.Fatal("a machine there is none of")
	}
}
