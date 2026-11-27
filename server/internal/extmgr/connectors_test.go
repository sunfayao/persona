package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/connector/github"
	"github.com/sunfayao/persona/server/internal/connector/imessage"
	"github.com/sunfayao/persona/server/internal/mail"
	"github.com/sunfayao/persona/server/internal/proto"
)

// exampleConnector reads and comments, on made-up data.
type exampleConnector struct{ commented []string }

func (*exampleConnector) ID() string   { return "example" }
func (*exampleConnector) Name() string { return "Example" }
func (*exampleConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapActions}}
}
func (*exampleConnector) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK, Account: "alice"}
}
func (*exampleConnector) Tools() []proto.Tool { return []proto.Tool{{Name: "example_read"}} }
func (*exampleConnector) Call(context.Context, string, json.RawMessage) (string, error) {
	return "example-org/example-repo#12", nil
}
func (*exampleConnector) Actions() []proto.Tool { return []proto.Tool{{Name: "example_comment"}} }
func (*exampleConnector) Describe(context.Context, string, json.RawMessage) (connector.Action, string, error) {
	return connector.Action{Visible: "example-org/example-repo 的所有人"}, "评论 example-org/example-repo#12：示例回复", nil
}
func (c *exampleConnector) Do(_ context.Context, name string, _ json.RawMessage) (string, error) {
	c.commented = append(c.commented, name)
	return "Commented.", nil
}

type namedConnector string

func (c namedConnector) ID() string                 { return string(c) }
func (c namedConnector) Name() string               { return string(c) }
func (namedConnector) Manifest() connector.Manifest { return connector.Manifest{} }

// toolConnector offers the tools named.
type toolConnector struct {
	id    string
	tools []string
}

func (c toolConnector) ID() string   { return c.id }
func (c toolConnector) Name() string { return c.id }
func (toolConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapTools}}
}
func (c toolConnector) Tools() []proto.Tool {
	var out []proto.Tool
	for _, n := range c.tools {
		out = append(out, proto.Tool{Name: n})
	}
	return out
}
func (toolConnector) Call(context.Context, string, json.RawMessage) (string, error) { return "", nil }

// A connector may not offer a tool the hub has itself; one only named like them ("lark") is fine, does not take
// the hub's lark_* calls (lark_agent_reply stays the hub's), and is listed only to an app that asks for all (?all=1).
func TestConnectorsShadowNoBuiltinTool(t *testing.T) {
	h := newTestManager(t)
	h.connectors = connector.NewRegistry(nil)
	h.opts.AssistantTools = func() []proto.Tool { // the assistant's, in this process
		return []proto.Tool{{Name: "kb_search"}, {Name: "lark_agent_reply"}, {Name: "agent_send"}}
	}
	for _, c := range []toolConnector{{"kb", []string{"kb_search"}}, {"phone", []string{"phone_contacts"}},
		{"agent", []string{"agent_send"}}} {
		if err := h.addConnector(c); err == nil {
			t.Errorf("connector %q offering %v was registered", c.id, c.tools)
		}
	}
	if err := h.addConnector(namedConnector("example")); err != nil {
		t.Fatal(err)
	}
	if err := h.addConnector(namedConnector("lark")); err != nil {
		t.Fatal(err)
	}
	_, out := call(t, h.routes(), "GET", "/api/v1/connectors", h.cfg.Token, "")
	if list := out["connectors"].([]any); len(list) != 1 || list[0].(map[string]any)["id"] != "example" {
		t.Fatalf("listed %v", list)
	}
	// An app that draws Feishu's row from the list asks for all of them.
	_, out = call(t, h.routes(), "GET", "/api/v1/connectors?all=1", h.cfg.Token, "")
	if list := out["connectors"].([]any); len(list) != 2 || list[1].(map[string]any)["id"] != "lark" {
		t.Fatalf("listed with all=1: %v", list)
	}
}

