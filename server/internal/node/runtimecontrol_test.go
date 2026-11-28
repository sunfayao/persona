package node

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// paseoEngineHello is a runtime with a launcher per provider and the phone's modes.
func paseoEngineHello() map[string]any {
	h := paseoHello()
	h["caps"] = map[string]any{"start": true, "send": true, "interrupt": true, "rename": true, "close": true, "modes": true}
	h["launchers"] = []map[string]any{
		{"name": "paseo-claude", "agent": "claude", "model": "Opus 5.5", "models": []map[string]any{
			{"id": "claude-opus-5-5", "name": "Opus 5.5", "default": true, "efforts": []string{"low", "high", "max", "turbo"}},
			{"id": "", "name": "no id"}}},
		{"name": "paseo-codex", "agent": "paseo", "model": "GPT"},
		{"name": "bad name!", "agent": "claude"},
		{"name": "paseo-claude", "agent": "claude"},
	}
	return h
}

// A runtime running several agents offers a launcher for each, with its agent and models; what the form chose with
// one reaches the runtime when the session starts; runtime.launchers replaces them.
func TestRuntimeLaunchers(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, paseoEngineHello())
	raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Launchers []struct {
			proto.Launcher
			AgentType string `json:"agent_type"`
		} `json:"launchers"`
	}
	_ = json.Unmarshal(raw, &res)
	got := map[string]proto.Launcher{}
	for _, l := range res.Launchers {
		if l.Source == "paseo" {
			got[l.Name] = l.Launcher
		}
	}
	if len(got) != 2 || got["paseo-claude"].Agent != "" || got["paseo-claude"].Model != "Opus 5.5" || got["paseo-codex"].Agent != "paseo" {
		t.Fatalf("launchers %s", raw)
	}
	raw, err = nodeCall(t, n, "launchers.models", map[string]any{"name": "paseo-claude"})
	if err != nil {
		t.Fatal(err)
	}
	var models proto.LauncherModels
	_ = json.Unmarshal(raw, &models)
	if models.Agent != "claude" || len(models.Models) != 1 || !models.Models[0].Default || len(models.Models[0].Efforts) != 3 {
		t.Fatalf("models %s", raw)
	}

	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "paseo-n1", "status": "idle", "cwd": "/w/app"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w/app", "launcher": "paseo-codex", "model": "gpt-6",
		"permission_mode": "default", "effort": "high", "workspace_id": "wks_1", "async": true}); err != nil {
		t.Fatal(err)
	}
	var start map[string]any
	_ = json.Unmarshal(host.got("session.start")[0], &start)
	if start["launcher"] != "paseo-codex" || start["model"] != "gpt-6" || start["permission_mode"] != "default" || start["effort"] != "high" ||
		start["workspace_id"] != "wks_1" || start["shell_launcher"] != nil || start["resume"] != nil {
		t.Fatalf("started with %v", start)
	}

	_ = host.peer.Notify("runtime.launchers", map[string]any{"launchers": []map[string]any{{"name": "paseo-grok", "agent": "paseo"}}})
	waitFor(t, "the new launchers", func() bool {
		return n.runtimes.launcherRuntime("paseo-grok") != nil && n.runtimes.launcherRuntime("paseo-codex") == nil
	})
}

// A runtime's launcher carries the name the runtime gives its provider and, when its agent is not Claude Code, the
// permission modes a start on it takes (#8261): Persona's own, whatever else the runtime sent. A Claude Code launcher's
// are the apps' own five; an older runtime's launcher says neither.
func TestRuntimeLauncherNamesAndPermissionModes(t *testing.T) {
	n, _ := runtimeNode(t)
	h := paseoEngineHello()
	h["launchers"] = []map[string]any{
		{"name": "my-claude", "agent": "claude", "display_name": "My Claude", "permission_modes": []string{"default", "bypassPermissions"}},
		{"name": "grok", "agent": "paseo", "display_name": "  Grok  ", "permission_modes": []string{"bypassPermissions", "yolo", "default"}},
		{"name": "kimi", "agent": "paseo", "display_name": strings.Repeat("π", 80), "permission_modes": []string{"default"}},
		{"name": "older", "agent": "paseo"},
	}
	dialRuntime(t, n, h)
	raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Launchers []proto.Launcher `json:"launchers"`
	}
	_ = json.Unmarshal(raw, &res)
	got := map[string]proto.Launcher{}
	for _, l := range res.Launchers {
		if l.Source == "paseo" {
			got[l.Name] = l
		}
	}
	both := proto.PermissionModeSetOf("default", "bypassPermissions")
	for name, want := range map[string]struct {
		display string
		modes   proto.PermissionModeSet
	}{
		"my-claude": {"My Claude", 0},
		"grok":      {"Grok", both},
		"kimi":      {strings.Repeat("π", 64), proto.PermissionModeSetOf("default")},
		"older":     {"", 0},
	} {
		if l := got[name]; l.DisplayName != want.display || l.PermissionModes != want.modes {
			t.Errorf("%s: %q %v, want %q %v", name, l.DisplayName, l.PermissionModes.List(), want.display, want.modes.List())
		}
	}
}

