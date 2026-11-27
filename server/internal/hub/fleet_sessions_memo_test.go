package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/proto"
)

// memoHub is a hub with one machine and apps of each kind: two older ones and three delta ones, a fourth delta app
// connecting later.
type memoHub struct {
	h    *Hub
	m    *Machine
	apps []*client
}

func newMemoHub(t *testing.T) *memoHub {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = append(h.machines, m), m
	x := &memoHub{h: h, m: m}
	for i := range 5 {
		x.connect(i >= 2)
	}
	return x
}

func (x *memoHub) connect(delta bool) *client {
	c := &client{out: make(chan []byte, 64), subs: map[[2]string]bool{}, role: roleFull}
	if delta {
		c.delta, c.have = true, map[string]map[string][]byte{}
	}
	x.h.mu.Lock()
	x.h.clients[c] = struct{}{}
	x.h.mu.Unlock()
	x.apps = append(x.apps, c)
	return c
}

// got is what each app was sent since the last call.
func (x *memoHub) got() [][]string {
	out := make([][]string, len(x.apps))
	for i, c := range x.apps {
		for len(c.out) > 0 {
			out[i] = append(out[i], string(<-c.out))
		}
	}
	return out
}

// setOwner makes manager the one that carries the machine.
func (x *memoHub) setOwner(manager string) {
	d := &x.h.devices
	d.mu.Lock()
	if d.kept == nil {
		d.kept = map[string]string{}
	}
	d.kept["box"] = manager
	d.mu.Unlock()
}

// A manager's list with one session of 150 changed costs the hub that one session: the list is decoded by changed
// rows (fleet_link.go) and the other sessions' rows are pushed again as the same slices (encodeKept). The oracle test
// above sees only the bytes, which are the same either way. 38 allocations measured for the hop with five delta apps
// and two older ones (43 while jsontext's decoder split the list), 571 with the list decoded whole; encoding every row
// again is 39, told apart by the rows' identity.
func TestFleetSessionsOneChangedReusesRows(t *testing.T) {
	if !isolatedSessionAllocation(t) {
		return
	}
	h := newTestHub(t)
	send := oneChangedHop(t, h)
	before := h.sessionsSent["box"]
	send(1) // s000 changed
	after := h.sessionsSent["box"]
	if len(after) != len(before) || len(after) != 150 {
		t.Fatalf("pushed %d rows, then %d", len(before), len(after))
	}
	for i := range after {
		if kept := &after[i].raw[0] == &before[i].raw[0]; kept != (i > 0) {
			t.Fatalf("row %d (%s) pushed as the same slice: %v, want %v", i, after[i].id, kept, i > 0)
		}
	}
	n := 1
	if allocs := testing.AllocsPerRun(20, func() { n++; send(n) }); allocs > 40 && !raceEnabled {
		t.Fatalf("a list with one session changed allocates %v times", allocs)
	}
}

// raceEnabled: built with -race, whose instrumentation allocates on its own (race_test.go).
var raceEnabled bool

// The whole list is put together and projected once per push for each kind of older app: one that takes the
// canonical contracts gets it as it is, the others as compat.ProjectJSON projects it for them, as queue did per app.
func TestWholeListProjectedOncePerKind(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = append(h.machines, m), m
	m.sessions = []proto.Session{{MachineID: "box", SessionID: "s1", Work: &proto.WorkState{Link: &proto.WorkLink{URL: "persona://pair"}}}}
	var apps []*client
	for _, canonical := range []bool{false, true, false, true} {
		c := &client{out: make(chan []byte, 4), subs: map[[2]string]bool{}, role: roleFull}
		c.canonicalContracts.Store(canonical)
		addTestClient(h, c)
		apps = append(apps, c)
	}
	h.sessionsChanged(m)
	full := sessionsMessage("box", encodeStamped(m.sessions, h.machineOwner("box")))
	if string(compat.ProjectJSON(full, false)) == string(full) {
		t.Fatal("the list is not one an older app gets projected")
	}
	for i, c := range apps {
		want := compat.ProjectJSON(full, c.canonical())
		if got := <-c.out; string(got) != string(want) {
			t.Fatalf("app %d:\n got %s\nwant %s", i, got, want)
		}
	}
}

