package hub

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/rpc"
)

// answerNode is a machine of the hub's whose node answers with fn, connected over a pipe.
func answerNode(t *testing.T, h *Hub, id string, fn rpc.Handler) *Machine {
	t.Helper()
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	registerMachine(h, m)
	hi, no := io.Pipe()
	ni, ho := io.Pipe()
	hp, np := rpc.New("h", ho), rpc.New("n", no)
	np.OnRequest = fn
	ctx, cancel := context.WithCancel(context.Background())
	go hp.Serve(ctx, hi)
	go np.Serve(ctx, ni)
	t.Cleanup(func() { cancel(); hp.Close(io.EOF); np.Close(io.EOF); hi.Close(); ni.Close() })
	m.peer, m.online = hp, true
	return m
}

// Workspace discovery is the manager's (assistant/workspaces.go): the hub only relays its route, 503 assistant_offline
// while the manager is away.
func TestWorkspacesRouteIsRelayed(t *testing.T) {
	h := newTestHub(t)
	w := hubCall(h, "GET", "/api/v1/workspaces", `{}`)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "assistant_offline") {
		t.Fatalf("GET /api/v1/workspaces without the manager: %d %s", w.Code, w.Body)
	}
}
