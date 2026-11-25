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

	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

// tokenStore keeps device tokens in memory, like the device_tokens table.
type tokenStore struct {
	memStore
	mu   sync.Mutex
	rows map[string]db.DeviceToken // hash -> token
	rev  map[string]bool
}

func (s *tokenStore) CreateDeviceToken(_ context.Context, t db.DeviceToken, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.CreatedAt = time.Now()
	s.rows[hash] = t
	return nil
}

func (s *tokenStore) DeviceTokenByHash(_ context.Context, hash string) (*db.DeviceToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.rows[hash]
	if !ok || s.rev[t.ID] {
		return nil, nil
	}
	return &t, nil
}

func (s *tokenStore) DeviceTokens(context.Context) ([]db.DeviceToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []db.DeviceToken
	for _, t := range s.rows {
		out = append(out, t)
	}
	return out, nil
}

func (s *tokenStore) RevokeDeviceToken(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, t := range s.rows {
		if t.ID == id && !s.rev[id] {
			s.rev[id] = true
			now := time.Now()
			t.RevokedAt = &now
			s.rows[hash] = t
			return true, nil
		}
	}
	return false, nil
}

func (s *tokenStore) TouchDeviceToken(context.Context, string) error { return nil }

func TestDeviceRevocationReachesEveryManager(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{rows: map[string]db.DeviceToken{
		"first": {ID: "dev_first"},
		"other": {ID: "dev_other"},
	}, rev: map[string]bool{}}
	h.bus.idents = &memIdents{}
	mux := http.NewServeMux()
	h.busRoutes(mux)
	h.tokenRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	events := make(chan bus.DeviceCredential, 2)
	for _, id := range []string{"assistant", "another-manager"} {
		m := connectGuardManager(t, h, srv.URL, id, func(string, json.RawMessage) (any, error) { return map[string]any{}, nil })
		m.mu.Lock()
		m.onEvent = func(event string, raw json.RawMessage) {
			if event == bus.EventCredentialRevoked {
				var p bus.DeviceCredential
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Error(err)
				}
				events <- p
			}
		}
		m.mu.Unlock()
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/auth/tokens/dev_first", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sign out: %d %s", w.Code, w.Body)
	}
	for range 2 {
		select {
		case p := <-events:
			if p.ID != "dev_first" {
				t.Fatalf("revoked credential: %+v", p)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a manager missed the revocation event")
		}
	}

	// A fresh hub has no in-memory revocation and can still answer a manager that missed the event.
	fresh := newTestHub(t)
	fresh.store = h.store
	fresh.bus.idents = &memIdents{}
	freshMux := http.NewServeMux()
	fresh.busRoutes(freshMux)
	freshSrv := httptest.NewServer(freshMux)
	t.Cleanup(freshSrv.Close)
	m := connectGuardManager(t, fresh, freshSrv.URL, "assistant", func(string, json.RawMessage) (any, error) { return map[string]any{}, nil })
	for credential, want := range map[string]bool{"dev_first": true, "dev_other": false} {
		var status bus.CredentialStatus
		if err := m.call(t, bus.MethodCredentialRevoked, bus.DeviceCredential{ID: credential}, &status); err != nil || status.Revoked != want {
			t.Fatalf("status for %s: %+v, %v", credential, status, err)
		}
	}
}

// pausedTokenStore returns the snapshot read before revocation only after the test releases it.
type pausedTokenStore struct {
	*tokenStore
	read    chan struct{}
	release chan struct{}
}

