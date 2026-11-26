package files

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testClock struct {
	wallClock
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

func testGit(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
}

func testConfig(home string) Config {
	return Config{
		Home:     home,
		RealPath: filepath.EvalSymlinks,
		StatPath: func(path string) (string, os.FileInfo, error) {
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", nil, err
			}
			st, err := os.Stat(real)
			return real, st, err
		},
		OffLimits:     func(string) error { return nil },
		ExplainDenied: func(_ string, err error) error { return err },
		GitCommand:    testGit,
		IgnoreCommand: func(ctx context.Context, dir string) *exec.Cmd {
			return testGit(ctx, dir, "check-ignore", "-z", "--stdin")
		},
	}
}

func runPullGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := testGit(context.Background(), dir, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}
