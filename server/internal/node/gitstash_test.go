package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Exercise the same JSON contract used by the hub, not a parallel selection helper.
func stashRequest(t *testing.T, n *Node, cwd, op string, selection map[string]any) (map[string]any, error) {
	t.Helper()
	p := map[string]any{"cwd": cwd, "op": op}
	for k, v := range selection {
		p[k] = v
	}
	raw, _ := json.Marshal(p)
	res, err := n.gitWrite(context.Background(), "git.stash", raw)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	return body, nil
}

func selectedStash(t *testing.T, n *Node, cwd string, index int) map[string]any {
	t.Helper()
	body, err := stashRequest(t, n, cwd, "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := body["stashes"].([]any)
	if len(entries) <= index {
		t.Fatalf("missing stash %d: %v", index, body)
	}
	entry := entries[index].(map[string]any)
	oid, _ := entry["oid"].(string)
	if oid != runPullGit(t, cwd, "rev-parse", "stash@{"+strconv.Itoa(index)+"}") {
		t.Fatalf("list must carry the full selected object ID: %v", entry)
	}
	return map[string]any{"index": index, "oid": oid}
}

func assertStashLocksReleased(t *testing.T, cwd string) {
	t.Helper()
	common := runPullGit(t, cwd, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	for _, p := range []string{"refs/stash.lock", "logs/refs/stash.lock", "packed-refs.lock"} {
		if _, err := os.Stat(filepath.Join(common, filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Fatalf("lock retained: %s (%v)", p, err)
		}
	}
}

func TestStashRepositoryOwnership(t *testing.T) {
	f := newPullFixture(t)
	for _, cwd := range []string{f.local, f.seed} {
		putFile(t, cwd, "app.txt", filepath.Base(cwd)+"\n")
		runPullGit(t, cwd, "stash", "push", "-qm", "local selection")
	}
	selected := selectedStash(t, f.node, f.local, 0)
	before := runPullGit(t, f.seed, "stash", "list", "--format=%H")
	for _, op := range []string{"apply", "pop", "drop"} {
		if _, err := stashRequest(t, f.node, f.seed, op, selected); err == nil {
			t.Fatal("another repository accepted selection")
		}
		if runPullGit(t, f.seed, "stash", "list", "--format=%H") != before || runPullGit(t, f.seed, "diff") != "" {
			t.Fatal("other repository mutated")
		}
	}
	assertStashLocksReleased(t, f.local)
	assertStashLocksReleased(t, f.seed)
}

// Git before 2.45 echoes the unknown --show-ref-format option; pop and drop must still work there.
func TestStashRefBackendOlderGit(t *testing.T) {
	for out, want := range map[string]string{"files\n": "files", "reftable\n": "reftable", "--show-ref-format\n": "files"} {
		if got := stashRefBackend(out); got != want {
			t.Errorf("stashRefBackend(%q) = %q, want %q", out, got, want)
		}
	}
}
