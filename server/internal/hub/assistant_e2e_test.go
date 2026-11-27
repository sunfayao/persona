package hub

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testApp is an app on the hub's WebSocket, as the phone connects.
type testApp struct {
	msgs chan map[string]any // each message, with its bytes as "raw"
}

func dialTestApp(t *testing.T, h *Hub, srv *httptest.Server) *testApp {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, strings.Replace(srv.URL, "http", "ws", 1)+"/api/v1/ws?token="+h.cfg.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadLimit(64 << 20)
	t.Cleanup(func() { _ = conn.CloseNow() })
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"client_hello","features":["ws_notify"]}`)); err != nil {
		t.Fatal(err)
	}
	a := &testApp{msgs: make(chan map[string]any, 1024)}
	go func() {
		for {
			_, b, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				m["raw"] = string(b)
				select {
				case a.msgs <- m:
				default:
				}
			}
		}
	}()
	return a
}

// wait is the next message the app gets that ok accepts (the others are skipped).
func (a *testApp) wait(t *testing.T, what string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-a.msgs:
			if ok(m) {
				return m
			}
		case <-deadline:
			t.Fatalf("the app did not get %s", what)
		}
	}
}