func (s *pausedTokenStore) DeviceTokenByHash(ctx context.Context, hash string) (*db.DeviceToken, error) {
	dt, err := s.tokenStore.DeviceTokenByHash(ctx, hash)
	if dt != nil && err == nil {
		close(s.read)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return dt, err
}

func TestDeviceTokenLookupConcurrentRevocation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "cold cache"
		if expired {
			name = "expired cache"
		}
		t.Run(name, func(t *testing.T) {
			h := newTestHub(t)
			const token, id = "jd_inflight", "dev_inflight"
			hash := hashToken(token)
			s := &pausedTokenStore{
				tokenStore: &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{
					hash: {ID: id, Name: "phone", Role: roleFull},
				}, rev: map[string]bool{}},
				read: make(chan struct{}), release: make(chan struct{}),
			}
			h.store = s
			if expired {
				h.devtok.cache = map[string]cachedToken{
					hash: {p: &principal{ID: id, Role: roleFull}, at: time.Now().Add(-2 * tokenCacheFor)},
				}
			}
			release := sync.OnceFunc(func() { close(s.release) })
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			type result struct {
				p  principal
				ok bool
			}
			done := make(chan result, 1)
			go func() {
				p, ok := h.authenticate(ctx, token)
				done <- result{p, ok}
			}()
			select {
			case <-s.read:
			case <-ctx.Done():
				t.Fatal("lookup did not reach the snapshot barrier")
			}
			if ok, err := s.RevokeDeviceToken(ctx, id); err != nil || !ok {
				t.Fatalf("revoke: %v, %v", ok, err)
			}
			h.revoked(id)
			release()
			select {
			case got := <-done:
				if got.ok || got.p.ID != "" || got.p.Role != "" {
					t.Fatalf("stale lookup authenticated revoked device: %+v", got)
				}
			case <-ctx.Done():
				t.Fatal("lookup did not finish")
			}
			h.devtok.mu.Lock()
			_, cached := h.devtok.cache[hash]
			h.devtok.mu.Unlock()
			if cached {
				t.Fatal("stale lookup repopulated the cache after revocation")
			}
			if _, ok := h.authenticate(ctx, token); ok {
				t.Fatal("subsequent lookup authenticated revoked device")
			}
		})
	}
}

func TestDeviceRevocationRetention(t *testing.T) {
	h := newTestHub(t)
	h.devtok.revokedIDs = map[string]time.Time{
		"dev_old":    time.Now().Add(-2 * tokenRevokedFor),
		"dev_recent": time.Now().Add(-time.Minute),
	}
	if h.isDeviceRevoked("dev_old") || h.isDeviceRevoked("dev_unknown") || h.isDeviceRevoked("") {
		t.Fatal("expired, unknown or master credentials marked revoked")
	}
	h.revoked("dev_new")
	if !h.isDeviceRevoked("dev_new") || !h.isDeviceRevoked("dev_recent") {
		t.Fatal("recent revocations were not retained")
	}
	h.devtok.mu.Lock()
	_, retained := h.devtok.revokedIDs["dev_old"]
	h.devtok.mu.Unlock()
	if retained {
		t.Fatal("expired revocation was not pruned")
	}
}

func call(t *testing.T, h http.Handler, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestDeviceTokens(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	mux := h.routes()
	master := h.cfg.Token

	code, web := call(t, mux, "POST", "/api/v1/auth/tokens", master, `{"name":"Browser","kind":"web"}`)
	if code != 200 || web["role"] != "full" || !strings.HasPrefix(web["token"].(string), "jd_") {
		t.Fatalf("%d %v", code, web)
	}
	webTok := web["token"].(string)
	// A watch's answer-only credential is not issued any more, and not as a full one in its place.
	if code, out := call(t, mux, "POST", "/api/v1/auth/tokens", webTok, `{"name":"Watch","kind":"watch"}`); code != http.StatusBadRequest || out["code"] != "invalid_kind" {
		t.Fatalf("a watch credential: %d %v", code, out)
	}

	code, mine := call(t, mux, "GET", "/api/v1/auth/tokens", webTok, "")
	if code != 200 || mine["current"] != web["id"] {
		t.Fatalf("%d %v", code, mine)
	}

	// Revoking the browser shuts it out at once; the pairing token keeps working.
	if code, _ := call(t, mux, "DELETE", "/api/v1/auth/tokens/"+web["id"].(string), master, ""); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/interactions", webTok, ""); code != http.StatusUnauthorized {
		t.Fatalf("revoked browser: %d", code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/interactions", "jd_made_up", ""); code != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", code)
	}
	code, list := call(t, mux, "GET", "/api/v1/auth/tokens", master, "")
	if code != 200 || len(list["tokens"].([]any)) != 1 {
		t.Fatalf("%d %v", code, list)
	}
}

