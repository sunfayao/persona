package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postTodo posts body to a to-do write's path through h's routes, with op as X-Persona-Op-Id when set.
func postTodo(t *testing.T, ctx context.Context, h *Hub, mux http.Handler, path, op, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://hub.example.com"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	if op != "" {
		r.Header.Set("X-Persona-Op-Id", op)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}
