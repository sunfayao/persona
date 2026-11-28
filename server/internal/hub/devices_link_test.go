package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// A hub that just restarted shows the machines as the manager handed them over, until the manager reports; a call
// meanwhile waits for it instead of failing.
func TestTheHubShowsTheHandOverUntilTheManagerReports(t *testing.T) {
	h := newTestHub(t)
	st := newMeshStore()
	h.store = st
	h.cfg.Machines = []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}}
	box := newMeshNode("uid-box", "box", "s1")
	box.resident = true
	_ = st.PutSetting(context.Background(), devicesWarmKey, map[string]any{"saved_at": time.Now(), "machines": map[string]any{
		"box": map[string]any{"info": proto.NodeInfo{Hostname: "box", Version: "test"}, "sessions": []proto.Session{{SessionID: "s1", Title: "旧的", Status: proto.StatusIdle}}}}})
	h.startDevices(context.Background())
	m := h.machineByID("box")
	if s := m.snapshot(); !s.Online || s.Hostname != "box" || len(m.sessionList()) != 1 {
		t.Fatalf("before the manager: %+v %v", s, m.sessionList())
	}
	var got struct {
		Path string `json:"path"`
	}
	res := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res <- m.call(ctx, "fs.list", map[string]any{"path": "/"}, &got)
	}()
	select {
	case err := <-res:
		t.Fatalf("the call did not wait for the manager: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	srv := newHubServer(t, h)
	startTestManager(t, h, srv, map[string]func(context.Context) (net.Conn, error){"box": box.dialer(h)})
	select {
	case err := <-res:
		if err != nil || got.Path != "/home/user" {
			t.Fatalf("the call once the manager is there: %v %+v", err, got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the call never went")
	}
	st.mu.Lock()
	_, left := st.settings[devicesWarmKey]
	st.mu.Unlock()
	if left {
		t.Fatal("the hand-over was read, not taken")
	}
	// Stopping, the hub hands the next process what the manager reported (its own settings, not the manager's).
	waitFor(t, "the manager reports box", func() bool { s := m.snapshot(); return s.Online && len(m.sessionList()) == 1 })
	// With the archived rows its manager told apart (#6836): still found by id while the manager comes back.
	m.setArchived("assistant", []proto.Session{{MachineID: "box", SessionID: "old-archived", Archived: true}})
	h.saveDevicesWarm(context.Background())
	next := newTestHub(t)
	next.store = st
	next.cfg.Machines = h.cfg.Machines
	next.startDevices(context.Background())
	// One row in its list; sessionList, the manager's whole list, has the archived row too.
	if s := next.machineByID("box").snapshot(); !s.Online || s.Hostname != "box" || len(next.machineByID("box").keptSessions()) != 1 ||
		len(next.machineByID("box").sessionList()) != 2 {
		t.Fatalf("the next hub process: %+v", s)
	}
	if s, ok := next.machineByID("box").session("old-archived"); !ok || !s.Archived {
		t.Fatalf("the next hub process lost the archived rows: %v %+v", ok, s)
	}
	// It stops again before its manager reported: what it still shows from the hand-over is handed on.
	next.saveDevicesWarm(context.Background())
	third := newTestHub(t)
	third.store = st
	third.cfg.Machines = h.cfg.Machines
	third.startDevices(context.Background())
	if s := third.machineByID("box").snapshot(); !s.Online || len(third.machineByID("box").keptSessions()) != 1 {
		t.Fatalf("a second quick restart: %+v", s)
	}
	if _, ok := third.machineByID("box").session("old-archived"); !ok {
		t.Fatal("a second quick restart lost the archived rows")
	}
	// ... with the first hand-over's deadline, not a new one: restarts in a row do not keep it shown for ever.
	nm, tm := next.machineByID("box"), third.machineByID("box")
	nm.mu.Lock()
	first := nm.warmUntil
	nm.mu.Unlock()
	tm.mu.Lock()
	again := tm.warmUntil
	tm.mu.Unlock()
	if again.After(first) {
		t.Fatalf("the deadline moved on: %v, first %v", again, first)
	}
}

// persona_settings (the hub's own tool) blocks a Claude config directory on a machine through the manager directly
// without an approval card, and removes it the same way.
func TestSelfSettingsBlocksClaudeDirsThroughTheManager(t *testing.T) {
	box := newMeshNode("uid-box", "box", "s1")
	box.resident = true
	mh := startTestHub(t, hubA, "Dev", []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}}, []*meshNode{box})
	h := mh.h
	eventually(t, 10*time.Second, "box online", func() bool { m := h.machineByID("box"); return m != nil && m.isOnline() })
	m := h.machineByID("box")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run := func(args string) string {
		t.Helper()
		out, err := h.toolPersonaSettings(ctx, assistantwire.AssistantID, m, "s1", json.RawMessage(args), nil)
		if err != nil {
			out = "error: " + err.Error()
		}
		if pending := h.pendingInteractions(); len(pending) != 0 {
			t.Fatalf("unexpected approval card for %s: %+v", args, pending)
		}
		return out
	}
	if out := run(`{"action":"get","key":"agents.blocked_claude_dirs"}`); !strings.Contains(out, "/home/user/.automation/claude-a (listed") {
		t.Fatalf("get: %s", out)
	}
	if out := run(`{"action":"add","key":"agents.blocked_claude_dirs","value":{"machine":"box","pattern":"~/.automation/*"}}`); !strings.Contains(out, "Blocked ~/.automation/* on box.") {
		t.Fatalf("add: %s", out)
	}
	if got := m.claudeDirsState(ctx).Blocked; len(got) != 1 || got[0] != "~/.automation/*" {
		t.Fatalf("the manager keeps %v", got)
	}
	if out := run(`{"action":"remove","key":"agents.blocked_claude_dirs","id":"box:~/.automation/*"}`); !strings.Contains(out, "Unblocked ~/.automation/* on box.") {
		t.Fatalf("remove: %s", out)
	}
	if got := m.claudeDirsState(ctx).Blocked; len(got) != 0 {
		t.Fatalf("still blocked: %v", got)
	}
}

// The manager's coded errors come back as the hub's own; what may have reached the node never reads as offline.
func TestDeviceErrorsKeepWhatMayHaveBeenDone(t *testing.T) {
	for _, c := range []struct {
		name    string
		err     error
		outcome string
		offline bool
	}{
		{"the bus dropped with the call out", fmt.Errorf("assistant: %w", errBusDropped), sendUnknown, false},
		{"no bus", fmt.Errorf("assistant: %w", errBusOffline), "", true},
		{"its node's connection dropped", &rpc.Error{Code: devicewire.CodeNodeClosed, Message: "dropped"}, sendUnknown, false},
		{"the manager's time ran out", &rpc.Error{Code: devicewire.CodeDeadline, Message: "deadline"}, sendUnknown, false},
		{"the machine offline", &rpc.Error{Code: devicewire.CodeMachineOffline, Message: "offline"}, "", true},
		{"the node's refusal", &rpc.Error{Code: "input_draft", Message: "draft"}, "", false},
	} {
		err := deviceError("box", c.err)
		if got := sendOutcome(err); got != c.outcome || errors.Is(err, errOffline) != c.offline {
			t.Errorf("%s: %v reads as outcome %q offline %v", c.name, err, got, errors.Is(err, errOffline))
		}
	}
	if !errors.Is(deviceError("box", fmt.Errorf("assistant: %w", errBusOffline)), errManagerOffline) {
		t.Error("no bus is the manager offline")
	}
	// The bus dropping with the call out: the app is told the manager is gone (503 assistant_offline), a send that
	// was out is unknown (above).
	if code, b := callErrBytes("", deviceError("box", fmt.Errorf("assistant: %w", errBusDropped))); code != http.StatusServiceUnavailable ||
		!strings.Contains(string(b), "assistant_offline") {
		t.Errorf("a read cut short by the manager going: %d %s", code, b)
	}
	for left, want := range map[time.Duration]time.Duration{30 * time.Second: 29500 * time.Millisecond, time.Second: 900 * time.Millisecond,
		0: time.Millisecond} {
		if got := managerTimeout(left); got != want {
			t.Errorf("managerTimeout(%v) = %v, want %v", left, got, want)
		}
	}
}

// A machine the manager shows as before its restart (warm) is online for the apps, but not connected: the hub's
// self-update waits for its node to be back, and only then has the nodes updated.
func TestAWarmMachineIsNotConnectedYet(t *testing.T) {
	h := newTestHub(t)
	report := func(s devicewire.State) {
		s.Machine.ID, s.Machine.Name, s.Machine.Kind = "box", "Box", "ssh"
		h.applyDeviceStates(devicewire.States{Machines: []devicewire.State{s}})
	}
	report(devicewire.State{Machine: proto.Machine{Online: true}, WarmUntil: time.Now().Add(time.Minute)})
	m := h.machineByID("box")
	if !m.snapshot().Online {
		t.Fatal("the apps see a warm machine as before the restart")
	}
	if got := h.upd.offline([]string{"box"}); len(got) != 1 {
		t.Fatalf("a warm machine counts as back: %v", got)
	}
	report(devicewire.State{Machine: proto.Machine{Online: true}, Connected: true, ConnectedAt: time.Now()})
	if got := h.upd.offline([]string{"box"}); len(got) != 0 || !m.snapshot().Online {
		t.Fatalf("its node back: offline %v", got)
	}
	report(devicewire.State{Machine: proto.Machine{Online: false}})
	if m.snapshot().Online || len(h.upd.offline([]string{"box"})) != 1 {
		t.Fatal("its node gone")
	}
}

// A machine nobody is known to carry (the ownership setting could not be read: unknownMachineOwner) is offline at once,
// hub start or not: there is no manager to wait for (#6795).
func TestNoWaitForAMachineNobodyIsKnownToCarry(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "own", Name: "Own", Kind: "local", OwnerRequired: true}}
	h.startDevices(context.Background()) // the start's window is open, nobody has reported
	m := h.machineByID("own")
	if o := m.ownerOrCarrier(); o != unknownMachineOwner {
		t.Fatalf("own's manager: %q", o)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	began := time.Now()
	if err := m.call(ctx, "fs.list", map[string]any{"path": "/"}, nil); !errors.Is(err, errManagerOffline) || time.Since(began) > time.Second {
		t.Fatalf("a call to a machine nobody carries: %v after %v", err, time.Since(began))
	}
	if h.devicesUpFor(unknownMachineOwner) {
		t.Fatal("nobody is up")
	}
}

