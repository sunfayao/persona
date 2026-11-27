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

// Steps Cua Driver would not run are refused before the Mac is asked anything, with the call that works: launch_app
// is no step of run_steps (it refuses the whole call, invalid_batch), so the app is opened by a script, and press_key
// takes key names, so a symbol is typed. LCU translates its steps itself, and is left to.
func TestComputerRunStepsCuaDriverWouldNotRun(t *testing.T) {
	h, seen := stepsMac(t, "mac-steps", `{"machines": ["mac-steps"], "app_approval": false}`)
	ctx := context.Background()
	run := func(steps string) (string, bool) {
		res := h.callComputerTool(ctx, "conv-steps", "computer_run", json.RawMessage(`{"machine": "mac-steps", "steps": `+steps+`}`))
		return res.Content[0].Text, res.IsError
	}
	for _, c := range []struct{ what, steps, want string }{
		{"an app opened as a step", `[{"launch_app": {"name": "Calculator"}}]`,
			"Nothing was done: launch_app cannot be a step (Cua Driver refuses it in run_steps). Open the app with script " +
				"`await cua.launch(\"Calculator\");`."},
		{"then the rest", `[{"tool": "launch_app", "args": {"name": "Calculator"}}, {"click": {"app": "Calculator", "name": "7"}}]`,
			"`await cua.launch(\"Calculator\");`, then send the other steps."},
		{"each app, its arguments beside its tool", `[{"tool": "launch_app", "name": "Notes"}, {"tool": "launch_app", "bundle_id": "com.apple.calculator"}]`,
			"Open the apps with script `await cua.launch(\"Notes\"); await cua.launch(\"com.apple.calculator\");`."},
		{"no app named", `[{"launch_app": {}}]`, "`await cua.launch(\"App name\");`"},
		{"System Settings: never, not by a script either", `[{"launch_app": {"name": "System Settings"}}]`,
			"Refused on mac-steps: it would operate System Settings. Persona never operates"},
		{"a symbol pressed", `[{"click": {"app": "Calculator", "name": "7"}}, {"press_key": {"app": "Calculator", "key": "*"}}]`,
			"Nothing was done: step 2 presses \"*\", and press_key takes key names (return, tab, escape, space, delete, " +
				"arrows, f1-f12, a letter or digit). Type a symbol with type_text, or click the app's button for it by its element_token."},
		{"a symbol, as {tool, args}", `[{"tool": "press_key", "args": {"app": "Calculator", "key": " $ "}}]`, "step 1 presses \"$\""},
		{"a symbol beside its tool", `[{"tool": "press_key", "app": "Calculator", "key": "×"}]`, "step 1 presses \"×\""},
	} {
		before := len(seen())
		text, isErr := run(c.steps)
		if !isErr || !strings.Contains(text, c.want) {
			t.Errorf("%s: refused, saying %q: %q", c.what, c.want, text)
		}
		if after := seen(); len(after) != before {
			t.Errorf("%s: the Mac was asked %+v", c.what, after[before:])
		}
	}
	// Key names, the unshifted punctuation keys and "+" (shift and "=") are press_key's own.
	for _, key := range []string{"return", "F5", "cmd+s", "a", "7", "+", "=", ".", "/", "-"} {
		if text, isErr := run(`[{"press_key": {"app": "Calculator", "key": "` + key + `"}}]`); isErr {
			t.Errorf("key %q: %s", key, text)
		}
	}
	// A script opens apps.
	if res := h.callComputerTool(ctx, "conv-steps", "computer_run", json.RawMessage(`{"machine": "mac-steps", "script": "await cua.launch(\"Calculator\"); return 1"}`)); res.IsError {
		t.Fatalf("a script opening an app: %+v", res)
	}

	// LCU: launch_app becomes cua.launch, and its runtime gets the key as it is.
	lcu, lcuSeen := stepsMac(t, "mac-lcu-steps", `{"machines": ["mac-lcu-steps"], "backend": "lcu", "app_approval": false}`)
	res := lcu.callComputerTool(ctx, "conv-steps", "computer_run", json.RawMessage(`{"machine": "mac-lcu-steps", "steps": `+
		`[{"launch_app": {"name": "Calculator"}}, {"press_key": {"app": "Calculator", "key": "*"}}]}`))
	calls := lcuSeen()
	if res.IsError || len(calls) != 1 || calls[0].Tool != "js" {
		t.Fatalf("LCU: %+v %+v", res, calls)
	}
	if code, _ := calls[0].Args["code"].(string); !strings.Contains(code, `await cua.launch("Calculator");`) || !strings.Contains(code, `.pressKey("*")`) {
		t.Fatalf("LCU's own JavaScript: %s", code)
	}
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
