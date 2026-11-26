package gitworktree

import (
	"context"
	"strings"
)

// BranchCheckedOutAt is the worktree of dir's repository that has branch checked out, "" when none does.
func BranchCheckedOutAt(ctx context.Context, git func(context.Context, string, ...string) (string, error), dir, branch string) (string, error) {
	out, err := git(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	var at string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			at = p
		} else if line == "branch refs/heads/"+branch {
			return at, nil
		}
	}
	return "", nil
}
