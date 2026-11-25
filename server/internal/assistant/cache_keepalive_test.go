package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCacheKeepaliveSettingsDefaultsValidationAndPartialPUT(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	ctx := context.Background()
	if p := cacheKeepaliveOf(s.settings(ctx)); !p.Enabled || p.IntervalMinutes != 55 || p.MaxRequests != 24 {
		t.Fatalf("defaults %+v", p)
	}
	put := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{"cache_keepalive_minutes":0}`, `{"cache_keepalive_minutes":56}`, `{"cache_keepalive_max":0}`, `{"cache_keepalive_max":145}`, `{"cache_keepalive":"false"}`, `{"cache_keepalive_minutes":1.5}`} {
		if w := put(body); w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	w := put(`{"cache_keepalive":false,"cache_keepalive_minutes":30,"cache_keepalive_max":12}`)
	if w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view["cache_keepalive"] != false || view["cache_keepalive_minutes"] != float64(30) || view["cache_keepalive_max"] != float64(12) {
		t.Fatal(view)
	}
	if w := put(`{"notes":"example"}`); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if p := cacheKeepaliveOf(s.settings(ctx)); p.Enabled || p.IntervalMinutes != 30 || p.MaxRequests != 12 {
		t.Fatalf("partial reset %+v", p)
	}
}
