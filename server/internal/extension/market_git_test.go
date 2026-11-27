package extension

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitMarket(t *testing.T) {
	for in, want := range map[string]string{
		"git:git@git.example.com:team/plugins.git":          "git:git@git.example.com:team/plugins.git",
		"git@git.example.com:team/plugins.git":              "git:git@git.example.com:team/plugins.git",
		"git:ssh://git@git.example.com/team/plugins.git#v2": "git:ssh://git@git.example.com/team/plugins.git#v2",
		"git:https://git.example.com/team/plugins.git":      "git:https://git.example.com/team/plugins.git",
	} {
		s, err := ParseMarket(in)
		if err != nil || s.Kind != "git" || s.String() != want {
			t.Errorf("%q: %+v %v", in, s, err)
		}
	}
	for _, in := range []string{"git:/srv/plugins.git", "git:file:///srv/plugins.git", "git:ext::sh -c x", "git:git@git.example.com:team/plugins.git#--upload-pack=x",
		"git:http://git.example.com/p.git", "git:-oProxyCommand=x", "git:-o@git.example.com:x", "git:git@-host:x"} {
		if _, err := ParseMarket(in); err == nil {
			t.Errorf("%q: parsed", in)
		}
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// A hub without git (the Docker image installs none) says so when it reads a git market, on the first clone and on
// the fetch of a clone it already has, instead of an empty "git clone: " / "git fetch: ".
func TestGitMarketWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, cloned := range []bool{false, true} {
		m := &Market{Spec: MarketSpec{Kind: "git", URL: "https://example.com/plugins.git"}, Dir: t.TempDir()}
		if cloned {
			if err := os.MkdirAll(filepath.Join(m.checkout(), ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.fetchErr(); err == nil || !strings.Contains(err.Error(), "没有 git") {
			t.Fatalf("cloned=%v: err = %v, want it to say the machine has no git", cloned, err)
		}
	}
}

func (m *Market) fetchErr() error { _, _, err := m.Fetch(context.Background()); return err }