// Without a connector nothing changes (the MCP golden); with one, the assistant gets its tools, agents do not, a read
// runs, a write waits for the phone's card and the card says who will see it. The hub lists and runs them for the
// agents' MCP (hubConnectorTools, HubConnectorTool), the card is the hub's.
func TestConnectorToolsForTheAssistant(t *testing.T) {
	h := newTestManager(t)
	h.connectors = connector.NewRegistry(nil)
	ctx := context.Background()
	tools := func(assistant bool) []string {
		out, err := testHubCall(ctx, h, hubConnectorTools, json.RawMessage(fmt.Sprintf(`{"assistant":%v}`, assistant)))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tl := range managerTools(out) {
			names = append(names, tl.Name)
		}
		return names
	}
	before := len(tools(true))
	c := &exampleConnector{}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	assistant := tools(true)
	if len(assistant) != before+2 || !slices.Contains(assistant, "example_read") || !slices.Contains(assistant, "example_comment") {
		t.Fatalf("assistant tools %v", assistant)
	}
	if slices.Contains(tools(false), "example_read") {
		t.Fatal("an agent got a connector's tool")
	}

	mcpCall := func(name string, assistant bool) (proto.ToolResult, bool) {
		params, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{}, "assistant": assistant, "machine_id": "box"})
		out, err := testHubCall(ctx, h, hubConnectorTool, params)
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]any)
		if m["handled"] != true {
			return proto.ToolResult{}, false
		}
		return m["result"].(proto.ToolResult), true
	}
	if r, ok := mcpCall("example_read", true); !ok || r.IsError || r.Content[0].Text != "example-org/example-repo#12" {
		t.Fatalf("read: %+v", r)
	}
	if _, ok := mcpCall("example_read", false); ok {
		t.Fatal("an agent ran a connector's tool")
	}

	asked := make(chan proto.Interaction, 1)
	release := make(chan struct{})
	hubOf(h).answer = func(ix proto.Interaction) proto.Resolution {
		asked <- ix
		<-release
		allow := true
		return proto.Resolution{Allow: &allow}
	}
	done := make(chan proto.ToolResult, 1)
	go func() { r, _ := mcpCall("example_comment", true); done <- r }()
	var ix proto.Interaction
	select {
	case ix = <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("no card")
	}
	if ix.ToolName != "example_comment" || ix.MachineID != "box" || !strings.Contains(ix.Summary, "谁会看到：example-org/example-repo 的所有人") {
		t.Fatalf("card %+v", ix)
	}
	if len(c.commented) != 0 {
		t.Fatal("commented before the user allowed it")
	}
	close(release)
	if r := <-done; r.IsError || len(c.commented) != 1 {
		t.Fatalf("comment: %+v %v", r, c.commented)
	}
}

func TestConnectorsEndpoint(t *testing.T) {
	h := newTestManager(t)
	h.connectors = connector.NewRegistry(nil)
	mux := h.routes()
	code, out := call(t, mux, "GET", "/api/v1/connectors", h.cfg.Token, "")
	if code != 200 || len(out["connectors"].([]any)) != 0 {
		t.Fatalf("%d %v", code, out)
	}
	_ = h.addConnector(&exampleConnector{})
	_, out = call(t, mux, "GET", "/api/v1/connectors", h.cfg.Token, "")
	list := out["connectors"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["account"] != "alice" || list[0].(map[string]any)["status"] != "ok" {
		t.Fatalf("%v", out)
	}
}

// What GET /api/v1/connectors?all=1 says each built-in connector can do (the app goes by these names).
func TestConnectorsListTheSameCapabilities(t *testing.T) {
	h := newTestManager(t)
	h.mail = (*mail.Service)(nil)
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	if err := h.addConnector(imessage.NewRemote(imessage.Config{Machine: "mac"}, "Mac", nil)); err != nil {
		t.Fatal(err)
	}
	connectFakeManager(t, h) // GitHub's options come with the inbox's muted repositories, the manager's
	var got []string
	for _, info := range h.listedConnectors(context.Background(), true) {
		got = append(got, fmt.Sprintf("%s %s hosts=%v options=%v", info.ID, strings.Join(info.Capabilities, ","), info.Hosts, info.Options != nil))
	}
	want := []string{
		"mail tools,actions hosts=[] options=false",
		"github status,tools,actions,events,hosts,inbox,login,options,threads hosts=[github.com] options=true",
		"imessage status,tools,catalog,events,inbox hosts=[] options=false", // catalog: inbox_things (#6317)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The coding agents get mail's tools, mail_send among them, and nobody else's.
	for name, want := range map[string]bool{"mail_send": true, "mail_search": true, "github_read": false, "lark_search": false} {
		if h.connectors.ForAgents(name) != want {
			t.Errorf("%s for agents: %v", name, !want)
		}
	}
}

func TestGhTokenFromAMachine(t *testing.T) {
	h := newTestManager(t)
	m := newMachine(h, MachineConfig{ID: "mac", Name: "Mac", Kind: "local"})
	h.byID["mac"] = m
	if _, err := h.ghToken("mac")(context.Background(), "github.com"); !errors.Is(err, github.ErrOffline) {
		t.Fatalf("offline: %v", err)
	}
	if _, err := h.ghToken("nope")(context.Background(), "github.com"); !errors.Is(err, github.ErrNoMachine) {
		t.Fatalf("unknown machine: %v", err)
	}
}
