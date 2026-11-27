package extmgr

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func personaHeaders(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, v := range r.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-persona-") {
			out[k] = strings.Join(v, ",")
		}
	}
	return out
}

// relayServer must keep idle relay streams open longer than the hub's relayIdleFor (90 s) so the hub retires them
// first instead of racing a peer-side idle close on a non-idempotent POST.
func TestRelayServerIdleTimeoutExceedsHubPool(t *testing.T) {
	h := New(Options{Dir: t.TempDir()})
	srv := h.relayServer()
	if srv.IdleTimeout <= 90*time.Second {
		t.Fatalf("relayServer IdleTimeout = %s, want > 90s", srv.IdleTimeout)
	}
	if srv.ReadHeaderTimeout != 30*time.Second {
		t.Fatalf("relayServer ReadHeaderTimeout = %s, want 30s", srv.ReadHeaderTimeout)
	}
}
