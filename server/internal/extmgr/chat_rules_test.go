package extmgr

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/proto"
)

// chatGitHub is a connector with GitHub's id and write actions, whose writes are only counted.
type chatGitHub struct {
	mu  sync.Mutex
	did []string
}

func (*chatGitHub) ID() string   { return "github" }
func (*chatGitHub) Name() string { return "GitHub" }
func (*chatGitHub) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapActions}}
}
func (*chatGitHub) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK}
}
func (*chatGitHub) Actions() []proto.Tool {
	return []proto.Tool{{Name: "github_comment"}, {Name: "github_review"}}
}
func (*chatGitHub) Describe(_ context.Context, name string, args json.RawMessage) (connector.Action, string, error) {
	return connector.Action{Name: name, Visible: "everyone who can see the repository"}, name + " " + string(args), nil
}
func (c *chatGitHub) Do(_ context.Context, name string, args json.RawMessage) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.did = append(c.did, name+" "+string(args))
	return "done", nil
}
func (c *chatGitHub) done() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.did)
}

const (
	chatIssue12 = `{"repo":"example-org/example-repo","number":12,"body":"ok"}`
	chatIssue13 = `{"repo":"example-org/example-repo","number":13,"body":"ok"}`
)

// chatRuleManager is a manager with its gate running (startConnectorGate: hand-written rules, then the store's), the
// fake GitHub connector, ledger and stored rules.
func chatRuleManager(t *testing.T, hand []connector.Rule, rules []connector.UserRule) (*Manager, *chatGitHub) {
	t.Helper()
	h := newTestManager(t)
	keepSessions(t, h) // the devices keep the computers' sessions (listSessions)
	h.store = newSentLedgerStore()
	box := newMachine(h, MachineConfig{ID: "box", Name: "box"}) // a computer whose node gate can tell of a write
	h.machines, h.byID["box"] = append(h.machines, box), box
	h.cfg.ConnectorGate = hand
	if err := h.store.PutSetting(t.Context(), chatRulesKey, rules); err != nil {
		t.Fatal(err)
	}
	h.startConnectorGate(context.Background())
	gh := &chatGitHub{}
	if err := h.addConnector(gh); err != nil {
		t.Fatal(err)
	}
	yes := true
	hubOf(h).answer = func(proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &yes} }
	return h, gh
}

// chatWrite sends a GitHub write as the assistant does and says whether the user was asked.
func chatWrite(t *testing.T, h *Manager, gh *chatGitHub, tool, args string) (asked bool) {
	t.Helper()
	cards, did := len(hubOf(h).asked()), gh.done()
	if r := h.callConnector(context.Background(), asker{machine: "box", session: "conv1"}, tool, json.RawMessage(args)); r.IsError || gh.done() != did+1 {
		t.Fatalf("%s %s: %+v, %d writes after %d", tool, args, r, gh.done(), did)
	}
	return len(hubOf(h).asked()) > cards
}

// A write can name a field twice so a write that says `repo` and `REPO` (or `number` and `NUMBER`) could satisfy the rule with one and act on
// the other. Whatever the gate does with such a write (ask, or refuse), the connector is not told to write: nobody here
// says yes. A write of the same issue with the plain fields still goes through without a card, so the rule is in force and
// the test is not passing for want of one.
func TestAWriteNamingAFieldTwiceDoesNotReachTheConnectorByAUserRule(t *testing.T) {
	ctx := context.Background()
	h, gh := chatRuleManager(t, nil, []connector.UserRule{ghUserRule(12)})
	if chatWrite(t, h, gh, "github_comment", chatIssue12) {
		t.Fatal("the rule is not in force: the plain write asked")
	}
	no := false
	hubOf(h).answer = func(proto.Interaction) proto.Resolution { return proto.Resolution{Allow: &no} }
	for name, args := range map[string]string{
		"repo, then REPO":     `{"repo":"example-org/example-repo","number":12,"REPO":"other-org/other-repo","body":"ok"}`,
		"REPO, then repo":     `{"REPO":"other-org/other-repo","repo":"example-org/example-repo","number":12,"body":"ok"}`,
		"repo, then Repo":     `{"repo":"example-org/example-repo","number":12,"Repo":"other-org/other-repo","body":"ok"}`,
		"number, then NUMBER": `{"repo":"example-org/example-repo","number":12,"NUMBER":13,"body":"ok"}`,
		"NUMBER, then number": `{"NUMBER":13,"repo":"example-org/example-repo","number":12,"body":"ok"}`,
		"number, then Number": `{"repo":"example-org/example-repo","number":12,"Number":13,"body":"ok"}`,
	} {
		did := gh.done()
		r := h.callConnector(ctx, asker{machine: "box", session: "conv1"}, "github_comment", json.RawMessage(args))
		if gh.done() != did {
			t.Errorf("%s: the connector was told to write %s (the call answered %+v)", name, args, r)
		}
	}
}

