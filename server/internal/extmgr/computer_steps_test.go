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

// Cua Driver says "(unverified)" in a step's report when it could not read back what the step typed or set: the answer
// adds a line for each such step, pointing at the element token, and makes up none for the rest.
func TestComputerRunSaysWhichStepsWereUnverified(t *testing.T) {
	const id = "mac-unverified"
	h, _ := stepsMac(t, id, `{"machines": ["mac-unverified"], "app_approval": false}`)
	ctx := context.Background()
	report := func(r string) {
		fakeNoLockMu.Lock()
		fakeRunSteps[id] = r
		fakeNoLockMu.Unlock()
	}
	t.Cleanup(func() { report("") })
	run := func(args string) string {
		t.Helper()
		res := h.callComputerTool(ctx, "conv-unverified", "computer_run", json.RawMessage(args))
		if res.IsError {
			t.Fatalf("%s: %+v", args, res)
		}
		return res.Content[0].Text
	}
	const note = "Step 2 (type_text) is unverified: the app may have ignored it (check with observe). To press a button, " +
		"click its element_token from computer_state or observe instead."
	report(`{"ok": true, "total": 3, "executed": 3, "failed_step": null, "steps": [
		{"index": 0, "tool": "click", "ok": true, "message": "✅ Performed AXPress on [5] AXButton \"7\"."},
		{"index": 1, "tool": "type_text", "ok": true, "message": "📨 Sent (unverified) 1 char(s). — driver could not confirm the text landed"},
		{"index": 2, "tool": "press_key", "ok": true, "message": "✅ Pressed return on pid 63."}]}`)
	text := run(`{"machine": "mac-unverified", "app": "Calculator", "steps": [{"click": {"name": "7"}}, {"type_text": {"text": "*"}}, {"press_key": {"key": "return"}}]}`)
	if !strings.HasSuffix(text, "}\n"+note) || strings.Count(text, "is unverified") != 1 {
		t.Fatalf("one line, for step 2: %q", text)
	}

	// Steps naming no app run one at a time: each answer is one step's, numbered as the call's.
	report(`{"ok": true, "total": 1, "executed": 1, "failed_step": null, "steps": [{"index": 0, "tool": "set_value", "ok": true, "message": "📨 Sent (unverified) AXValue on [4] AXTextField."}]}`)
	text = run(`{"machine": "mac-unverified", "steps": [{"set_value": {"element_token": "s1:4", "value": "x"}}, {"set_value": {"element_token": "s1:5", "value": "y"}}]}`)
	if !strings.Contains(text, `"one_by_one":true`) || !strings.Contains(text, "\nStep 1 (set_value) is unverified") || !strings.Contains(text, "\nStep 2 (set_value) is unverified") {
		t.Fatalf("one at a time: %q", text)
	}

	// Nothing said unverified, nothing added: a step that failed says what went wrong itself.
	for _, r := range []string{
		`{"ok": true, "executed": 1}`,
		`{"ok": true, "total": 1, "executed": 1, "steps": [{"index": 0, "tool": "type_text", "ok": true, "message": "✅ Inserted 1 char(s)."}]}`,
		`{"ok": false, "total": 1, "executed": 1, "failed_step": 0, "steps": [{"index": 0, "tool": "type_text", "ok": false, "message": "type_text failed (unverified)"}]}`,
	} {
		report(r)
		if text := run(`{"machine": "mac-unverified", "app": "Calculator", "steps": [{"type_text": {"text": "1"}}]}`); strings.Contains(text, "is unverified") {
			t.Errorf("%s: %q", r, text)
		}
	}
}
