package gitworktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/pathutil"
)

type testProjectClock struct{ at time.Time }

func (c *testProjectClock) Now() time.Time { return c.at }

func (*testProjectClock) WithTimeout(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}

func testGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func TestCodexProjectRootRealWorktrees(t *testing.T) {
	t.Parallel()
	roots := NewProjectRoots(testGit, &testProjectClock{})
	base := t.TempDir()
	repo := filepath.Join(base, "one", "project")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) {
		t.Helper()
		if out, err := testGit(context.Background(), dir, args...); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	run(repo, "init")
	run(repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "Initial")
	cache := map[string]string{}
	for _, name := range []string{"tree-one", "tree-two"} {
		tree := filepath.Join(base, name)
		run(repo, "worktree", "add", "--detach", tree)
		nested := filepath.Join(tree, "src")
		if err := os.Mkdir(nested, 0700); err != nil {
			t.Fatal(err)
		}
		if got := roots.Resolve(nested, cache); got != pathutil.Real(repo) {
			t.Fatalf("%s root=%s", tree, got)
		}
	}
	other := filepath.Join(base, "two", "project")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	run(other, "init")
	if got := roots.Resolve(other, cache); got == roots.Resolve(repo, cache) {
		t.Fatal("same basename merged independent repositories")
	}
	t.Run("symlink", func(t *testing.T) {
		alias := filepath.Join(base, "alias")
		if err := os.Symlink(repo, alias); err != nil {
			if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
				t.Skip("Windows symlink creation requires Developer Mode or the symlink privilege")
			}
			t.Fatal(err)
		}
		if roots.Resolve(alias, cache) != pathutil.Real(repo) {
			t.Fatal("symlink did not resolve")
		}
	})
}

func TestCodexProjectCacheRetriesAfterExpiry(t *testing.T) {
	t.Parallel()
	clock := &testProjectClock{}
	roots := NewProjectRoots(testGit, clock)
	root := t.TempDir()
	cwd := filepath.Join(root, "nested")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if got := roots.Resolve(cwd, map[string]string{}); got != pathutil.Real(cwd) {
		t.Fatalf("non-repository root=%s", got)
	}
	if out, err := testGit(context.Background(), root, "init"); err != nil {
		t.Fatalf("init: %s %v", out, err)
	}
	// A separate inventory pass still reuses the cached result within the TTL.
	if got := roots.Resolve(cwd, map[string]string{}); got != pathutil.Real(cwd) {
		t.Fatalf("cache was not reused: %s", got)
	}
	clock.at = clock.at.Add(projectCacheTTL)
	if got := roots.Resolve(cwd, map[string]string{}); got != pathutil.Real(root) {
		t.Fatalf("expired failure did not retry: %s", got)
	}
}
