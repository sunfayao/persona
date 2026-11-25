package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)



func shortHistoryTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := historyTimeout
	historyTimeout = d
	t.Cleanup(func() { historyTimeout = old })
}

func permission(id string) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: "s1", Kind: proto.IxPermission,
		ExpiresAt: time.Now().Add(time.Hour)}
}

// newFleetTestOn is newFleetTest on the store st.
func newFleetTestOn(t *testing.T, st Store) *fleetTest {
	t.Helper()
	f := newFakeHub()
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: f, Store: func() Store { return st }, Logf: t.Logf, Devices: d})
	runCtx, cancel := context.WithCancel(context.Background())
	s.running.Store(&runCtx)
	ft := &fleetTest{t: t, s: s, d: d, f: f, cancel: cancel}
	t.Cleanup(func() { drainFleetBackground(t, ft.s, cancel) })
	return ft
}
