package assistant

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/devices"
)

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
