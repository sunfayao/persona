package hub

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/bus"
	"github.com/sunfayao/persona/server/internal/db"
)

// flakyIdents is memIdents whose lookups fail while down is set (Postgres restarting).
type flakyIdents struct {
	memIdents
	down atomic.Bool
}

func (f *flakyIdents) BusIdentityByHash(ctx context.Context, hash string) (*db.BusIdentity, error) {
	if f.down.Load() {
		return nil, errors.New("connection refused")
	}
	return f.memIdents.BusIdentityByHash(ctx, hash)
}

// While the store is down the bus answers 503 without asking it again for a moment (one lookup for a burst of dials),
// logs one line a minute, and counts nothing against the source: two managers behind one address reconnecting
// through an outage must not lock that address (the user's phone on it too) out of the apps' API.
func TestBusStoreErrorIsThrottled(t *testing.T) {
	h := newTestHub(t)
	idents := &countingIdents{}
	idents.down.Store(true)
	h.bus.idents = idents
	h.cfg.Security.AuthFailures = 3
	var lines atomic.Int32
	h.logger = log.New(writerFunc(func(p []byte) (int, error) {
		if strings.Contains(string(p), "looking the token up") {
			lines.Add(1)
		}
		return len(p), nil
	}), "", 0)
	mux := http.NewServeMux()
	h.busRoutes(mux)
	for i := range 20 { // two managers, ten dials each, from one address
		w := securityRequest(mux, "GET", bus.Path, "jb_000"+string(rune('a'+i%2)), "", "198.51.100.1:1", true)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("dial %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if n := idents.lookups.Load(); n != 1 {
		t.Fatalf("%d lookups for a burst of dials", n)
	}
	if n := lines.Load(); n != 1 {
		t.Fatalf("%d log lines", n)
	}
	r := httptest.NewRequest("GET", bus.Path, nil)
	r.RemoteAddr = "198.51.100.1:1"
	if retry := h.devtok.failures.retry(authSourceKey(r), time.Now(), h.cfg.Security); retry > 0 {
		t.Fatalf("the address is locked out of the apps' API for %s", retry)
	}
}

// countingIdents is flakyIdents counting its lookups.
type countingIdents struct {
	flakyIdents
	lookups atomic.Int32
}

func (c *countingIdents) BusIdentityByHash(ctx context.Context, hash string) (*db.BusIdentity, error) {
	c.lookups.Add(1)
	return c.flakyIdents.BusIdentityByHash(ctx, hash)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
