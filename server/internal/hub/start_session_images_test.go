package hub

import (
	"context"
	"net/http"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestStartSessionForwardsImageOnlyPromptAndClientMessageID(t *testing.T) {
	h := newTestHub(t)
	if !h.loadMachineTrust(context.Background()) {
		t.Fatal("machine trust could not be initialized")
	}
	starts := make(chan map[string]any, 1)
	modelsNode(t, h, MachineConfig{ID: "test-node"}, func(method string, p map[string]any) (any, error) {
		if method == "session.start" {
			starts <- p
			return map[string]any{"session": proto.Session{SessionID: "agy-new", Cwd: "/work", Launcher: "agy"}}, nil
		}
		return map[string]any{}, nil
	})
	connectAssistant(t, h)
	r := modelsRequest(h, "POST", "/api/v1/sessions", `{"machine_id":"test-node","cwd":"/work","launcher":"agy","client_msg_id":"start-photo","images":[{"mime":"image/png","data":"aW1hZ2U="}]}`)
	if r.Code != http.StatusOK {
		t.Fatalf("start: %d %s", r.Code, r.Body)
	}
	params := <-starts
	images, _ := params["images"].([]any)
	if params["client_msg_id"] != "start-photo" || params["prompt"] != "" || len(images) != 1 {
		t.Fatalf("node start: %+v", params)
	}
}
