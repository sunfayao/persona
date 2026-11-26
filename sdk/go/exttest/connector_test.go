package exttest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/sdk/go/ext"
	"github.com/sunfayao/persona/sdk/go/exttest"
	"github.com/sunfayao/persona/sdk/go/wire"
)

var connectorManifest = []byte(`{
  "manifest_version": 1, "id": "examplechat", "name": "Example Chat", "version": "1.0.0",
  "engines": {"extension_protocol": 1},
  "runtime": {"command": "bin/examplechat"},
  "activation_events": ["on_startup"],
  "permissions": {"hub": ["todos.read"]},
  "contributes": {
    "connectors": [{"id": "examplechat", "name": "Example Chat", "capabilities": ["status", "inbox", "threads", "login", "events"], "kinds": ["mention"]}],
    "events": [{"id": "todos", "subscribe": ["todo.changed"]}]
  }
}`)

// exampleChat is a connector written with the SDK alone: its calls are the schema's types.
type exampleChat struct {
	hub     *ext.Hub
	init    *ext.Init
	mu      sync.Mutex
	heard   []string
	account string
}

func (c *exampleChat) program() ext.Program {
	conn := ext.Methods{
		"status": ext.Method(func(ctx context.Context, p wire.ConnectorParams) (wire.ConnectorStatus, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.account == "" {
				return wire.ConnectorStatus{State: wire.ConnectorStateNotSignedIn, Error: "Not signed in."}, nil
			}
			return wire.ConnectorStatus{State: wire.ConnectorStateOk, Account: c.account}, nil
		}),
		"inbox.collect": ext.Method(func(ctx context.Context, p wire.ConnectorParams) (wire.ConnectorCollectResult, error) {
			return wire.ConnectorCollectResult{Things: []wire.ConnectorInboxThing{{Thread: "chat_1", Kind: "mention",
				Title: "Example group", At: time.Date(2026, 11, 4, 8, 26, 0, 0, time.UTC), Last: "6", Tier: wire.ConnectorTierAction}}}, nil
		}),
		"thread.page": ext.Method(func(ctx context.Context, p wire.ConnectorThreadParams) (wire.ConnectorThreadPage, error) {
			return wire.ConnectorThreadPage{Chat: wire.ConnectorThreadChat{ID: p.Thread, Name: "Example group", Kind: "group"},
				Messages: []wire.ConnectorThreadMessage{{ID: "6", At: time.Date(2026, 11, 4, 8, 26, 0, 0, time.UTC),
					Sender: wire.ConnectorThreadPerson{Name: "Example A"}, Text: "Hello"}}}, nil
		}),
		"login.step": ext.Method(func(ctx context.Context, p wire.ConnectorLoginParams) (wire.ConnectorPrompt, error) {
			if p.Step == "" {
				return wire.ConnectorPrompt{Step: "key", Fields: []wire.ConnectorField{{Name: "app_key", Label: "Application key", Secret: true}}}, nil
			}
			if err := c.hub.SaveSecret(ctx, p.Contribution, []byte(p.Values["app_key"])); err != nil {
				return wire.ConnectorPrompt{}, err
			}
			c.mu.Lock()
			c.account = "user_example_a"
			c.mu.Unlock()
			_ = c.hub.ConnectorChanged(p.Contribution, wire.ConnectorScopeInbox)
			return wire.ConnectorPrompt{Done: true, Account: "user_example_a"}, nil
		}),
		"logout": ext.Method(func(ctx context.Context, p wire.ConnectorParams) (ext.Empty, error) {
			return ext.Empty{}, c.hub.DeleteSecret(ctx, p.Contribution)
		}),
	}
	return ext.Program{ID: "examplechat", Version: "1.0.0", Manifest: connectorManifest,
		Init: func(ctx context.Context, in *ext.Init, hub *ext.Hub) (map[string]any, error) {
			c.hub, c.init = hub, in
			return nil, nil
		},
		Namespaces: map[string]ext.Handler{
			ext.ConnectorNamespace: conn.For("examplechat"),
			"event": func(ctx context.Context, method string, params json.RawMessage) (any, error) {
				c.mu.Lock()
				c.heard = append(c.heard, method)
				c.mu.Unlock()
				return nil, nil
			},
		},
	}
}

func TestAConnectorWrittenWithTheSDK(t *testing.T) {
	c := &exampleChat{}
	h := exttest.CheckConnectors(t, connectorManifest, exttest.Serve(t, c.program()))

	var p wire.ConnectorPrompt
	if err := h.Call("connector.login.step", wire.ConnectorLoginParams{Contribution: "examplechat", Step: "key",
		Values: map[string]string{"app_key": "fixture-key"}}, &p); err != nil || !p.Done {
		t.Fatalf("sign in: %+v %v", p, err)
	}
	if got := string(h.Secret("examplechat")); got != "fixture-key" {
		t.Fatalf("the secret kept: %q", got)
	}
	var st wire.ConnectorStatus
	if err := h.Call("connector.status", wire.ConnectorParams{Contribution: "examplechat"}, &st); err != nil || st.State != wire.ConnectorStateOk {
		t.Fatalf("status after sign-in: %+v %v", st, err)
	}
	if err := h.Call("connector.logout", wire.ConnectorParams{Contribution: "examplechat"}, nil); err != nil || h.Secret("examplechat") != nil {
		t.Fatalf("logout: %v, secret %q", err, h.Secret("examplechat"))
	}
	waitFor(t, func() bool { return len(h.Events()) > 0 })
	if e := h.Events()[0]; e.Name != "connector.changed" || !strings.Contains(string(e.Params), `"inbox"`) {
		t.Fatalf("event %s %s", e.Name, e.Params)
	}
	if err := h.Hear("event.todo.changed", wire.TodoChangedEvent{ID: "todo_example", Title: "Example", What: "created"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.heard) > 0 })
	if c.heard[0] != "todo.changed" {
		t.Fatalf("heard %v", c.heard)
	}
}

