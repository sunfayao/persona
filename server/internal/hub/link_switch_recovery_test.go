package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/devicewire"
)

// What a switch survives: an end racing its begin, a manager that stopped waiting, a hub that stopped mid-begin, a
// stale or foreign end, a move or a removal at the same time, and a manager that never ends it.

// switchSteps is operation op's steps in computer mid's audit, oldest first.
func switchSteps(t *testing.T, database *db.DB, mid, op string) []string {
	t.Helper()
	rows, err := database.LinkSwitchHistory(context.Background(), mid, op)
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	for _, r := range rows {
		steps = append(steps, r.Step)
	}
	return steps
}

func linkInUse(t *testing.T, h *Hub, mid string) bool {
	t.Helper()
	used, err := h.store.MachineLinkInUse(context.Background(), mid)
	if err != nil {
		t.Fatal(err)
	}
	return used
}

// countingStore counts a begin's checks (MachineLinkInUse).
type countingStore struct {
	*db.DB
	checks int
}

func (s *countingStore) MachineLinkInUse(ctx context.Context, machine string) (bool, error) {
	s.checks++
	return s.DB.MachineLinkInUse(ctx, machine)
}

// A begin whose manager stopped waiting while it waited for its turn changes nothing, and asks nothing more.
func TestLinkSwitchBeginAbandonedByItsManager(t *testing.T) {
	h, database, _ := switchHub(t)
	counted := &countingStore{DB: database}
	h.store = counted
	op := newSwitchOp()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(devicewire.LinkSwitchBegin{MachineID: "lsw-box", Operation: op, SSHHost: "me@box.example"})
	if _, err := busMethods[devicewire.MethodLinkSwitchBegin](h, ctx, nil, raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("begin: %v", err)
	}
	checks := counted.checks
	if checks != 0 || diskKind(t, h, "lsw-box") != "ssh" || linkInUse(t, h, "lsw-box") || len(switchSteps(t, database, "lsw-box", op)) != 0 ||
		h.linkSwitch.live("lsw-box", time.Now()) {
		t.Fatalf("an abandoned begin went on (%d checks)", checks)
	}
}

// An end from a manager that does not carry the computer (it moved): only the manager that began the switch, naming
// its code, revokes that code's credential (nothing else, the kind stays the new manager's), told rolled_back_gone;
// any other is refused as no_machine and changes nothing.
func TestLinkSwitchEndFromAnotherManager(t *testing.T) {
	h, database, _ := switchHub(t)
	ctx := context.Background()
	op := newSwitchOp()
	got, err := busBegin(h, nil, devicewire.LinkSwitchBegin{MachineID: "lsw-box", Operation: op, SSHHost: "me@box.example"})
	if err != nil {
		t.Fatal(err)
	}
	code, out := redeem(t, h, pairTokenOf(t, got.Link), `{}`)
	if code != 200 {
		t.Fatalf("redeem %d", code)
	}
	cred := out["token"].(string)
	h.devices.mu.Lock()
	h.devices.assign = map[string]string{"lsw-box": "m-0e0e0e"} // moved meanwhile (a race; a move waits for the switch)
	h.devices.mu.Unlock()
	third := &busConn{ident: db.BusIdentity{ID: "m-0d0d0d"}}
	for _, c := range []struct {
		name string
		conn *busConn
		hash string
	}{{"a manager that did not begin it", third, got.PairHash}, {"without its hash", nil, ""}, {"another hash", nil, hashToken("jp_x")}} {
		refusedWith(t, c.name, busEnd(h, c.conn, devicewire.LinkSwitchEnd{MachineID: "lsw-box", Operation: op, PairHash: c.hash,
			Outcome: devicewire.LinkSwitchRolledBack}), devicewire.LinkSwitchCodeNoMachine)
		if _, ok := h.authenticate(ctx, cred); !ok || !h.linkSwitch.live("lsw-box", time.Now()) {
			t.Fatalf("%s: changed something", c.name)
		}
	}
	refusedWith(t, "its own, with its hash", busEnd(h, nil, devicewire.LinkSwitchEnd{MachineID: "lsw-box", Operation: op, PairHash: got.PairHash,
		Outcome: devicewire.LinkSwitchRolledBack}), devicewire.LinkSwitchCodeRolledBackGone)
	if _, ok := h.authenticate(ctx, cred); ok || h.linkSwitch.live("lsw-box", time.Now()) || diskKind(t, h, "lsw-box") != "link" {
		t.Fatal("its credential, its switch or its kind")
	}
	if steps := switchSteps(t, database, "lsw-box", op); !slices.Equal(steps, []string{"begin", "begun", "rolled_back"}) {
		t.Fatalf("audited %v", steps)
	}
}

// A move or a removal holds its computers from a begin while it runs (config_changed), and a switch holds them from
// a move or a removal, also once the hub restarted (the audit says it is under way).
func TestLinkSwitchAndMovesWaitForEachOther(t *testing.T) {
	h, _, _ := switchHub(t)
	ctx := context.Background()
	release, err := h.holdLinkSwitch(ctx, "lsw-box")
	if err != nil {
		t.Fatal(err)
	}
	op := newSwitchOp()
	_, err = busBegin(h, nil, devicewire.LinkSwitchBegin{MachineID: "lsw-box", Operation: op, SSHHost: "me@box.example"})
	refusedWith(t, "while a move holds it", err, devicewire.LinkSwitchCodeChanged)
	if diskKind(t, h, "lsw-box") != "ssh" || linkInUse(t, h, "lsw-box") {
		t.Fatal("a held computer began a switch")
	}
	release()
	if _, err := busBegin(h, nil, devicewire.LinkSwitchBegin{MachineID: "lsw-box", Operation: op, SSHHost: "me@box.example"}); err != nil {
		t.Fatal(err)
	}
	h.linkSwitch = linkSwitches{} // the hub restarted
	if _, err := h.moveMachine(ctx, "lsw-box", "m-0e0e0e"); !errors.Is(err, errLinkSwitchInProgress) {
		t.Fatalf("moved: %v", err)
	}
	if w := adminMachinesRequest(h, "DELETE", "/api/v1/admin/machines/lsw-box", ""); w.Code != http.StatusConflict {
		t.Fatalf("removed: %d", w.Code)
	}
	if _, err := h.holdManagerLinkSwitch(ctx, devicesOwner); !errors.Is(err, errLinkSwitchInProgress) {
		t.Fatalf("its manager removed: %v", err)
	}
}
