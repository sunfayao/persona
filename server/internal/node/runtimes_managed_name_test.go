package node

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// A Persona-managed launcher made from a runtime's provider keeps the provider's name; the runtime offering that name
// must not take its starts or its assistant conversations: personad launch runs it, as the pickers list it.
func TestManagedLauncherNamedLikeARuntimeProviderIsNotTheRuntimes(t *testing.T) {
	n, _ := runtimeNode(t)
	hello := dshHello()
	hello["caps"].(map[string]any)["assistant"] = true
	host := dialRuntime(t, n, hello)
	started := 0
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			started++
			return map[string]any{"session": map[string]any{"session_id": "session-new", "cwd": "/w", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	launcherCache.Lock()
	launcherCache.at, launcherCache.list = time.Now(), []proto.Launcher{{Name: "claude"}}
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = time.Time{}, nil
		launcherCache.Unlock()
	})
	if n.runtimeOf("dsh") == nil {
		t.Fatal("the runtime's own launcher: the runtime runs it")
	}
	n.setManaged([]proto.ManagedLauncher{{Name: "dsh", BaseURL: "https://gw.example", Model: "m"}})
	if n.runtimeOf("dsh") != nil || n.assistantRuntime("dsh") != nil {
		t.Fatal("a managed launcher of the runtime's name went to the runtime")
	}
	if _, ok := n.runtimeLauncherModels("dsh"); ok {
		t.Fatal("the managed launcher's models came from the runtime")
	}
	_, _, ok := n.runtimeStart(context.Background(), runtimeStartParams{Cwd: "/w", Launcher: "dsh", Prompt: "hi"})
	if ok || started != 0 {
		t.Fatalf("the start went to the runtime (ok %v, starts %d)", ok, started)
	}
}

// A managed Antigravity launcher is started by the runtime's underlying agy launcher. Its model picker uses that same
// runtime, despite the managed name taking precedence for ordinary launchers.
func TestManagedAgyLauncherUsesUnderlyingRuntimeModels(t *testing.T) {
	withShellLaunchers(t, "claude")
	n, _ := runtimeNode(t)
	n.setManaged([]proto.ManagedLauncher{{Name: "agy", Agent: "agy", BaseURL: "https://gw.example", Model: "managed-model"}})
	hello := paseoEngineHello()
	hello["runtime"], hello["agent"], hello["launcher"] = "agy", "agy", "agy"
	hello["caps"].(map[string]any)["models"] = true
	hello["launchers"] = []map[string]any{{"name": "agy", "agent": "agy", "models": []map[string]any{{"id": "advertised-model"}}}}
	host := dialRuntime(t, n, hello)
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "launchers.models" {
			return proto.LauncherModels{Models: []proto.AgentModel{{ID: "native-model", Name: "Native Model"}}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	raw, err := nodeCall(t, n, "launchers.models", map[string]any{"name": "agy", "refresh": true})
	if err != nil {
		t.Fatal(err)
	}
	var got proto.LauncherModels
	_ = json.Unmarshal(raw, &got)
	if got.Agent != "agy" || len(got.Models) != 1 || got.Models[0].ID != "native-model" {
		t.Fatalf("managed agy models %s", raw)
	}
	calls := host.got("launchers.models")
	if len(calls) != 1 {
		t.Fatalf("native model discovery calls %d", len(calls))
	}
	var call map[string]any
	_ = json.Unmarshal(calls[0], &call)
	if call["launcher"] != "agy" || call["refresh"] != true {
		t.Fatalf("native model discovery params %v", call)
	}

	// The start goes to that runtime with the managed model, so ultracode is judged as a runtime start too.
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "agy-managed", "cwd": "/w", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	cwd := t.TempDir()
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": cwd, "launcher": "agy", "async": true}); err != nil {
		t.Fatal(err)
	}
	var start map[string]any
	_ = json.Unmarshal(host.got("session.start")[0], &start)
	if start["model"] != "managed-model" {
		t.Fatalf("managed agy start %v", start)
	}
	_, err = nodeCall(t, n, "session.start", map[string]any{"cwd": cwd, "launcher": "agy", "ultracode": true, "async": true})
	if rpcCode(err) != ultracodeUnsupportedCode || len(host.got("session.start")) != 1 {
		t.Fatalf("ultracode on a managed agy start: %v, starts %d", err, len(host.got("session.start")))
	}
}
