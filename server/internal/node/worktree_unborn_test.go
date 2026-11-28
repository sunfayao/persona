package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A folder used as a plain working directory is often a repository nobody has committed to: HEAD names an unborn
// branch. A worktree of it has nothing to start from, so neither the dialog nor a task may take the new-checkout path
// there, and the refusal is a plain sentence with a code the hub acts on, not git's "invalid reference" (#5548).

// unbornRepo is a repository with an unborn main and an untracked file, the way a never-committed project folder looks.
func unbornRepo(t *testing.T, dir string) string {
	t.Helper()
	repo := mkdir(t, dir)
	runPullGit(t, repo, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("draft\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func assertNoCommits(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil || rpcCode(err) != "worktree_no_commits" || strings.Contains(err.Error(), "invalid reference") ||
		err.Error() == "" {
		t.Fatalf("%s on a repository without commits = %v (code %q)", what, err, rpcCode(err))
	}
}

func TestHerdrWorktreeRefusesARepositoryWithoutCommits(t *testing.T) {
	w := worktreeNode(t)
	setWorktreeSupport(t, false) // the git fallback: `git worktree add` would fail with "invalid reference: HEAD"
	runPullGit(t, w.repo, "checkout", "--orphan", "fresh")
	runPullGit(t, w.repo, "branch", "-D", "main", "spare")
	_, err := request(t, w.n, "worktree.prepare", map[string]any{"workspace_id": "w1"})
	assertNoCommits(t, "prepare", err)
	_, err = request(t, w.n, "worktree.create", map[string]any{"workspace_id": "w1", "branch": "task/one",
		"path": filepath.Join(w.home, "trees", "task-one")})
	assertNoCommits(t, "create", err)
	if got := w.f.trace("worktree.create", "workspace.create"); len(got) != 0 {
		t.Fatalf("a workspace was opened for a repository without commits: %v", got)
	}
}

// Commits on any ref are enough: HEAD on an orphan branch still has something to check out, so it is not refused as
// "no commits".
func TestWorktreeWithCommitsElsewhereIsNotRefused(t *testing.T) {
	w := worktreeNode(t)
	runPullGit(t, w.repo, "checkout", "--orphan", "gh-pages")
	if _, err := request(t, w.n, "worktree.prepare", map[string]any{"workspace_id": "w1"}); err != nil {
		t.Fatalf("prepare with HEAD on an orphan branch: %v", err)
	}
}

// The refusal is worded in the caller's locale.
func TestWorktreeNoCommitsFollowsTheCallersLocale(t *testing.T) {
	w := worktreeNode(t)
	empty := unbornRepo(t, filepath.Join(w.home, "scratchpad"))
	for locale, want := range map[string]string{"en": "has no commits yet", "ja": "コミットがない"} {
		_, err := request(t, w.n, "worktree.prepare", map[string]any{"cwd": empty, "locale": locale})
		assertNoCommits(t, "prepare in "+locale, err)
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("prepare in %s: %v", locale, err)
		}
	}
}
