package codex

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestCodexHierarchyCycleTerminates(t *testing.T) {
	t.Parallel()
	list := []proto.Session{{SessionID: "a", ParentSessionID: "b", IsSubagent: true}, {SessionID: "b", ParentSessionID: "a", IsSubagent: true}}
	ResolveHierarchy(list)
	for _, s := range list {
		if s.RootSessionID != "" {
			t.Fatalf("cycle has root: %+v", s)
		}
	}
}