// A token that still has the retired answer-only role (a watch's) opens nothing.
func TestRetiredAnswerRoleOpensNothing(t *testing.T) {
	p := principal{ID: "dev_watch", Role: roleRetiredAnswer}
	for _, c := range [][2]string{{"GET", "/api/v1/machines"}, {"GET", "/api/v1/interactions"}, {"POST", "/api/v1/interactions/x/resolve"},
		{"POST", "/api/v1/sessions/m/s/messages"}, {"PUT", "/api/v1/devices/x/widgets"}} {
		if allowed(p, c[0], c[1]) {
			t.Errorf("%s %s is allowed", c[0], c[1])
		}
	}
}

// A browser signing in again names its earlier credential and the hub takes that one off, so the browser stays one
// device; an id that is not a web credential (a phone's) is left alone, and an old client without "replaces" still
// just gets a new one.
func TestWebSignInReplacesItsEarlierDevice(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	mux := h.routes()
	master := h.cfg.Token

	_, phone := call(t, mux, "POST", "/api/v1/auth/tokens", master, `{"name":"iPhone","kind":"phone"}`)
	code, first := call(t, mux, "POST", "/api/v1/auth/tokens", master, `{"name":"网页（Chrome · Mac · 11月6日）","kind":"web"}`)
	if code != 200 || first["replaced"] != nil {
		t.Fatalf("first sign-in: %d %v", code, first)
	}
	firstTok := first["token"].(string)

	code, second := call(t, mux, "POST", "/api/v1/auth/tokens", master,
		`{"name":"网页（Chrome · Mac · 11月6日）","kind":"web","replaces":"`+first["id"].(string)+`"}`)
	if code != 200 || second["replaced"] != first["id"] || second["id"] == first["id"] {
		t.Fatalf("signing in again: %d %v", code, second)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/interactions", firstTok, ""); code != http.StatusUnauthorized {
		t.Fatalf("the replaced credential still works: %d", code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/machines", second["token"].(string), ""); code != 200 {
		t.Fatalf("the new credential: %d", code)
	}

	// A phone's id is not the browser's to take off.
	code, third := call(t, mux, "POST", "/api/v1/auth/tokens", master, `{"name":"网页","kind":"web","replaces":"`+phone["id"].(string)+`"}`)
	if code != 200 || third["replaced"] != nil {
		t.Fatalf("replacing a phone: %d %v", code, third)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/machines", phone["token"].(string), ""); code != 200 {
		t.Fatalf("the phone was taken off: %d", code)
	}

	code, list := call(t, mux, "GET", "/api/v1/auth/tokens", master, "")
	kinds := map[string]int{}
	for _, it := range list["tokens"].([]any) {
		kinds[it.(map[string]any)["kind"].(string)]++
	}
	if code != 200 || kinds["web"] != 3 || kinds["other"] != 1 {
		t.Fatalf("kinds %v", kinds)
	}
}

// retiredWatch is the credential of a watch paired before the answer-only role was dropped: a token of the retired
// role, which the hub refuses everywhere. It returns what POST /api/v1/auth/tokens used to: id, token and role.
func retiredWatch(t *testing.T, h *Hub) map[string]any {
	t.Helper()
	tok := "jd_" + randomHex(24)
	row := db.DeviceToken{ID: "dev_" + randomHex(6), Name: "Watch", Kind: "watch", Role: roleRetiredAnswer}
	if err := h.store.CreateDeviceToken(context.Background(), row, hashToken(tok)); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": row.ID, "token": tok, "role": roleRetiredAnswer, "name": "Watch"}
}
