package assistant

import (
	"net/http/httptest"
	"strings"
)

// placeDo sends one request to s's routes, with role as its device's role ("" for full).
func placeDo(s *Service, role, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if role != "" {
		req.Header.Set(RoleHeader, role)
	}
	s.ServeHTTP(rec, req)
	return rec
}
