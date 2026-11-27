package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/bus"
	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// stateManager is a manager on the bus for the per-manager state: it answers http.request, devices.call,
// assistant.tools and the source moves, and keeps the events the hub sends it.
type stateManager struct {
	id   string
	peer *rpc.Peer
	ws   interface{ CloseNow() error }

	mu      sync.Mutex
	events  []string        // "event params"
	calls   []string        // "method params" of the source moves
	sources map[string]bool // the mail accounts it has
}

func connectStateManager(t *testing.T, h *Hub, srv *httptest.Server, id string, sources ...string) *stateManager {
	t.Helper()
	f := &stateManager{id: id, sources: map[string]bool{}}
	for _, s := range sources {
		f.sources[s] = true
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ws, err := bus.Dial(ctx, srv.URL, busToken(t, h, id, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	f.ws = ws
	conn := bus.NewConn(ctx, ws)
	f.peer = rpc.New("s", conn)
	f.peer.OnEvent = func(event string, params json.RawMessage) {
		f.mu.Lock()
		f.events = append(f.events, event+" "+string(params))
		f.mu.Unlock()
	}
	f.peer.OnRequest = f.answer
	go func() { _ = f.peer.Serve(ctx, conn) }()
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ccancel()
	if err := f.peer.Call(cctx, busHello, map[string]any{"id": id}, nil); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *stateManager) answer(_ context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case assistantwire.MethodHTTP:
		body, _ := json.Marshal(map[string]any{"who": f.id})
		return assistantwire.HTTPResponse{Status: http.StatusOK, Body: body}, nil
	case devicewire.MethodCall:
		return map[string]any{"who": f.id}, nil
	case assistantwire.MethodTools:
		return map[string]any{"tools": []proto.Tool{{Name: "tool_of_" + strings.ReplaceAll(f.id, "-", "_")}}}, nil
	case extwire.MethodSourceExport, extwire.MethodSourceImport, extwire.MethodSourceRelease:
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, method+" "+string(params))
		var ref extwire.SourceBundle
		_ = json.Unmarshal(params, &ref)
		switch method {
		case extwire.MethodSourceExport:
			if !f.sources[ref.Source] {
				return nil, &rpc.Error{Code: extwire.CodeNoSource, Message: "no such source"}
			}
			return extwire.SourceBundle{Source: ref.Source, Kind: "mail", Data: json.RawMessage(`{"password":"pw"}`)}, nil
		case extwire.MethodSourceImport:
			if f.sources[ref.Source] {
				return nil, &rpc.Error{Code: extwire.CodeSourceExists, Message: "has it"}
			}
			f.sources[ref.Source] = true
			return extwire.SourceRef{Source: ref.Source}, nil
		default:
			delete(f.sources, ref.Source)
			return map[string]any{}, nil
		}
	}
	return nil, &rpc.Error{Code: "no_such_thing", Message: "the fake manager has no " + method}
}

func (f *stateManager) notify(t *testing.T, event string, params any) {
	t.Helper()
	if err := f.peer.Notify(event, params); err != nil {
		t.Fatal(err)
	}
}

func (f *stateManager) call(t *testing.T, method string, params, out any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return f.peer.Call(ctx, method, params, out)
}

// lastConfig is the machines of the last devices.config the hub sent it ("" before one came).
func (f *stateManager) lastConfig() (ids []string, got bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.events) - 1; i >= 0; i-- {
		ev, params, _ := strings.Cut(f.events[i], " ")
		if ev != devicewire.EventConfig {
			continue
		}
		var cfg struct {
			Machines []struct {
				ID string `json:"id"`
			} `json:"machines"`
		}
		_ = json.Unmarshal([]byte(params), &cfg)
		for _, m := range cfg.Machines {
			ids = append(ids, m.ID)
		}
		return ids, true
	}
	return nil, false
}

func (f *stateManager) has(source string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sources[source]
}
