package assistant

import (
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/devices"
	"github.com/sunfayao/persona/server/internal/proto"
)

// fleetSessionsToldLimit bounds what telling a changed list of 200 sessions allocates when a GC has just emptied
// json's pools of encoder buffers, which in a busy manager is every few lists: 708,868 bytes measured (the decorated
// copy of the list, its encoding and the bus's line), plus 3%. Told as json.RawMessage it was 1,224,668: the bus's
// encoder checked the 246 KB of the list and copied them twice more into buffers it grew. With warm pools the two
// allocate the same (662,643 and 663,212), so the bound is taken cold.
const fleetSessionsToldLimit = 730000

// Telling a changed list writes it into the bus's line once.
func TestFleetSessionsToldAllocatesTheLineOnce(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	if !isolated(t) {
		return
	}
	s, d, h, lists := toldFleet(t, 200)
	if per := toldAllocation(s, d, lists); per > fleetSessionsToldLimit {
		t.Errorf("telling a changed list of 200 sessions allocated %d bytes, want at most %d", per, fleetSessionsToldLimit)
	}
	if h.toldOf(assistantwire.EventFleetSessions) == 0 {
		t.Fatal("no list was told")
	}
}

// The same measurement and limit reject the list told as json.RawMessage, as it was.
func TestFleetSessionsToldLimitRejectsRawParams(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	if !isolated(t) {
		return
	}
	s, d, h, lists := toldFleet(t, 200)
	h.raw = true
	if per := toldAllocation(s, d, lists); per <= fleetSessionsToldLimit {
		t.Errorf("told as json.RawMessage a changed list allocated %d bytes, want more than %d", per, fleetSessionsToldLimit)
	}
}

// toldAllocation is the bytes one changed list allocates on its way into the bus's line, each one after two
// collections (they empty json's pools of encoder buffers, both generations) and with none during it; on one
// processor, so nothing else of the process is counted in between.
func toldAllocation(s *Service, d *fleetDevs, lists [2][]proto.Session) uint64 {
	const runs = 30
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	publish := func(n int) {
		d.setSessions("mac", lists[n&1])
		s.publishSessions("mac")
	}
	for n := range 4 {
		publish(n)
	}
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	var total uint64
	for n := range runs {
		runtime.GC()
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		publish(n)
		runtime.ReadMemStats(&after)
		total += after.TotalAlloc - before.TotalAlloc
	}
	return total / runs
}

// summaryIn is the entry of list with the id canon: how a fake's Summary answers from its List.
func summaryIn(list []devices.Summary, canon string) (devices.Summary, bool) {
	for _, s := range list {
		if s.ID == canon {
			return s, true
		}
	}
	return devices.Summary{}, false
}

// fleetMachineByList is fleetMachine as it was: every machine built to pick the one with the id's canonical form.
func fleetMachineByList(s *Service, id string) (fleetMachine, bool) {
	if s.devs == nil || id == "" {
		return fleetMachine{}, false
	}
	canon := s.devs.Canonical(id)
	for _, m := range s.fleetMachines() {
		if m.ID == canon {
			return m, true
		}
	}
	return fleetMachine{}, false
}

// aliasDevs are devices where an id another machine absorbed names that machine, as the real ones have it.
type aliasDevs struct {
	*fleetDevs
	alias map[string]string
}

func (d *aliasDevs) Canonical(id string) string {
	if to, ok := d.alias[id]; ok {
		return to
	}
	return id
}

func (d *aliasDevs) Summary(id string) (devices.Summary, bool) {
	return summaryIn(d.List(), d.Canonical(id))
}

// fleetMachine asks the devices for one machine and gets what the whole list gave: for a machine, an id another one
// absorbed, an id no machine has, the empty id, and with no devices at all.
func TestFleetMachineIsTheListedOne(t *testing.T) {
	d := &aliasDevs{fleetDevs: newFleetDevs(), alias: map[string]string{"box-link": "box", "gone-link": "gone"}}
	d.add("mac", "Mac")
	d.add("box", "").home = "/home/box"
	s := New(Options{Hub: newFakeHub(), Logf: testLogf(t), Devices: d})
	for _, id := range []string{"mac", "box", "box-link", "gone-link", "nobody", ""} {
		got, ok := s.fleetMachine(id)
		want, wok := fleetMachineByList(s, id)
		if got != want || ok != wok {
			t.Errorf("fleetMachine(%q) = %+v, %v; through the list %+v, %v", id, got, ok, want, wok)
		}
	}
	if got, ok := s.fleetMachine("box-link"); !ok || got.ID != "box" || got.Home != "/home/box" || got.manager != s.id {
		t.Fatalf("the absorbed id's machine = %+v, %v", got, ok)
	}
	if got := s.machineName("box-link"); got != "box-link" { // no name: the id asked by, as before
		t.Fatalf("machineName = %q", got)
	}
	none := New(Options{Hub: newFakeHub(), Logf: testLogf(t)})
	if got, ok := none.fleetMachine("mac"); ok || got != (fleetMachine{}) {
		t.Fatalf("with no devices: %+v, %v", got, ok)
	}
}
