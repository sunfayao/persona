package node

import (
	"time"

	"github.com/sunfayao/persona/server/internal/gitworktree"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCodexProjectRootDoesNotStartGitOutsideRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX fake git")
	}
	root := t.TempDir()
	cwd := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "git-called")
	fake := "#!/bin/sh\nprintf called >> \"$CODEX_TEST_GIT_CALLS\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_TEST_GIT_CALLS", marker)

	clock := &fakeProjectRootClock{at: time.Now()}
	roots := gitworktree.NewProjectRoots(git, clock)
	if got := roots.Resolve(cwd, map[string]string{}); got != realPath(cwd) {
		t.Fatalf("root = %q, want the non-repository directory", got)
	}
	clock.at = clock.at.Add(30 * time.Second)
	if got := roots.Resolve(cwd, map[string]string{}); got != realPath(cwd) {
		t.Fatalf("root after cache expiry = %q, want the non-repository directory", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("a directory with no repository marker still started Git")
	}
}

type fakeProjectRootClock struct {
	projectRootClock
	at time.Time
}

func (c *fakeProjectRootClock) Now() time.Time { return c.at }
