package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// stuckHistoryStore is a database whose interaction history never answers: every read and write waits for its
// context.
type stuckHistoryStore struct{ *fleetMem }

func (stuckHistoryStore) InsertInteraction(ctx context.Context, _ proto.Interaction) error {
	<-ctx.Done()
	return ctx.Err()
}

func (stuckHistoryStore) FinishInteraction(ctx context.Context, _, _ string, _ *proto.Resolution) error {
	<-ctx.Done()
	return ctx.Err()
}

func (stuckHistoryStore) InteractionOutcome(ctx context.Context, _ string) (string, *proto.Resolution, error) {
	<-ctx.Done()
	return "", nil, ctx.Err()
}

// stuckAlwaysStore is a database whose assistant_always table never answers.
type stuckAlwaysStore struct{ *fleetMem }

func (stuckAlwaysStore) AllowAlways(ctx context.Context, _, _ string, _ []string) error {
	<-ctx.Done()
	return ctx.Err()
}

func (stuckAlwaysStore) AlwaysAllowed(ctx context.Context, _ string, _ []string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

func shortHistoryTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := historyTimeout
	historyTimeout = d
	t.Cleanup(func() { historyTimeout = old })
}

// waitShown waits for the apps to be told about interaction id (through the hub).
func (ft *fleetTest) waitShown(id string, within time.Duration) {
	ft.t.Helper()
	deadline := time.Now().Add(within)
	for {
		for _, b := range ft.toldOf(assistantwire.EventFleetInteraction) {
			var p assistantwire.FleetInteraction
			if json.Unmarshal(b, &p) == nil && p.Interaction.ID == id {
				return
			}
		}
		if time.Now().After(deadline) {
			ft.t.Fatalf("the apps were not shown %s within %v", id, within)
		}
		time.Sleep(time.Millisecond)
	}
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
