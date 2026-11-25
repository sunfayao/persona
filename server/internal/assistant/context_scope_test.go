package assistant

import (
	"context"
	"testing"
)

func scopedContext(t *testing.T, s *Service, id string) string {
	t.Helper()
	return s.Context(context.Background(), ContextParams{SessionID: id}).Text
}
