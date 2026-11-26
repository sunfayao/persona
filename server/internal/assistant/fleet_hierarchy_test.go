package assistant

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestFleetDecorationPreservesCodexHierarchy(t *testing.T) {
	ft := newFleetTestNoStore(t)
	child := proto.Session{SessionID: "child", Agent: "codex", SourceKind: "codex_app_server", Status: proto.StatusWorking,
		ParentThreadID: "raw-root", ParentSessionID: "root", RootSessionID: "root", IsSubagent: true, RepoRoot: "/code/project"}
	ft.d.setSessions("mac", []proto.Session{
		{SessionID: "root", Agent: "codex", Status: proto.StatusWorking}, child,
	})
	got := listed(t, ft.s, "mac", "child")
	if got.ParentThreadID != child.ParentThreadID || got.ParentSessionID != child.ParentSessionID || got.RootSessionID != child.RootSessionID || !got.IsSubagent || got.RepoRoot != child.RepoRoot {
		t.Fatalf("decoration lost source hierarchy: %+v", got)
	}
}
