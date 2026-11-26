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
