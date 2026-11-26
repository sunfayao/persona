package orca

import (
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/orcart"
)

func TestOrcaExplicitWorkspaceDoesNotFallBackToAnotherWorkspace(t *testing.T) {
	t.Parallel()
	h := New(Config{Refuse: func(_, message string, _ map[string]any) error { return errors.New(message) }})
	trees := []orcart.Worktree{{WorktreeID: "first", Path: "/work/shared"}, {WorktreeID: "chosen", Path: "/work/shared"}}
	got, err := h.resolveWorktree(t.Context(), trees, "chosen", "/work/shared/sub")
	if err != nil || got.WorktreeID != "chosen" {
		t.Fatalf("explicit workspace was not honored: %v, %v", got, err)
	}
	for _, target := range []struct{ id, cwd string }{{"gone", "/work/shared"}, {"chosen", "/elsewhere"}} {
		if got, err := h.resolveWorktree(t.Context(), trees, target.id, target.cwd); err == nil || got != nil {
			t.Fatalf("stale or incompatible selection silently moved to another workspace: %v, %v", got, err)
		}
	}
}
