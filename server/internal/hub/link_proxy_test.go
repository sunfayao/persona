package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sunfayao/persona/server/internal/link"
)

// A link request without TLS: from this machine, or from the LAN when a reverse proxy says it took it over HTTPS;
// never from the internet, whatever the headers say.
func TestPlainLinkOnlyThroughAProxy(t *testing.T) {
	h, _, _ := newLinkHub(t)
	token, _ := linkToken(t, h, "laptop")
	req := func(remote string, header map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", link.Path, nil)
		r.RemoteAddr = remote
		r.Header.Set("Authorization", "Bearer "+token)
		for k, v := range header {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.routes().ServeHTTP(w, r)
		return w
	}
	tlsRequired := func(w *httptest.ResponseRecorder) bool {
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code == http.StatusForbidden && body["code"] == "tls_required"
	}
	for _, tc := range []struct {
		name, remote string
		header       map[string]string
		refused      bool
	}{
		{"LAN, no proxy", "[fd00::20]:5000", nil, true},
		{"LAN, a proxy that took plain HTTP", "[fd00::20]:5000", map[string]string{"X-Forwarded-Proto": "http"}, true},
		{"LAN, through an HTTPS proxy", "[fd00::20]:5000", map[string]string{"X-Forwarded-Proto": "https"}, false},
		{"LAN, Forwarded proto=https", "[fd00::21]:5000", map[string]string{"Forwarded": `for=192.0.2.1;proto=https`}, false},
		{"loopback (a proxy beside the hub)", "127.0.0.1:5000", nil, false},
		{"internet, claiming HTTPS", "203.0.113.9:5000", map[string]string{"X-Forwarded-Proto": "https"}, true},
	} {
		if w := req(tc.remote, tc.header); tlsRequired(w) != tc.refused {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
	// The apps' API from the internet stays refused in plaintext whatever the headers say.
	r := httptest.NewRequest("GET", "/api/v1/machines", nil)
	r.RemoteAddr, r.Header = "203.0.113.9:5000", http.Header{"X-Forwarded-Proto": {"https"}, "Authorization": {"Bearer " + h.cfg.Token}}
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, r)
	requireCode(t, w, http.StatusForbidden, "tls_required")
}