func TestTheHubsRulesFromTheSDK(t *testing.T) {
	c := &exampleChat{}
	h := exttest.Start(t, connectorManifest, exttest.Serve(t, c.program()))
	ctx := context.Background()
	var e *ext.Error
	if err := c.hub.Call(ctx, "connector.nope", map[string]any{}, nil); !errors.As(err, &e) || e.Code != wire.ErrorCodeUnknownMethod {
		t.Errorf("a method the hub does not have: %v", err)
	}
	// The extension's only connector takes its calls whatever they name, as the hub routes them.
	if _, err := c.hub.LoadSecret(ctx, ""); err != nil {
		t.Errorf("the only connector's secret, no contribution named: %v", err)
	}
	// An answer the test gives replaces the host's own, secrets too.
	h.Answer("connector.secrets.load", func(json.RawMessage) (any, error) { return nil, &ext.Error{Message: "the store is down"} })
	if _, err := c.hub.LoadSecret(ctx, "examplechat"); err == nil || !strings.Contains(err.Error(), "store is down") {
		t.Errorf("secrets.load answered by the test: %v", err)
	}
	// What the real hub starts it with: its contributions active, its permissions granted, the hub's capabilities.
	if in := c.init; in == nil || !slices.Contains(in.Active["connectors"], "examplechat") || !slices.Contains(in.Active["events"], "todos") ||
		!slices.Contains(in.Granted.Hub, "todos.read") || !slices.Contains(in.Hub.Capabilities, wire.HubCapabilityInboxGroup) {
		t.Errorf("initialize: %+v", c.init)
	}
}

// A connector that sends in the user's name on its own declares it (sends_as_user) and tells the hub of each send; as
// the real hub, the fake refuses a connector that does not declare it, and holds the params to the schema.
func TestRecordSentFromTheSDK(t *testing.T) {
	manifest := []byte(`{
  "manifest_version": 1, "id": "examplechat", "name": "Example Chat", "version": "1.0.0",
  "engines": {"extension_protocol": 1},
  "runtime": {"command": "bin/examplechat"},
  "activation_events": ["on_startup"],
  "contributes": {"connectors": [
    {"id": "examplechat", "name": "Example Chat", "capabilities": ["status"], "sends_as_user": true},
    {"id": "examplequiet", "name": "Example Quiet", "capabilities": ["status"]}
  ]}
}`)
	c := &exampleChat{}
	prog := c.program()
	prog.Manifest = manifest
	h := exttest.Start(t, manifest, exttest.Serve(t, prog))
	ctx := context.Background()
	err := c.hub.RecordSent(ctx, wire.ConnectorSentRecordParams{Contribution: "examplechat", Target: map[string]json.RawMessage{"chat": json.RawMessage(`"chat_1"`)},
		Text: "Thanks, got it.", Summary: "Auto-reply", SessionID: "conv_1", Setting: "auto_reply"})
	if got := h.Sent(); err != nil || len(got) != 1 || got[0].Contribution != "examplechat" || got[0].Text != "Thanks, got it." ||
		got[0].Setting != "auto_reply" || string(got[0].Target["chat"]) != `"chat_1"` {
		t.Fatalf("sent.record: %v, recorded %+v", err, h.Sent())
	}
	// As the hub keeps it: 20 keys of the target at most, in key order.
	many := map[string]json.RawMessage{}
	for i := range 25 {
		many[fmt.Sprintf("k%02d", i)] = json.RawMessage(`"v"`)
	}
	if err := c.hub.RecordSent(ctx, wire.ConnectorSentRecordParams{Contribution: "examplechat", Target: many, Text: ""}); err != nil {
		t.Fatalf("a long target and an empty text: %v", err)
	}
	if got := h.Sent(); len(got) != 2 || len(got[1].Target) != 20 || got[1].Target["k19"] == nil || got[1].Target["k20"] != nil {
		t.Errorf("the target as the hub keeps it: %+v", got)
	}
	var e *ext.Error
	err = c.hub.RecordSent(ctx, wire.ConnectorSentRecordParams{Contribution: "examplequiet", Target: map[string]json.RawMessage{}, Text: "x"})
	if !errors.As(err, &e) || e.Code != wire.ErrorCodePermissionDenied || len(h.Sent()) != 2 {
		t.Errorf("a connector that does not declare sends_as_user: %v, recorded %+v", err, h.Sent())
	}
	err = c.hub.RecordSent(ctx, wire.ConnectorSentRecordParams{Contribution: "another", Target: map[string]json.RawMessage{}, Text: "x"})
	if !errors.As(err, &e) || e.Code != wire.ErrorCodePermissionDenied || len(h.Sent()) != 2 {
		t.Errorf("a connector the extension does not have: %v, recorded %+v", err, h.Sent())
	}
}

func TestStartWithAnotherGrant(t *testing.T) {
	c := &exampleChat{}
	exttest.StartWith(t, connectorManifest, exttest.Serve(t, c.program()), func(p *wire.InitializeParams) {
		p.Granted = wire.Permissions{Hub: []string{"todos.read", "todos.write"}}
	})
	if in := c.init; in == nil || !slices.Equal(in.Granted.Hub, []string{"todos.read", "todos.write"}) {
		t.Errorf("initialize: %+v", c.init)
	}
}
