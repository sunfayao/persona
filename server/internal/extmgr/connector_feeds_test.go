package extmgr

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// feedConnector has an inbox, and counts what it was asked.
type feedConnector struct{ collects int }

func (*feedConnector) ID() string   { return "example" }
func (*feedConnector) Name() string { return "Example" }
func (*feedConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}, Kinds: []string{"dm"}}
}
func (f *feedConnector) Collect(context.Context) ([]connector.InboxThing, error) {
	f.collects++
	return []connector.InboxThing{{Thread: "c1", Kind: "dm", Title: "示例用户", Preview: "示例消息", At: time.Now(), Last: "1"}}, nil
}
func (*feedConnector) Reply(context.Context, connector.InboxReply) error { return nil }

func listedExample(t *testing.T, h *Manager) map[string]any {
	t.Helper()
	_, out := call(t, h.routes(), "GET", "/api/v1/connectors", h.cfg.Token, "")
	for _, c := range out["connectors"].([]any) {
		if m := c.(map[string]any); m["id"] == "example" {
			return m
		}
	}
	t.Fatalf("example is not listed: %v", out)
	return nil
}

// "Add to Inbox" off: nothing new comes and what came stays. It is kept for the next start, and only a connector that
// has the feed can turn it off.
func TestConnectorFeedsTurnedOff(t *testing.T) {
	ctx := context.Background()
	h := newTestManager(t)
	st := newKeepStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	f := &feedConnector{}
	if err := h.addConnector(f); err != nil {
		t.Fatal(err)
	}
	mux := h.routes()
	if c := listedExample(t, h); c["archive"] != nil || c["inbox"] != true {
		t.Fatalf("the feed is not on to begin with: %v", c)
	}
	if src := h.readConnectors(ctx); f.collects != 1 || len(src) != 1 || len(src[0].Things) != 1 {
		t.Fatalf("the inbox did not collect: %d collect(s), %+v", f.collects, src)
	}

	if code, out := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"inbox":false}`); code != 200 || out["inbox"] != false || out["enabled"] != true {
		t.Fatalf("inbox off: %d %v", code, out)
	}
	// Read with nothing: nothing new comes, and what came stays (the manager's inbox leaves a source read with nothing
	// as it was).
	if src := h.readConnectors(ctx); f.collects != 1 || len(src) != 1 || len(src[0].Things) != 0 {
		t.Fatalf("collected %d time(s) with 进收件箱 off: %+v", f.collects, src)
	}

	next := newTestManager(t)
	next.store = st
	next.connectors = connector.NewRegistry(nil)
	_ = next.addConnector(&feedConnector{})
	next.restoreConnectorSwitches(ctx)
	if next.connectors.Feeds("example", connector.FeedInbox) {
		t.Fatalf("the next hub forgot the feed: %s", st.settings[connectorSettingsKey])
	}
	if !next.connectors.Enabled("example") {
		t.Fatal("turning the feed off switched the connector off")
	}

	other := newTestManager(t)
	other.connectors = connector.NewRegistry(nil)
	_ = other.addConnector(&exampleConnector{})
	if code, _ := call(t, other.routes(), "PUT", "/api/v1/connectors/example/settings", other.cfg.Token, `{"inbox":false}`); code != 400 {
		t.Fatalf("a connector without an inbox turned it off: %d", code)
	}
	_, list := call(t, other.routes(), "GET", "/api/v1/connectors", other.cfg.Token, "")
	if c := list["connectors"].([]any)[0].(map[string]any); c["archive"] != nil || c["inbox"] != nil {
		t.Fatalf("feeds listed for a connector without them: %v", c)
	}
}
