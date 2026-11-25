package github

import (
	"net/http"
	"strconv"
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
