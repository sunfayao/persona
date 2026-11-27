package hub

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// The live screen's "Allow again" reaches the Mac's node only from a full device, for a Mac whose screen is shared;
// the node's answer comes back as it is, and a node without the method or without the helper is a 409 that says so.
func TestScreenPermissionRelaysTheUsersAllowAgain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	var mu sync.Mutex
	var calls []string
	answer := func() (any, error) {
		return proto.ScreenVideo{Version: 1, Permission: "missing", Asked: true}, nil
	}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		mu.Lock()
		calls = append(calls, method+" "+string(params))
		a := answer
		mu.Unlock()
		return a()
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { _ = h2nW.Close(); _ = n2hW.Close() })
	h := &Hub{byID: map[string]*Machine{}, logger: log.New(io.Discard, "", 0)}
	m := newMachine(h, MachineConfig{ID: "m1", Name: "Mac"})
	m.peer = hubSide
	h.byID["m1"] = m
	post := func(role string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/machines/m1/screen/permission", nil)
		r.SetPathValue("mid", "m1")
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: role, ID: "dev"}))
		w := httptest.NewRecorder()
		h.withMachine(h.screenPermission)(w, r)
		return w
	}

	if w := post(roleFull); w.Code != http.StatusForbidden {
		t.Fatalf("a Mac whose screen is not shared: %d", w.Code)
	}
	h.ext.report.Store(&extwire.Report{Machines: map[string]extwire.MachineExtras{"m1": {ScreenPort: true}}})
	for _, role := range []string{roleRetiredAnswer, roleExtension, roleLink} {
		if w := post(role); w.Code != http.StatusForbidden {
			t.Errorf("role %s: %d, want 403", role, w.Code)
		}
	}
	mu.Lock()
	if len(calls) != 0 {
		t.Fatalf("a refused request reached the node: %q", calls)
	}
	mu.Unlock()

	w := post(roleFull)
	var got proto.ScreenVideo
	if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil || got.Permission != "missing" || !got.Asked {
		t.Fatalf("full device: %d %s", w.Code, w.Body.String())
	}
	mu.Lock()
	if len(calls) != 1 || calls[0] != proto.MethodScreenPermission+" {}" {
		t.Fatalf("the node was called with %q", calls)
	}
	mu.Unlock()

	for code, fail := range map[string]error{
		proto.CodeNodeOutdated: &rpc.Error{Message: "unknown method " + proto.MethodScreenPermission},
		"video_unavailable":    &rpc.Error{Code: "video_unavailable", Message: "no live-screen video helper on this node"},
	} {
		mu.Lock()
		answer = func() (any, error) { return nil, fail }
		mu.Unlock()
		w := post(roleFull)
		var body struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusConflict || body.Code != code || !strings.Contains(body.Error, "Mac") || strings.Contains(body.Error, "no live-screen video helper") {
			t.Errorf("%s: %d %s", code, w.Code, w.Body.String())
		}
	}
}