// A row records once whether compat.ProjectJSON changes it, and the whole list goes to older apps unprojected when no
// row would change: the list's own members are never projected, so that is exactly when projecting the list changes
// nothing. One row of each kind ProjectJSON renames, and rows that only look like them.
func TestWholeListProjectedByRows(t *testing.T) {
	plain := `{"session_id":"s0","started_by_persona":true,"title":"persona-visor","last_text":"see persona://x"}`
	for _, tc := range []struct {
		name, row string
		projects  bool
	}{
		{"persona- in a name it renames", `{"session_id":"s1","persona_id":"persona-blob"}`, true},
		{"tool_name", `{"session_id":"s1","interaction":{"tool_name":"ask_persona"}}`, true},
		{"look", `{"session_id":"s1","look":"persona-visor"}`, true},
		{"a look's id", `{"session_id":"s1","subagents":[{"id":"persona-pixel","type":"look"}]}`, true},
		{"persona:// link", `{"session_id":"s1","work":{"link":{"url":"persona://pair?code=1"}}}`, true},
		{"context tag in text", `{"session_id":"s1","text":"<persona-context id=\"a\">x</persona-context>"}`, true},
		{"target_kind", `{"session_id":"s1","target_kind":"persona"}`, true},
		{"persona- in other text", `{"session_id":"s1","text":"persona-tool","tool_name":"Bash"}`, false},
		{"a look's name elsewhere", `{"session_id":"s1","subagents":[{"id":"persona-pixel","type":"agent"}]}`, false},
		{"no persona at all", `{"session_id":"s1","title":"build"}`, false},
	} {
		list := []sentSession{{id: "s0", raw: json.RawMessage(plain)}, {id: "s1", raw: json.RawMessage(tc.row)}}
		for i := range list {
			list[i].asIs = !compat.Projects(list[i].raw)
		}
		for _, machine := range []string{"box", "persona-box"} {
			full := sessionsMessage(machine, list)
			projected := compat.ProjectJSON(full, false)
			if changed := !bytes.Equal(projected, full); changed != !allAsIs(list) || changed != tc.projects {
				t.Errorf("%s on %s: the list projected %v, its rows %v, want %v\n%s", tc.name, machine, changed, !allAsIs(list), tc.projects, projected)
			}
		}
	}
}

// Delta apps that have the same list share one map and one sessionsDelta per push, and each still gets what
// sessionsDelta gives for what it has itself: apps connect at different times, Codex history comes and goes (a live
// session turned history stays with the apps that had it, so their maps differ), sessions change and go.
func TestDeltaAppsShareOneComputation(t *testing.T) {
	x := newMemoHub(t)
	x.apps = nil
	x.h.clients = map[*client]struct{}{}
	rng := rand.New(rand.NewPCG(3, 5))
	shadow := map[*client]map[string][]byte{} // what each app has, by sessionsDelta alone
	list := func() []proto.Session {
		var out []proto.Session
		for i := range 8 {
			if rng.IntN(6) == 0 {
				continue
			}
			s := proto.Session{MachineID: "box", SessionID: fmt.Sprintf("s%d", i), Title: fmt.Sprint(rng.IntN(2))}
			if rng.IntN(3) == 0 {
				s.SourceKind = "codex_history"
			}
			out = append(out, s)
		}
		return out
	}
	shared := false
	for step := range 200 {
		if rng.IntN(5) == 0 && len(x.apps) < 6 {
			c := x.connect(true)
			x.h.snapshotSessions(c, x.m)
			shadow[c] = maps.Clone(c.have["box"])
			<-c.out
			continue
		}
		x.h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "box", Sessions: list()})
		enc := x.h.sessionsSent["box"]
		for i, c := range x.apps {
			want, next := sessionsDelta("box", enc, shadow[c])
			shadow[c] = next
			var got []byte
			if len(c.out) > 0 {
				got = <-c.out
			}
			if string(got) != string(want) || !maps.EqualFunc(c.have["box"], next, bytes.Equal) {
				t.Fatalf("step %d, app %d:\n got %s\nwant %s", step, i, got, want)
			}
			for _, o := range x.apps[:i] {
				shared = shared || mapIdentity(o.have["box"]) == mapIdentity(c.have["box"]) && len(c.have["box"]) > 0
			}
		}
	}
	if !shared {
		t.Fatal("no two apps ever shared a map")
	}
}