// A launcher whose agents' approval is a switch Allow all turns on (Paseo's Auto Accept) says so in launchers.list, the
// runtime's own launcher too: the manager starts its own Agents there with Allow all (#8141). One the runtime does not
// say it of has no such field, and runtime.launchers carries it as hello does.
func TestRuntimeLauncherAutoAccept(t *testing.T) {
	n, _ := runtimeNode(t)
	h := paseoEngineHello()
	h["launcher"] = "paseo-grok"
	h["launchers"] = []map[string]any{
		{"name": "paseo-claude", "agent": "claude"},
		{"name": "paseo-grok", "agent": "paseo", "auto_accept": true},
		{"name": "paseo-pi", "agent": "paseo"},
	}
	host := dialRuntime(t, n, h)
	autoAccept := func() map[string]bool {
		raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Launchers []proto.Launcher `json:"launchers"`
		}
		_ = json.Unmarshal(raw, &res)
		got := map[string]bool{}
		for _, l := range res.Launchers {
			if l.Source == "paseo" {
				got[l.Name] = l.AutoAccept
			}
		}
		return got
	}
	if got := autoAccept(); len(got) != 3 || !got["paseo-grok"] || got["paseo-claude"] || got["paseo-pi"] {
		t.Fatalf("auto_accept by launcher: %v", got)
	}
	_ = host.peer.Notify("runtime.launchers", map[string]any{"launchers": []map[string]any{
		{"name": "paseo-grok", "agent": "paseo"}, {"name": "paseo-pi", "agent": "paseo", "auto_accept": true},
	}})
	waitFor(t, "the new launchers", func() bool { got := autoAccept(); return got["paseo-pi"] && !got["paseo-grok"] })
}

