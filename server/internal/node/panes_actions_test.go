package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

const actionSession = "11111111-2222-4333-8444-555555555555"

// actionNode is a node that has listed a workspace with an agent tab (w1:t1), a one-pane shell tab (w1:t2) and a
// split shell tab (w1:t3, two panes), as the terminal browser's menus see them.
func actionNode(t *testing.T) (*Node, *fakeHerdr, string) {
	t.Helper()
	streamingHerdr()
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	shell := func(pane, tab string) map[string]any {
		return map[string]any{"pane_id": pane, "tab_id": tab, "workspace_id": "w1", "cwd": cwd, "terminal_title": "zsh", "terminal_title_stripped": "zsh"}
	}
	agent := claudeAgent(actionSession, proto.StatusIdle, "w1:p1", "w1:t1", "w1", cwd, "fix it")
	f.reply("session.snapshot", snapshot(
		[]any{map[string]any{"workspace_id": "w1", "label": "proj", "number": 1, "cwd": cwd, "branch": "main"}},
		[]any{map[string]any{"tab_id": "w1:t1", "workspace_id": "w1", "label": "agent-1", "number": 1},
			map[string]any{"tab_id": "w1:t2", "workspace_id": "w1", "label": "shell", "number": 2},
			map[string]any{"tab_id": "w1:t3", "workspace_id": "w1", "label": "split", "number": 3}},
		[]any{agent},
		[]any{agent, shell("w1:p2", "w1:t2"), shell("w1:p3", "w1:t3"), shell("w1:p4", "w1:t3")},
	))
	listSessions(t, n)
	f.reset()
	return n, f, cwd
}

