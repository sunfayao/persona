package extension

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// A token clone refuses a git that would ignore GIT_CONFIG_COUNT (before 2.31) instead of cloning without the token,
// the refused redirects and the cleared credential helper.
func TestGitTokenNeedsGit231(t *testing.T) {
	was := gitVersionOutput
	t.Cleanup(func() { gitVersionOutput = was })
	for _, tc := range []struct {
		out     string
		err     error
		wantKey string
	}{
		{out: "git version 2.30.2\n", wantKey: "extension-market_git.git_too_old"},
		{out: "git version 1.9.5\n", wantKey: "extension-market_git.git_too_old"},
		{out: "git version 2.31.0\n"},
		{out: "git version 2.39.3 (Apple Git-146)\n"},
		{out: "git version 2.45.1.windows.1\n"},
		{out: "git version 3.0.0\n"},
		{err: exec.ErrNotFound, wantKey: "extension-market_git.git_missing"},
	} {
		gitVersionOutput = func(context.Context) ([]byte, error) { return []byte(tc.out), tc.err }
		err := gitReadsConfigEnv(context.Background())
		if tc.wantKey == "" {
			if err != nil {
				t.Errorf("%q: %v", tc.out, err)
			}
			continue
		}
		var msg *i18n.Message
		if !errors.As(err, &msg) || msg.Key != tc.wantKey {
			t.Errorf("%q %v: got %v, want %s", tc.out, tc.err, err, tc.wantKey)
		}
	}
	// The clone never starts: no git runs with the token after the version check refuses it.
	gitVersionOutput = func(context.Context) ([]byte, error) { return []byte("git version 2.30.2\n"), nil }
	m := &Market{Spec: MarketSpec{Kind: "git", URL: "https://git.example.com/market.git"}, Token: "fixture-secret", Dir: t.TempDir()}
	if _, err := m.FetchSigned(context.Background()); err == nil || !strings.Contains(err.Error(), "2.30") {
		t.Fatalf("token clone on git 2.30: %v", err)
	}
	if entries, _ := os.ReadDir(m.Dir); len(entries) != 0 {
		t.Fatalf("a clone was started: %v", entries)
	}
}
