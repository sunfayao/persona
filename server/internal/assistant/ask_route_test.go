package assistant

import (
	"testing"
)

// settleService waits until s's supervision messages due have gone out. Taking the digest lock for each observation
// covers restoration as well as delivery and closes the window where a finishing delivery arms its retry.
func settleService(t *testing.T, s *Service) {
	t.Helper()
	b := &s.supervise
	waitFor(t, "the supervision messages due to go out", func() bool {
		b.digestMu.Lock()
		defer b.digestMu.Unlock()
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.pending) == 0 && b.timer == nil
	})
}