// The phone's permission mode and effort reach a runtime that takes them; one that does not refuses them.
func TestRuntimeModes(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, paseoEngineHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "paseo-m1", "agent": "claude", "status": "idle"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["paseo-m1"]; return ok })
	if _, err := nodeCall(t, n, "session.modes", map[string]any{"session_id": "paseo-m1", "permission_mode": "acceptEdits", "effort": "max"}); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(host.got("session.modes")[0], &m)
	if m["permission_mode"] != "acceptEdits" || m["effort"] != "max" {
		t.Fatalf("modes %v", m)
	}
	if _, err := nodeCall(t, n, "session.modes", map[string]any{"session_id": "paseo-m1", "permission_mode": "yolo"}); err == nil {
		t.Fatal("an unknown mode was passed on")
	}

	dsh := dialRuntime(t, n, dshHello()) // no caps.modes
	_ = dsh.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-m2", "status": "idle"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-m2"]; return ok })
	_, err := nodeCall(t, n, "session.modes", map[string]any{"session_id": "session-m2", "effort": "high"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "unsupported" || len(dsh.got("session.modes")) != 0 {
		t.Fatalf("a runtime without modes: %v", err)
	}
}

func TestRuntimeNativeModelsAndModelControl(t *testing.T) {
	n, _ := runtimeNode(t)
	hello := paseoEngineHello()
	hello["caps"].(map[string]any)["model"] = true
	hello["caps"].(map[string]any)["models"] = true
	host := dialRuntime(t, n, hello)
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "launchers.models" {
			return proto.LauncherModels{Models: []proto.AgentModel{{ID: "future-model", Name: "Future Model"}}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	got, err := n.launcherModels(context.Background(), "paseo-claude", "", true)
	if err != nil || len(got.Models) != 1 || got.Models[0].ID != "future-model" {
		t.Fatalf("native models: %+v %v", got, err)
	}
	var discovery map[string]any
	_ = json.Unmarshal(host.got("launchers.models")[0], &discovery)
	if discovery["refresh"] != true {
		t.Fatalf("refresh lost: %v", discovery)
	}
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "paseo-model", "agent": "claude", "status": "idle"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["paseo-model"]; return ok })
	row := listed(t, n)["paseo-model"]
	if row.Capabilities.Model == nil || !*row.Capabilities.Model {
		t.Fatal("model capability missing")
	}
	if _, err := nodeCall(t, n, "session.modes", map[string]any{"session_id": row.SessionID, "model": "future-model"}); err != nil {
		t.Fatal(err)
	}
	var change map[string]any
	_ = json.Unmarshal(host.got("session.modes")[0], &change)
	if change["model"] != "future-model" {
		t.Fatalf("model lost: %v", change)
	}
	host.mu.Lock()
	host.answer = func(string, json.RawMessage) (any, error) { return nil, errors.New("native catalog unavailable") }
	host.mu.Unlock()
	if _, err := n.launcherModels(context.Background(), "paseo-claude", "", true); err == nil {
		t.Fatal("native failure was hidden")
	}
}

func TestRuntimeWithoutModelCapabilityRejectsModel(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, paseoEngineHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "paseo-model", "agent": "claude", "status": "idle"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["paseo-model"]; return ok })
	if _, err := nodeCall(t, n, "session.modes", map[string]any{"session_id": "paseo-model", "model": "future"}); err == nil {
		t.Fatal("unadvertised model switch passed")
	}
	if len(host.got("session.modes")) != 0 {
		t.Fatal("unsupported model reached runtime")
	}
}

func TestRuntimeAvailabilityCheckRequiresOptIn(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, paseoEngineHello())
	if _, handled := n.runtimeLauncherCheck(context.Background(), n.runtimes.launcherRuntime("paseo-claude"), "paseo-claude", t.TempDir(), "probe", ""); handled {
		t.Fatal("older runtime intercepted the shell check")
	}
	if len(host.got("launchers.check")) != 0 {
		t.Fatal("unsupported check reached runtime")
	}
}

// The launcher a runtime runs the assistant on carries the Agent launcher it was set up to run it for
// (proto.Launcher.AssistantFor: integrations/grok's grok-chat for the Paseo provider grok); another launcher of it, a
// name no launcher may have and its own name do not.
func TestRuntimeAssistantLauncherSaysWhatItRunsTheAssistantFor(t *testing.T) {
	n, _ := runtimeNode(t)
	h := map[string]any{"runtime": "grok-chat", "name": "Grok", "agent": "grok", "launcher": "grok-chat",
		"caps": map[string]any{"start": true, "send": true, "interrupt": true, "assistant": true},
		"launchers": []map[string]any{
			{"name": "grok-chat", "agent": "grok", "display_name": "Grok", "assistant_for": "grok"},
			{"name": "grok-side", "agent": "grok", "assistant_for": "grok"},
		}}
	host := dialRuntime(t, n, h)
	got := func() map[string]proto.Launcher {
		raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Launchers []proto.Launcher `json:"launchers"`
		}
		_ = json.Unmarshal(raw, &res)
		out := map[string]proto.Launcher{}
		for _, l := range res.Launchers {
			out[l.Name] = l
		}
		return out
	}
	ls := got()
	if l := ls["grok-chat"]; !l.Assistant || l.AssistantFor != "grok" || l.DisplayName != "Grok" {
		t.Fatalf("grok-chat: %+v", l)
	}
	if l := ls["grok-side"]; l.Assistant || l.AssistantFor != "" {
		t.Fatalf("a launcher that does not run the assistant says what it runs it for: %+v", l)
	}
	for _, bad := range []string{"bad name!", "grok-chat"} {
		_ = host.peer.Notify("runtime.launchers", map[string]any{"launchers": []map[string]any{{"name": "grok-chat", "agent": "grok", "assistant_for": bad}}})
		waitFor(t, "the new launchers "+bad, func() bool { l := got()["grok-chat"]; return l.Assistant && l.AssistantFor == "" })
	}
}