// What the node's gate is served: the hand-written rules first (connector_gate's Lark rules, then lark_gate's), the
// user's after, so a hand-written deny or confirm still wins in the node's first-match order; and the same rules, nothing
// added, when the user has made none.
func TestServedLarkRulesKeepTheHandWrittenOnesFirst(t *testing.T) {
	ctx := context.Background()
	hand := []connector.Rule{
		{Connector: "lark", Action: "POST /conversations/oc_1/messages", Decision: connector.Deny},
		{Connector: "github", Action: "github_review", Decision: connector.Deny},
	}
	h, _ := chatRuleManager(t, hand, []connector.UserRule{larkUserRule("oc_1"), larkUserRule("oc_3")})
	h.cfg.LarkGate = []proto.LarkRule{{Method: "POST", Path: "/conversations/oc_2/messages", Action: "confirm"}}
	handWritten := []proto.LarkRule{
		{Method: "POST", Path: "/conversations/oc_1/messages", Action: "deny"},
		{Method: "POST", Path: "/conversations/oc_2/messages", Action: "confirm"},
	}
	want := append(slices.Clone(handWritten),
		proto.LarkRule{Method: "POST", Path: "/conversations/oc_1/messages", Action: "allow"},
		proto.LarkRule{Method: "POST", Path: "/conversations/oc_3/messages", Action: "allow"})
	lk, err := h.assistantLark(ctx)
	if err != nil || !reflect.DeepEqual(lk.Rules, want) {
		t.Fatalf("with user rules: %+v, %v; want %+v", lk.Rules, err, want)
	}
}

func TestChatRulesAreLoadedAtStart(t *testing.T) {
	ctx := context.Background()
	gh12, _ := connector.ChatRuleFor("github", "github_comment", json.RawMessage(`{"ref":"example-org/example-repo#12"}`))
	lark1, _ := connector.ChatRuleFor("lark", "POST /conversations/oc_1/messages", json.RawMessage(`{"conversation":"oc_1"}`))
	rules := []connector.UserRule{
		gh12,
		{Rule: connector.Rule{Connector: "*", Action: "*", Decision: connector.Allow}},
		{Rule: connector.Rule{Connector: "lark", Action: "POST /conversations/*/messages", Decision: connector.Allow}},
		lark1,
		{Rule: connector.Rule{Connector: "github", Action: "github_comment", Decision: connector.Deny,
			Match: map[string]any{"repo": "example-org/example-repo", "number": float64(13)}}},
	}
	h, gh := chatRuleManager(t, nil, rules)
	if got := h.connectors.UserRules(); len(got) != 2 {
		t.Fatalf("applied %+v", got)
	}
	if chatWrite(t, h, gh, "github_comment", chatIssue12) || !chatWrite(t, h, gh, "github_comment", chatIssue13) {
		t.Error("the gate does not follow the rules that were loaded")
	}
	if lk, _ := h.assistantLark(ctx); !reflect.DeepEqual(lk.Rules, []proto.LarkRule{{Method: "POST", Path: "/conversations/oc_1/messages", Action: "allow"}}) {
		t.Errorf("served %+v", lk.Rules)
	}
}
