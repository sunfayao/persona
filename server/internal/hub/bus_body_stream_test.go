package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/rpc"
)

// bodyManager is the assistant on a real bus connection, answering the forwarded requests: http.request as it
// comes, http.request_streamed with the body read from the relay (bus.ReadBody, as the assistant's BusHub does)
// unless turn says to turn it down with a code.
type bodyManager struct {
	mu        sync.Mutex
	got       []assistantwire.HTTPRequest // as the route would see them: Body filled, BodyStream as it came
	how       []string                    // the method each came with
	turn      string                      // "": take the stream; else answer http.request_streamed with this code
	onRequest func()
}

func (m *bodyManager) requests() ([]string, []assistantwire.HTTPRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.how...), append([]assistantwire.HTTPRequest{}, m.got...)
}

// connectBodyManager connects m as the assistant to the hub at srv over client (nil: plain), naming the body
// stream's capability in its hello when streams is set.
func connectBodyManager(t *testing.T, h *Hub, srv *httptest.Server, client *http.Client, streams bool, m *bodyManager) {
	t.Helper()
	token := busToken(t, h, assistantwire.AssistantID, "assistant")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ws, err := bus.Dial(ctx, srv.URL, token, client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	conn := bus.NewConn(ctx, ws)
	peer := rpc.New("m", conn)
	peer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != assistantwire.MethodHTTP && method != assistantwire.MethodHTTPStreamed {
			return map[string]any{}, nil
		}
		var p assistantwire.HTTPRequest
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		m.mu.Lock()
		notify := m.onRequest
		m.mu.Unlock()
		if notify != nil {
			notify()
		}
		if method == assistantwire.MethodHTTPStreamed {
			m.mu.Lock()
			turn := m.turn
			m.mu.Unlock()
			if turn != "" {
				m.mu.Lock()
				m.how, m.got = append(m.how, method+" turned down"), append(m.got, p)
				m.mu.Unlock()
				return nil, &rpc.Error{Code: turn, Message: "turned down"}
			}
			c, err := bus.DialAt(ctx, srv.URL, token, client, devicewire.RelayPath, url.Values{"id": {p.BodyStream.ID}}.Encode())
			if err != nil {
				return nil, &rpc.Error{Code: assistantwire.CodeBodyIncomplete, Message: err.Error()}
			}
			if p.Body, err = bus.ReadBody(ctx, c, p.BodyStream.Size); err != nil {
				return nil, &rpc.Error{Code: assistantwire.CodeBodyIncomplete, Message: err.Error()}
			}
		}
		m.mu.Lock()
		m.how, m.got = append(m.how, method), append(m.got, p)
		m.mu.Unlock()
		return assistantwire.HTTPResponse{Status: http.StatusOK, Header: map[string][]string{"Content-Type": {"application/json"}},
			Body: []byte(`{"size":` + jsonInt(len(p.Body)) + `}`)}, nil
	}
	go func() { _ = peer.Serve(ctx, conn) }()
	hello := map[string]any{"id": assistantwire.AssistantID, "capabilities": []string{assistantwire.CapabilityConfigOwn}}
	if streams {
		hello["capabilities"] = []string{assistantwire.CapabilityConfigOwn, assistantwire.CapabilityHTTPBodyStream}
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hcancel()
	if err := peer.Call(hctx, busHello, hello, nil); err != nil {
		t.Fatal(err)
	}
}

func jsonInt(n int) string { b, _ := json.Marshal(n); return string(b) }

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

// putPlace is an app's upload of body on one of the assistant's routes, with the hub's own token.
func putPlace(t *testing.T, h *Hub, srv *httptest.Server, id string, body []byte) (int, string) {
	t.Helper()
	r, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/assistant/memory/"+id+"?x=1", bytes.NewReader(body))
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
	code, small := putPlace(t, h, srv, "r1", []byte(`{}`))
	large := append([]byte(`{"pad":"`), bytes.Repeat([]byte("a"), bodyStreamMin)...)
	large = append(large, `"}`...)
	if c, out := putPlace(t, h, srv, "r1", large); c != code || out != small {
		t.Fatalf("large: %d %s, small: %d %s", c, out, code, small)
	}
	eventually(t, 5*time.Second, "the relay let go", func() bool { return relaysLeft(h) == 0 })
	if strings.Contains(logs.String(), "turned down") {
		t.Fatalf("the assistant turned the stream down:\n%s", logs.String())
	}
}
