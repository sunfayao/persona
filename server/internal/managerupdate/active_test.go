package managerupdate

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func pendingFixture(t *testing.T, phase string) (*Controller, Plan) {
	t.Helper()
	c, p := fixture(t)
	prepare(t, p)
	if err := copyExclusive(p.Binary, filepath.Join(txDir(p), "previous")); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(activePath(p), p); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(txDir(p), "receipt.json"), receipt(p, phase, nil)); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquire(filepath.Join(p.Dir, ".manager-update.lock"))
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	return c, p
}

func TestStartRejectsForeignNonterminalTransaction(t *testing.T) {
	for _, phase := range []string{"checked", "applied", "unknown"} {
		t.Run(phase, func(t *testing.T) {
			c, a := pendingFixture(t, phase)
			b := a
			b.RunID = "second-run"
			before := tree(t, c.Home)
			if err := c.Start(context.Background(), b); err == nil {
				t.Fatal("foreign pending transaction accepted")
			}
			if !reflect.DeepEqual(before, tree(t, c.Home)) {
				t.Fatal("rejected start changed transaction or service state")
			}
		})
	}
}

func TestOldTerminalGuardianLeavesForeignMarker(t *testing.T) {
	for _, phase := range []string{"confirmed", "rollback"} {
		t.Run(phase, func(t *testing.T) {
			c, a := pendingFixture(t, phase)
			b := a
			b.RunID = "second-run"
			if err := writeJSON(activePath(b), b); err != nil {
				t.Fatal(err)
			}
			before := tree(t, c.Home)
			r, err := c.Run(context.Background(), a)
			if err != nil || r.Phase != phase {
				t.Fatalf("phase=%s err=%v", r.Phase, err)
			}
			if !reflect.DeepEqual(before, tree(t, c.Home)) {
				t.Fatal("terminal guardian changed foreign state")
			}
		})
	}
}
