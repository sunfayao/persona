package github

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// quotaAnswer is the quota headers of one answer; an empty resource leaves X-RateLimit-Resource out.
func quotaAnswer(h http.Header, resource string, left, limit int, reset time.Time) {
	if resource != "" {
		h.Set("X-RateLimit-Resource", resource)
	}
	h.Set("X-RateLimit-Limit", strconv.Itoa(limit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(left))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
}

// The core quota is still tracked: a low core answer, or one without X-RateLimit-Resource (older or proxied answers),
// marks it low; quotas the connector does not track change nothing.
func TestNoteRateByResource(t *testing.T) {
	reset := time.Now().Add(time.Hour)
	for _, tc := range []struct {
		resource           string
		left, limit        int
		coreLow, searchLow bool
	}{
		{"core", 100, 5000, true, false},
		{"", 100, 5000, true, false},
		{"core", 4900, 5000, false, false},
		{"search", 2, 30, false, true},
		{"graphql", 10, 5000, false, false},
	} {
		c := New(Config{Auth: AuthConfig{FromMachine: "mac"}}, "Mac", nil)
		h := http.Header{}
		quotaAnswer(h, tc.resource, tc.left, tc.limit, reset)
		c.noteRate(h)
		if c.ratesLow() != tc.coreLow || c.searchQuotaLow() != tc.searchLow {
			t.Errorf("%q %d/%d: core low %v, search low %v", tc.resource, tc.left, tc.limit, c.ratesLow(), c.searchQuotaLow())
		}
	}
}
