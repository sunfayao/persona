package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/extwire"
)

func TestA2AScopedCredentialStreamsOnlyA2A(t *testing.T) {
	entered := make(chan http.Header, 1)
	h, _, srv := relayTestHubServing(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {}\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
		<-r.Context().Done()
	}))
	code, out := call(t, srv.Config.Handler, "POST", "/api/v1/auth/tokens", h.cfg.Token,
		`{"name":"A2A integration","kind":"other","scopes":["agents.a2a"]}`)
	if code != http.StatusOK || out["role"] != roleScoped {
		t.Fatalf("issue scoped credential: %d %v", code, out)
	}
	token := out["token"].(string)
	for _, path := range []string{"/api/v1/sessions", "/api/v1/assistant/leaders"} {
		if status, _ := call(t, srv.Config.Handler, "GET", path, token, ""); status != http.StatusForbidden {
			t.Fatalf("an A2A token opened %s: %d", path, status)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api/v1/a2a/computer/session/rpc", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("A2A-Version", "1.0")
	req.Header.Set(extwire.HeaderRole, roleFull)
	req.Header.Set(extwire.HeaderDevice, "forged")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("stream response %d: %v", resp.StatusCode, body)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "data: {}\n" {
		t.Fatalf("first frame: %q %v", line, err)
	}
	headers := <-entered
	if headers.Get(extwire.HeaderRole) != roleScoped || headers.Get(extwire.HeaderDevice) != out["id"] ||
		headers.Get(extwire.HeaderScopes) != assistantwire.ScopeA2A || headers.Get("Authorization") != "" ||
		headers.Get("A2A-Version") != "1.0" {
		t.Fatalf("relayed headers: %v", headers)
	}
}
