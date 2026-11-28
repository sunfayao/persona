package slashcommands

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

func TestSessionTargetIsDerivedFromTheRoute(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/?cwd=/forged&launcher=forged", nil)
	req.SetPathValue("sid", "example")
	req.Header.Set(assistantwire.RoleHeader, "full")
	response := httptest.NewRecorder()
	Serve(response, req, true, func(ctx context.Context, target proto.SlashCommandQuery, out *proto.SlashCommandCatalog) error {
		if target != (proto.SlashCommandQuery{SessionID: "example"}) {
			t.Fatalf("target = %+v", target)
		}
		out.Commands = []proto.SlashCommand{{Name: "/status", Source: proto.SlashBuiltin}}
		return nil
	})
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response %d %s", response.Code, response.Body)
	}
}

func TestOnlyOwnerAndCapableNodesDiscoverCommands(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		role    string
		capable bool
		want    int
	}{
		{"extension", true, http.StatusForbidden},
		{"full", false, http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, "/?cwd=/example&launcher=codex", nil)
		req.Header.Set(assistantwire.RoleHeader, test.role)
		response := httptest.NewRecorder()
		Serve(response, req, test.capable, func(context.Context, proto.SlashCommandQuery, *proto.SlashCommandCatalog) error {
			t.Fatal("ineligible request reached the node")
			return nil
		})
		if response.Code != test.want {
			t.Fatalf("response %d, want %d", response.Code, test.want)
		}
	}
}
