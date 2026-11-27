package node

import (
	"context"
	"path/filepath"
	"testing"
)

// A branch given with letters the slug drops (a Chinese name), or nothing to slug at all, still gets a folder of its
// own in the checkouts directory: never that directory itself, nor another branch's (fix/登录 and fix/注册 both slug
// to "fix"), as for agent_start (#5775).
func TestAgentWorktreeForABranchWithNoASCIIName(t *testing.T) {
	w := worktreeNode(t)
	ctx := context.Background()
	prep, err := w.n.worktreePrepare(ctx, "", "w1", "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, branch := range []string{"修复登录", "需求/重构", "fix/登录", "fix/注册", "_"} {
		a, err := w.n.worktreeAdd(ctx, "", w.repo, branch, "")
		if err != nil || filepath.Dir(a.Path) != prep.Path || branchOf(t, a.Path) != branch {
			t.Fatalf("%s: added %+v %v (checkouts under %s)", branch, a, err, prep.Path)
		}
		if p := seen[a.Path]; p != "" {
			t.Fatalf("%s: same folder as %s", branch, p)
		}
		seen[a.Path] = branch
	}
}
