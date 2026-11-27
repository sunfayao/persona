package hub

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/update"
)

// Behind the edge an update does not exec: once idle, the new binary is in place and the edge is asked to switch to
// it, as the build the manifest says; no watchdog is started (the edge is the watchdog).
func TestUpdateHandsOverToTheEdge(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execs := updatingHub(t, p, "release")
	type call struct {
		target string
		build  int64
	}
	calls := make(chan call, 1)
	h.upd.handover = func(_ context.Context, target string, build int64) error {
		calls <- call{target, build}
		return nil
	}
	h.upd.startWatch = func(string, string) error { t.Error("a watchdog was started behind the edge"); return nil }
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(ctx, false); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-calls:
		if c.target != target || c.build != 202611021700 {
			t.Fatalf("handed over to %s build %d", c.target, c.build)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("never handed over")
	}
	if len(execs()) != 0 {
		t.Fatalf("exec'd %v behind the edge", execs())
	}
	if b, _ := os.ReadFile(target); !bytes.Equal(b, p.script) {
		t.Fatal("the new binary is not in place for the edge to start")
	}
	if ok, _ := update.Load(update.PendingPath(h.upd.dir), &update.Pending{}); !ok {
		t.Fatal("no pending marker for the new slot to confirm")
	}
}

// A confirmed update tells the edge, whose own 3-minute check of it ends.
func TestUpdateConfirmTellsTheEdge(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, _ := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	told := 0
	h.upd.edgeConfirmed = func() { told++ }
	pending := update.Pending{FromBuild: 202611021500, ToBuild: 202611021700, Target: target, Prev: target + ".prev", Online: []string{"box"}}
	update.Save(update.PendingPath(h.upd.dir), pending)
	h.upd.confirm(context.Background(), pending)
	if told != 1 {
		t.Fatalf("the edge was told %d times", told)
	}
}