func TestTabCloseNamesAListedTabOnly(t *testing.T) {
	n, f, _ := actionNode(t)
	if _, err := request(t, n, "tabs.close", map[string]any{"tab_id": "w1:t2"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("tab.close"), []string{call("tab.close", map[string]any{"tab_id": "w1:t2"})})

	f.reset()
	for _, id := range []string{"", "w9:t9", "--help"} {
		if _, err := request(t, n, "tabs.close", map[string]any{"tab_id": id}); err == nil {
			t.Errorf("closed tab %q the host does not list", id)
		}
	}
	if got := f.trace("tab.close", "pane.close"); len(got) != 0 {
		t.Errorf("herdr was told to close for a refused request: %v", got)
	}
}

func TestTabRenameTrimsAndBoundsTheName(t *testing.T) {
	n, f, _ := actionNode(t)
	if _, err := request(t, n, "tabs.rename", map[string]any{"tab_id": "w1:t2", "label": "  build \n logs "}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("tab.rename"), []string{call("tab.rename", map[string]any{"tab_id": "w1:t2", "label": "build logs"})})

	f.reset()
	for _, label := range []string{"", "   ", strings.Repeat("长", maxLabel+1)} {
		if _, err := request(t, n, "tabs.rename", map[string]any{"tab_id": "w1:t2", "label": label}); err == nil {
			t.Errorf("renamed a tab to %q", label)
		}
	}
	if _, err := request(t, n, "tabs.rename", map[string]any{"tab_id": "w9:t9", "label": "x"}); err == nil {
		t.Error("renamed a tab the host does not list")
	}
	if got := f.trace("tab.rename"); len(got) != 0 {
		t.Errorf("tab.rename for a refused request: %v", got)
	}
}

// A shell's pane closes with its tab when it is the tab's only pane and alone when the tab is split; a pane the host
// does not list is never closed.
func TestPaneCloseShells(t *testing.T) {
	n, f, _ := actionNode(t)
	f.reply("pane.list", map[string]any{"panes": []any{map[string]any{"tab_id": "w1:t2"}}})
	if _, err := request(t, n, "panes.close", map[string]any{"pane": "w1:p2"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("pane.close", "tab.close"), []string{call("tab.close", map[string]any{"tab_id": "w1:t2"})})

	f.reset()
	f.reply("pane.list", map[string]any{"panes": []any{map[string]any{"tab_id": "w1:t3"}, map[string]any{"tab_id": "w1:t3"}}})
	if _, err := request(t, n, "panes.close", map[string]any{"pane": "w1:p4"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("pane.close", "tab.close"), []string{call("pane.close", map[string]any{"pane_id": "w1:p4"})})

	f.reset()
	if _, err := request(t, n, "panes.close", map[string]any{"pane": "w9:p9"}); err == nil {
		t.Error("closed a pane the host does not list")
	}
	if got := f.trace("pane.close", "tab.close"); len(got) != 0 {
		t.Errorf("close for a pane that is not there: %v", got)
	}
}

func TestPaneSplitStartsInThePanesDirectory(t *testing.T) {
	n, f, cwd := actionNode(t)
	f.reply("pane.split", map[string]any{"pane": map[string]any{"pane_id": "w1:p9", "tab_id": "w1:t2"}})
	res, err := request(t, n, "panes.split", map[string]any{"pane": "w1:p2", "direction": "right", "cwd": "/etc"})
	if err != nil {
		t.Fatal(err)
	}
	if res["pane"] != "w1:p9" || res["terminal"] != "pane:w1:p9" {
		t.Errorf("result = %v", res)
	}
	assertTrace(t, f.trace("pane.split"), []string{
		call("pane.split", map[string]any{"target_pane_id": "w1:p2", "direction": "right", "focus": false, "cwd": cwd}),
	})

	f.reset()
	for _, p := range []map[string]any{
		{"pane": "w1:p2", "direction": "diagonal"}, {"pane": "w1:p2"}, {"pane": "w9:p9", "direction": "down"}, {"direction": "down"},
	} {
		if _, err := request(t, n, "panes.split", p); err == nil {
			t.Errorf("split %v", p)
		}
	}
	if got := f.trace("pane.split"); len(got) != 0 {
		t.Errorf("pane.split for a refused request: %v", got)
	}
}

// A branch whose upstream was deleted on the remote and pruned (a merged pull request's branch) is not "up to date":
// the status says the upstream is gone, and Push publishes the branch again instead of finding nothing to push.
func TestGitUpstreamGone(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	runPullGit(t, f.local, "switch", "-q", "-c", "feature")
	f.commit(t, f.local, "feature.txt", "first\n")
	runPullGit(t, f.local, "push", "-q", "-u", "origin", "feature")
	runPullGit(t, f.seed, "push", "-q", "origin", "--delete", "feature")
	runPullGit(t, f.local, "fetch", "-q", "--prune")
	f.commit(t, f.local, "feature.txt", "after the merge\n")

	st, err := f.node.gitStatus(ctx, f.local)
	if err != nil {
		t.Fatal(err)
	}
	if st.Upstream != "origin/feature" || !st.Gone || st.Ahead != 0 || st.Behind != 0 {
		t.Fatalf("status = upstream %q gone %v ahead %d behind %d", st.Upstream, st.Gone, st.Ahead, st.Behind)
	}
	res, err := f.node.gitOp(ctx, f.local, "push")
	if err != nil || res.Outcome != proto.GitOutcomePushed {
		t.Fatalf("push = %+v, %v", res, err)
	}
	if got := runPullGit(t, f.seed, "ls-remote", "origin", "refs/heads/feature"); !strings.HasPrefix(got, runPullGit(t, f.local, "rev-parse", "HEAD")) {
		t.Fatalf("the remote branch is %q", got)
	}
	if st, _ = f.node.gitStatus(ctx, f.local); st.Gone || st.Ahead != 0 {
		t.Fatalf("after the push: gone %v ahead %d", st.Gone, st.Ahead)
	}

	// the Git panel's push publishes it again the same way
	runPullGit(t, f.seed, "push", "-q", "origin", "--delete", "feature")
	runPullGit(t, f.local, "fetch", "-q", "--prune")
	w, err := f.node.gitPush(ctx, f.local, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.Status.Gone || w.Status.Upstream != "origin/feature" {
		t.Fatalf("after the panel's push: %+v", w.Status)
	}
}

func TestParseStatusV2Upstream(t *testing.T) {
	for _, c := range []struct {
		name, out     string
		gone          bool
		ahead, behind int
	}{
		{"tracked", "# branch.head main\x00# branch.upstream origin/main\x00# branch.ab +2 -1\x00", false, 2, 1},
		{"level", "# branch.head main\x00# branch.upstream origin/main\x00# branch.ab +0 -0\x00", false, 0, 0},
		{"gone", "# branch.head feature\x00# branch.upstream origin/feature\x00? new.txt\x00", true, 0, 0},
		{"none", "# branch.head feature\x00", false, 0, 0},
	} {
		st := &GitStatus{}
		parseStatusV2(c.out, st)
		if st.Gone != c.gone || st.Ahead != c.ahead || st.Behind != c.behind {
			t.Errorf("%s: gone %v ahead %d behind %d", c.name, st.Gone, st.Ahead, st.Behind)
		}
	}
}

func TestGitOpsRefuse(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	plain := t.TempDir()
	for _, op := range []string{"fetch", "pull", "push"} {
		if _, err := f.node.gitOp(ctx, plain, op); refusalCode(err) != gitOpNotRepo {
			t.Errorf("%s outside a repository: %v", op, err)
		}
	}
	if _, err := f.node.gitOp(ctx, "", "fetch"); err == nil {
		t.Error("no directory accepted")
	}
	if _, err := f.node.gitOp(ctx, f.local, "reset"); err == nil {
		t.Error("an unknown op accepted")
	}
	// a dirty tree is not pulled over
	f.upstreamCommit(t)
	if err := os.WriteFile(filepath.Join(f.local, "app.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.node.gitOp(ctx, f.local, "pull"); refusalCode(err) != gitOpUncommitted {
		t.Errorf("pulled over uncommitted changes: %v", err)
	}
	// a detached HEAD cannot be pushed
	runPullGit(t, f.local, "checkout", "--", "app.txt")
	runPullGit(t, f.local, "checkout", "--detach")
	if _, err := f.node.gitOp(ctx, f.local, "push"); refusalCode(err) != gitOpDetached {
		t.Errorf("pushed a detached HEAD: %v", err)
	}
	// a branch with no upstream is neither pushed nor pulled
	runPullGit(t, f.local, "switch", "-q", "-c", "alone")
	for _, op := range []string{"push", "pull"} {
		if _, err := f.node.gitOp(ctx, f.local, op); refusalCode(err) != gitOpNoUpstream {
			t.Errorf("%s with no upstream: %v", op, err)
		}
	}
	// a remote that cannot be reached: git's own words, under its code
	runPullGit(t, f.local, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	if _, err := f.node.gitOp(ctx, f.local, "fetch"); refusalCode(err) != gitOpFetchFailed || !strings.Contains(err.Error(), "git fetch:") {
		t.Errorf("fetch from a missing remote: %v", err)
	}
	if _, err := f.node.gitOp(ctx, "", "fetch"); refusalCode(err) != gitOpNoDirectory {
		t.Errorf("no directory: %v", err)
	}
}

// panes.git takes a pane or a workspace the host lists and works in the directory the host reports for it; the
// caller names no directory.
func TestPanesGitUsesTheListedDirectory(t *testing.T) {
	n, _, cwd := actionNode(t) // a directory that is not a repository
	for _, p := range []map[string]any{
		{"pane": "w1:p2", "op": "fetch", "cwd": "/etc"}, {"workspace_id": "w1", "op": "fetch", "cwd": "/etc"},
	} {
		if _, err := request(t, n, "panes.git", p); refusalCode(err) != gitOpNotRepo {
			t.Errorf("%v in %s: %v", p, cwd, err)
		}
	}
	for _, p := range []map[string]any{{"op": "fetch"}, {"pane": "w9:p9", "op": "fetch"}, {"workspace_id": "w9", "op": "fetch"}} {
		if _, err := request(t, n, "panes.git", p); err == nil {
			t.Errorf("%v accepted", p)
		}
	}
}
