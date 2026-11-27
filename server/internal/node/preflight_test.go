//go:build !windows

package node

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// preflightHome is a home with Claude Code's directory (settings, a skill linked from elsewhere, a link to nothing) and
// a working directory with its own linked command.
func preflightHome(t *testing.T) (home, claude, work string) {
	t.Helper()
	home = t.TempDir()
	claude = mkdir(t, filepath.Join(home, ".claude"))
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	shared := mkdir(t, filepath.Join(home, "dotfiles", "skills", "review"))
	mkdir(t, filepath.Join(claude, "skills"))
	symlink(t, shared, filepath.Join(claude, "skills", "review"))
	symlink(t, filepath.Join(home, "gone"), filepath.Join(claude, "skills", "old"))
	work = mkdir(t, filepath.Join(home, "code", "app"))
	cmds := mkdir(t, filepath.Join(work, ".claude", "commands"))
	if err := os.WriteFile(filepath.Join(home, "dotfiles", "ship.md"), []byte("ship"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(home, "dotfiles", "ship.md"), filepath.Join(cmds, "ship.md"))
	return home, claude, work
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestBlockedPath(t *testing.T) {
	for _, tc := range []struct {
		r    preflightResult
		want string
	}{
		{preflightResult{path: "/a/b"}, "/a/b"},
		{preflightResult{path: "/h/.claude/skills/x", target: "/h/Desktop/x/", link: true}, "/h/Desktop/x"},
		{preflightResult{path: "/h/.claude/skills/x", target: "../../Desktop/x", link: true}, "/h/Desktop/x"},
	} {
		if got := blockedPath(tc.r); got != tc.want {
			t.Errorf("%+v: %q", tc.r, got)
		}
	}
}

func TestParsePreflight(t *testing.T) {
	items := []preflightItem{{kind: "dir", path: "/a"}, {kind: "links", path: "/b"}, {kind: "dir", path: "/c"}, {kind: "dir", path: "/d"}}
	out := "preflight 1\nstart\t1\nnone\t1\nstart\t2\nok\t2\nlink\t2.1\t/b/x\t/elsewhere/x\nstart\t2.1\nok\t2.1\n" +
		"link\t2.2\t/b/y\t/slow/y\nstart\t2.2\n"
	o := parsePreflight(out, items, false, true)
	states := map[string]string{}
	for _, r := range o.results {
		states[r.path] = r.state
	}
	want := map[string]string{"/a": "missing", "/b": "ok", "/b/x": "ok", "/b/y": "hung"}
	if len(states) != len(want) || o.unchecked != 2 {
		t.Fatalf("%+v", o)
	}
	for p, s := range want {
		if states[p] != s {
			t.Errorf("%s: %s, want %s", p, states[p], s)
		}
	}
	// Finished: nothing hung, nothing unchecked; lines that do not parse are skipped.
	o = parsePreflight("start\t1\nok\t1\ngarbage\nstart\t9\nok\t9\n", items[:1], true, false)
	if len(o.results) != 1 || o.results[0].state != "ok" || o.unchecked != 0 {
		t.Errorf("%+v", o)
	}
}

func TestLaunchedVia(t *testing.T) {
	// The node's agent under today's label or the one a Mac set up before the rename keeps.
	for _, label := range []string{NodeLaunchdLabel, legacyNodeLaunchdLabel} {
		for want, env := range map[string]map[string]string{
			"ssh":     {"SSH_CONNECTION": "192.0.2.2 50000 192.0.2.1 22", "XPC_SERVICE_NAME": label},
			"launchd": {"XPC_SERVICE_NAME": label},
			"":        {"XPC_SERVICE_NAME": "application.com.apple.Terminal.123"},
		} {
			if got := launchedVia(func(k string) string { return env[k] }); got != want {
				t.Errorf("%v: %q, want %q", env, got, want)
			}
		}
	}
	if got := launchedVia(func(k string) string { return map[string]string{"XPC_SERVICE_NAME": "0"}[k] }); got != "" {
		t.Errorf("a terminal: %q", got)
	}
}

// The command line waits for the node longer than the node's longest run, so a slow run is not taken for no node.
func TestPreflightAnswerWaitOutlastsTheRun(t *testing.T) {
	if run := preflightStartDefault + (preflightMaxStalls-1)*preflightStall + preflightLinger; preflightAnswerWait <= run {
		t.Errorf("waits %s for a run of up to %s", preflightAnswerWait, run)
	}
}

// The doctor puts the check after the others, and runs it once even with --fix (which checks again).
func TestDoctorRunsThePreflightOnce(t *testing.T) {
	e := newDoctorEnv(t)
	e.healthy(t)
	os.Remove(filepath.Join(e.opts.Dir, "mcp.json"))
	runs := 0
	e.opts.Preflight = func(context.Context, i18n.Lang) DoctorCheck {
		runs++
		return DoctorCheck{Name: pathAccessCheck, Status: DoctorFail, Detail: "blocked"}
	}
	e.opts.Fix = true
	rep := RunDoctor(context.Background(), e.opts)
	if runs != 1 || rep.OK || rep.Checks[len(rep.Checks)-1].Name != pathAccessCheck || rep.Checks[len(rep.Checks)-1].Items == nil {
		t.Errorf("runs %d ok %v checks %+v", runs, rep.OK, rep.Checks)
	}
	if !slices.Equal(rep.Fixed, []string{"mcp.json"}) {
		t.Errorf("fixed %v", rep.Fixed)
	}
}

// The node's working directories: the assistant's first, then by last use, without reading the disk.
func TestNodeWorkDirs(t *testing.T) {
	e := newDoctorEnv(t)
	now := time.Now()
	e.n.metas = map[string]quickMeta{
		"/p/a/1.jsonl": {cwd: "/w/a", mtime: now.Add(-time.Hour)},
		"/p/a/2.jsonl": {cwd: "/w/a", mtime: now},
		"/p/b/1.jsonl": {cwd: "/w/b", mtime: now.Add(-time.Minute)},
	}
	got := e.n.workDirs(3)
	if len(got) != 3 || got[1] != "/w/a" || got[2] != "/w/b" || !strings.HasSuffix(got[0], "assistant") {
		t.Errorf("%q", got)
	}
	if got := e.n.workDirs(2); len(got) != 2 {
		t.Errorf("cap: %q", got)
	}
}

// The child mode, as `personad node doctor --preflight-child` runs it: the hello line, then a line per step.
func TestPreflightChildMode(t *testing.T) {
	home, claude, _ := preflightHome(t)
	var out strings.Builder
	failed, err := DoctorCommand(context.Background(), []string{preflightChildArg, "d:" + claude, "f:" + filepath.Join(claude, "nope.json"),
		"l:" + filepath.Join(claude, "skills")}, &out)
	if failed || err != nil {
		t.Fatal(failed, err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{preflightHello, "start\t1", "ok\t1", "start\t2", "none\t2", "start\t3", "ok\t3",
		"link\t3.1\t" + filepath.Join(claude, "skills", "old") + "\t" + filepath.Join(home, "gone"), "start\t3.1", "none\t3.1",
		"link\t3.2\t" + filepath.Join(claude, "skills", "review") + "\t" + filepath.Join(home, "dotfiles", "skills", "review"), "start\t3.2", "ok\t3.2"}
	if !slices.Equal(lines, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestPreflightChildProtocolStaysByteIdentical(t *testing.T) {
	home, claude, _ := preflightHome(t)
	want := strings.Join([]string{preflightHello, "start\t1", "ok\t1", "start\t2", "none\t2", "start\t3", "ok\t3",
		"link\t3.1\t" + filepath.Join(claude, "skills", "old") + "\t" + filepath.Join(home, "gone"), "start\t3.1", "none\t3.1",
		"link\t3.2\t" + filepath.Join(claude, "skills", "review") + "\t" + filepath.Join(home, "dotfiles", "skills", "review"), "start\t3.2", "ok\t3.2"}, "\n") + "\n"
	for _, locale := range []string{"en", "zh-Hans", "ja", "fr", "LANG=en_US.UTF-8"} {
		t.Run(locale, func(t *testing.T) {
			for _, key := range []string{"PERSONA_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
				t.Setenv(key, "")
			}
			if locale == "LANG=en_US.UTF-8" {
				t.Setenv("LANG", "en_US.UTF-8")
			} else {
				t.Setenv("PERSONA_LANG", locale)
			}
			var out strings.Builder
			preflightChild([]string{"d:" + claude, "f:" + filepath.Join(claude, "nope.json"), "l:" + filepath.Join(claude, "skills")}, &out)
			if out.String() != want {
				t.Fatalf("child protocol changed:\n%q\nwant:\n%q", out.String(), want)
			}
		})
	}
}
