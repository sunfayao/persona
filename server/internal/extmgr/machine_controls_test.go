package extmgr

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The assistant's machine lines and machines_list read what can be done on each Mac from the plugins' settings, and
// the live screen's state from what the Mac's node said last: nothing while both plugins are off, and a Mac keeps its
// switches while it is away.
func TestMachineControlsFollowThePlugins(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	add := func(id string) *Machine {
		m := newMachine(h, MachineConfig{ID: id, Name: id})
		m.online, m.info = true, proto.NodeInfo{OS: "darwin"}
		h.machines, h.byID[id] = append(h.machines, m), m
		return m
	}
	mac, mini, _ := add("mac"), add("mini"), add("studio")
	if got := h.MachineControls(); got != nil {
		t.Fatalf("both plugins off: %v", got)
	}
	for id, config := range map[string]string{cuaExtensionID: `{"machines": ["mac"], "backend": "lcu"}`, egoExtensionID: `{"machines": ["mac", "mini"]}`} {
		if err := h.extensions.reg.SetEnabled(ctx, id, true, "test"); err != nil {
			t.Fatal(err)
		}
		if err := h.extensions.reg.SetConfig(ctx, id, json.RawMessage(config), "test"); err != nil {
			t.Fatal(err)
		}
	}
	got := h.MachineControls()
	if len(got) != 2 || got["mac"] != (MachineControl{ComputerUse: "lcu", Driver: "lcu", Browser: true}) || got["mini"] != (MachineControl{Browser: true}) {
		t.Fatalf("before any status: %+v", got)
	}

	// The live screen is cua's on a Mac it is on for, ego's on one only ego is on for.
	mac.mu.Lock()
	mac.computer = &proto.Computer{ScreenSharing: true, ScreenVideo: &proto.ScreenVideo{Permission: "missing", Asked: true}}
	mac.browser = &proto.Browser{ScreenSharing: false}
	mac.online = false
	mac.mu.Unlock()
	mini.mu.Lock()
	mini.browser = &proto.Browser{ScreenSharing: true, ScreenVideo: &proto.ScreenVideo{Permission: "granted"}}
	mini.mu.Unlock()
	got = h.MachineControls()
	if want := (MachineControl{ComputerUse: "lcu", Driver: "lcu", Browser: true, Reported: true, ScreenSharing: true,
		ScreenPermission: "missing", ScreenAsked: true}); got["mac"] != want {
		t.Fatalf("a Mac away keeps what it said last: %+v", got["mac"])
	}
	if want := (MachineControl{Browser: true, Reported: true, ScreenSharing: true, ScreenPermission: "granted"}); got["mini"] != want {
		t.Fatalf("a Mac with the browser only: %+v", got["mini"])
	}
	if err := h.extensions.reg.SetConfig(ctx, cuaExtensionID, json.RawMessage(`{"machines": ["mac"], "backend": "auto"}`), "test"); err != nil {
		t.Fatal(err)
	}
	if c := h.MachineControls()["mac"]; c.ComputerUse != "auto" || c.Driver != "auto" {
		t.Fatalf("auto before a status says LCU: %+v", c)
	}
}
