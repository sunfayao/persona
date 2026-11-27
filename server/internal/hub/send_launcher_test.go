package hub

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// A conversation whose model the gateway dropped (claude-opus-5) falls back to the launcher's default instead of
// failing every turn with "There's an issue with the selected model".
func TestSendDropsAModelTheLauncherNoLongerOffers(t *testing.T) {
	for _, c := range []struct{ model, want string }{{"claude-opus-5", ""}, {"claude-opus-5-5", "claude-opus-5-5"}} {
		t.Run(c.model, func(t *testing.T) {
			h := newTestHub(t)
			m := newMachine(h, MachineConfig{ID: "box-" + c.model, Name: "box"})
			h.byID[m.cfg.ID] = m
			h.machines = append(h.machines, m)
			hubIn, nodeOut := io.Pipe()
			nodeIn, hubOut := io.Pipe()
			hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
			var mu sync.Mutex
			var got map[string]any
			nodePeer.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
				switch method {
				case "launchers.models":
					return map[string]any{"agent": "claude", "models": []any{map[string]any{"id": "claude-opus-5-5"}}}, nil
				case "launchers.list": // the launcher is still there; only the model is gone
					return map[string]any{"launchers": []any{map[string]any{"name": "l"}}}, nil
				case "session.send":
					mu.Lock()
					got = map[string]any{}
					_ = json.Unmarshal(params, &got)
					mu.Unlock()
				}
				return map[string]any{"ok": true}, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			go func() { _ = hubPeer.Serve(ctx, hubIn) }()
			go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
			m.mu.Lock()
			m.peer, m.online = hubPeer, true
			m.info.Home = "/home/u"
			m.sessions = []proto.Session{{SessionID: "side", Launcher: "l", Model: c.model, Assistant: true, Cwd: "/home/u/.persona/assistant"}}
			m.mu.Unlock()
			connectAssistant(t, h) // what a message to its conversation runs on is its decision
			if code, out := call(t, h.routes(), "POST", "/api/v1/sessions/"+m.cfg.ID+"/side/messages", h.cfg.Token, `{"text":"hi"}`); code != 200 {
				t.Fatalf("%d %v", code, out)
			}
			mu.Lock()
			model, _ := got["model"].(string)
			mu.Unlock()
			if model != c.want {
				t.Errorf("model %q, want %q", model, c.want)
			}
		})
	}
}

// newAgentStore answers "New Agent"'s model setting (the assistant's to read) with b.
type newAgentStore struct {
	*memStore
	b assistantwire.ModelBinding
}

func (s *newAgentStore) Setting(_ context.Context, key string, out any) (bool, error) {
	if key != "model_config.agent.new" {
		return false, nil
	}
	raw, _ := json.Marshal(map[string]any{"revision": 1, "binding": s.b})
	return true, json.Unmarshal(raw, out)
}

// A message to a session that is not running and whose launcher nobody knows (started outside Persona) carries "New
// Agent's launcher and model, so the node resumes it on them instead of plain claude; a known launcher, a running
// session and a launcher the app picked are left alone. Which launcher and model that is is the manager's to say
// (agents.resume_params); while it is not connected the hub adds nothing.
func TestOfflineSessionWithoutLauncherResumesOnNewAgents(t *testing.T) {
	h := newTestHub(t)
	h.store = &newAgentStore{memStore: newMemStore(), b: assistantwire.ModelBinding{Provider: "launcher.work", Model: "m-1"}}
	m := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	h.byID["box"] = m
	h.machines = append(h.machines, m)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	var mu sync.Mutex
	var got map[string]any
	nodePeer.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method == "session.send" {
			mu.Lock()
			got = map[string]any{}
			_ = json.Unmarshal(params, &got)
			mu.Unlock()
		}
		return map[string]any{"ok": true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = hubPeer.Serve(ctx, hubIn) }()
	go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.sessions = []proto.Session{
		{SessionID: "gone-1", Status: proto.StatusOffline},
		{SessionID: "known-2", Status: proto.StatusOffline, Launcher: "mine", StartedByPersona: true},
		{SessionID: "live-3", Status: proto.StatusIdle},
		{SessionID: "codex-4", Status: proto.StatusOffline, Agent: "codex"},
	}
	m.mu.Unlock()

	mux := h.routes()
	send := func(sid, body string) (string, string) {
		t.Helper()
		if code, out := call(t, mux, "POST", "/api/v1/sessions/box/"+sid+"/messages", h.cfg.Token, body); code != 200 {
			t.Fatalf("%s: %d %v", sid, code, out)
		}
		mu.Lock()
		defer mu.Unlock()
		l, _ := got["launcher"].(string)
		model, _ := got["model"].(string)
		return l, model
	}
	// Without the manager the hub adds nothing (it does not reach the machine without it: devices_link.go).
	if l, model := send("gone-1", `{"text":"hi"}`); l != "" || model != "" {
		t.Errorf("without the manager: launcher %q model %q, want nothing added", l, model)
	}
	connectAssistant(t, h)
	for _, c := range []struct{ sid, body, launcher, model string }{
		{"gone-1", `{"text":"hi"}`, "work", "m-1"},
		{"gone-1", `{"text":"hi","launcher":"other"}`, "other", ""},
		{"known-2", `{"text":"hi"}`, "", ""},
		{"live-3", `{"text":"hi"}`, "", ""},
		{"codex-4", `{"text":"hi"}`, "", ""},
	} {
		if l, model := send(c.sid, c.body); l != c.launcher || model != c.model {
			t.Errorf("%s %s: launcher %q model %q, want %q %q", c.sid, c.body, l, model, c.launcher, c.model)
		}
	}
}
