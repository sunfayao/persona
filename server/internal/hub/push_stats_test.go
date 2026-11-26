package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// statsRequest serves one GET against the push grading routes with an optional principal (nil is a full credential).
func statsRequest(t *testing.T, h *Hub, p *principal, target string) (int, []byte) {
	t.Helper()
	mux := http.NewServeMux()
	h.pushGradingRoutes(mux)
	r := httptest.NewRequest("GET", target, nil)
	if p != nil {
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, *p))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}

func extPrincipal(manager string) *principal {
	return &principal{Role: roleExtension, Manager: manager}
}

// gradingZonedClock installs one race-free clock whose instant and UTC offset can move independently.
func gradingZonedClock(h *Hub) (instant, offset *atomic.Int64) {
	instant, offset = &atomic.Int64{}, &atomic.Int64{}
	instant.Store(time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC).UnixNano())
	h.push.now = func() time.Time {
		return time.Unix(0, instant.Load()).In(time.FixedZone("", int(offset.Load())))
	}
	return instant, offset
}
