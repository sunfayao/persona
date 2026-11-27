package hub

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

// bodyHub is a hub without a database, its routes served, identities in memory.
func bodyHub(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	h := newTestHub(t)
	h.bus.idents = &memIdents{}
	srv := httptest.NewServer(h.routes())
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		for _, c := range h.stopBus() {
			_ = c.ws.CloseNow()
		}
	})
	return h, srv
}

// putAssistantSettingsBody is an app's upload of body on one of the assistant's routes, with the hub's own token.
func putAssistantSettingsBody(t *testing.T, h *Hub, srv *httptest.Server, body []byte) (int, string) {
	t.Helper()
	r, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/assistant?x=1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	r.Header.Set("Content-Type", "application/json")
	res, err := srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

// relaysLeft is how many relays the hub still holds for a manager or has handed to one.
func relaysLeft(h *Hub) int {
	h.relay.mu.Lock()
	defer h.relay.mu.Unlock()
	return len(h.relay.pending) + len(h.relay.owner) + len(h.relay.taken)
}

// The assistant names the capability, and a large upload to one of its routes reaches the route: it answers it as
// it answers a small one.
func TestAssistantTakesBodyStreams(t *testing.T) {
	h, srv := bodyHub(t)
	logs := &syncBuffer{}
	h.logger = log.New(logs, "", 0)
	connectAssistant(t, h)
	if !h.managerCan(assistantwire.AssistantID, assistantwire.CapabilityHTTPBodyStream) {
		t.Fatal("the assistant's hello does not name the body stream")
	}
	code, small := putAssistantSettingsBody(t, h, srv, []byte(`{}`))
	large := append([]byte(`{"pad":"`), bytes.Repeat([]byte("a"), bodyStreamMin)...)
	large = append(large, `"}`...)
	if c, out := putAssistantSettingsBody(t, h, srv, large); c != code || out != small {
		t.Fatalf("large: %d %s, small: %d %s", c, out, code, small)
	}
	eventually(t, 5*time.Second, "the relay let go", func() bool { return relaysLeft(h) == 0 })
	if strings.Contains(logs.String(), "turned down") {
		t.Fatalf("the assistant turned the stream down:\n%s", logs.String())
	}
}