// The end of a manager's connection forgets its up state only when no newer connection is there: one that connected
// and reported before the old end was handled keeps it (#6795).
func TestAGoneConnectionDoesNotForgetTheNewOnesUp(t *testing.T) {
	h := newTestHub(t)
	const b = "m-0b0b0b"
	h.devicesConnected(b)
	h.applyDeviceStatesFrom(b, devicewire.States{})
	d := &h.devices
	d.mu.Lock()
	conns := d.conns[b]
	d.mu.Unlock()
	// The next connection's handshake and report land before the old one's end is handled.
	h.devicesConnected(b)
	h.applyDeviceStatesFrom(b, devicewire.States{})
	h.upGone(b, conns)
	if !h.devicesUpFor(b) {
		t.Fatal("the new connection's up state went with the old connection")
	}
	// Nothing newer: the end forgets it, and a call waits for a new connection (the start's window) or fails.
	d.mu.Lock()
	conns = d.conns[b]
	d.mu.Unlock()
	h.upGone(b, conns)
	if h.devicesUpFor(b) {
		t.Fatal("the up state outlived its connection")
	}
}

// A manager that is new is asked to watch again what the apps watch, a few at a time: a computer slow to answer does
// not hold up the others' items.
func TestWatchedAgainAtOnceWithANewManager(t *testing.T) {
	box, lap := newMeshNode("uid-box", "box", "s1"), newMeshNode("uid-lap", "lap", "s2")
	box.resident, lap.resident = true, true
	mh := startTestHub(t, hubA, "Dev", []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}, {ID: "lap", Name: "Lap", Kind: "ssh"}},
		[]*meshNode{box, lap})
	tok, _ := mh.deviceToken(t, roleFull)
	eventually(t, 10*time.Second, "both online", func() bool { ms := mh.machines(t, tok); return ms["box"].Online && ms["lap"].Online })
	a := dialApp(t, mh, tok)
	a.send(t, map[string]any{"type": "subscribe", "machine_id": "box", "session_id": "s1"})
	a.wait(t, "box's backlog", 0, hasItem("hi from s1"))
	a.send(t, map[string]any{"type": "subscribe", "machine_id": "lap", "session_id": "s2"})
	a.wait(t, "lap's backlog", 0, hasItem("hi from s2"))
	slow := make(chan struct{})
	box.mu.Lock()
	box.hold = map[string]chan struct{}{"session.watch": slow}
	box.mu.Unlock()
	t.Cleanup(func() { close(slow) })
	watched := func() int { lap.mu.Lock(); defer lap.mu.Unlock(); return lap.watched["s2"] }
	before := watched()
	mh.mgr.stop()
	mh.mgr = startTestManager(t, mh.h, mh.srv, map[string]func(context.Context) (net.Conn, error){"box": box.dialer(mh.h), "lap": lap.dialer(mh.h)})
	eventually(t, 5*time.Second, "lap watched again while box is slow to answer", func() bool { return watched() > before })
	lap.addItem("s2", "after the new manager")
	a.wait(t, "lap's live item", 0, hasItem("after the new manager"))
}
