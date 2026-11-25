package assistant

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// assistantHome is where the node's sessions are the assistant's conversations.
const assistantHome = "/home/u/.persona/assistant"

// routeEnv is a manager with one task, "修登录崩溃", handed out by leader conversation lead1 (unless leaders is false: then
// the main conversation conv1 supervises it and no leader is configured), its agents s1 and s2 on mac.
type routeEnv struct {
	*fleetTest
	todo   Todo
	agents []proto.Session // mac's sessions
}

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
