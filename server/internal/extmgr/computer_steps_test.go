package extmgr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/protectedapps"
)

// stepsMac is a Mac the cua plugin is on for with config (no app's approval asked: computer_run goes straight to the
// Mac), and what its node was asked.
func stepsMac(t *testing.T, id, config string) (*Manager, func() []computerCallSeen) {
	t.Helper()
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	_, seen := computerMac(t, h, id)
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if err := h.extensions.reg.SetConfig(ctx, cuaExtensionID, json.RawMessage(config), "test"); err != nil {
		t.Fatal(err)
	}
	return h, func() []computerCallSeen { _, calls := seen(); return calls }
}

// A refusal hands the user the Mac's live screen: on the hub's public address when it has one, else its path in the
// web app.
func TestRefusalsNameTheMacsLiveScreen(t *testing.T) {
	m := newMachine(nil, MachineConfig{ID: "mac 1", Name: "Mac Studio"})
	for _, c := range []struct {
		h    *Manager
		want string
	}{
		{&Manager{cfg: &Config{PublicURL: "https://hub.example.com/persona/"}}, "https://hub.example.com/persona/screen/mac%201"},
		{&Manager{cfg: &Config{}}, "/screen/mac%201"},
		{&Manager{}, "/screen/mac%201"},
	} {
		if got := c.h.refusalMac(m); got != (protectedapps.Mac{Name: "Mac Studio", Screen: c.want}) {
			t.Errorf("%+v", got)
		}
	}

	// On LCU, a step naming System Settings is refused by name, with the same hand-off.
	h, _ := stepsMac(t, "mac-lcu-refused", `{"machines": ["mac-lcu-refused"], "backend": "lcu", "app_approval": false}`)
	res := h.callComputerTool(context.Background(), "conv-steps", "computer_run",
		json.RawMessage(`{"machine": "mac-lcu-refused", "steps": [{"click": {"app": "System Settings", "name": "x"}}]}`))
	if !res.IsError || !strings.Contains(res.Content[0].Text, "do this themselves, on the Mac or through its live screen at "+
		"http://hub.example:18660/screen/mac-lcu-refused in the Persona web app (it opens in View only; switch to Take control to click)") {
		t.Fatalf("LCU: %+v", res)
	}
}
