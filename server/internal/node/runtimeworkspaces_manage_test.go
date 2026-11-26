package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// manageAnswers has the fake runtime rename and archive its workspaces as Paseo does: a title, a project's name, and an
// archive that removes a worktree it made from the disk (forced, as Paseo's is), its branch kept. archiveErr, when
// set, is its refusal instead.
func (r *runtimeRepo) manageAnswers(t *testing.T, archiveErr error) {
	t.Helper()
	r.rt.mu.Lock()
	defer r.rt.mu.Unlock()
	prev := r.rt.answer
	r.rt.answer = func(method string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		switch method {
		case "workspace.rename":
			if id := str(p["workspace_id"]); id != "" {
				return map[string]any{"workspace": map[string]any{"workspace_id": id, "title": p["title"], "cwd": r.repo, "kind": "checkout",
					"branch": "main", "space_id": "prj_1", "space_name": "proj", "repo_root": r.repo}}, nil
			}
			return map[string]any{"space_name": "space " + str(p["title"])}, nil
		case "workspace.archive":
			if archiveErr != nil {
				return nil, archiveErr
			}
			for _, w := range r.n.runtimeWorkspaces() {
				if w.ID == str(p["workspace_id"]) && w.Owned {
					if _, err := git(context.Background(), r.repo, "worktree", "remove", "--force", w.Cwd); err != nil {
						return nil, err
					}
				}
			}
			return map[string]any{"archived_at": "2026-11-12T00:00:00.000Z"}, nil
		}
		return prev(method, params)
	}
}

// tellSessions has the fake runtime say its sessions (runtime.sessions) and waits until the node has them as said.
func (r *runtimeRepo) tellSessions(t *testing.T, sessions ...map[string]any) {
	t.Helper()
	list := make([]any, len(sessions))
	for i, s := range sessions {
		list[i] = s
	}
	_ = r.rt.peer.Notify("runtime.sessions", map[string]any{"sessions": list})
	h, _, _ := r.n.runtimeEngine(proto.EnginePaseo)
	waitFor(t, "the sessions", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.sessions) != len(sessions) {
			return false
		}
		for i, s := range sessions {
			if h.sessions[i].SessionID != s["session_id"] || h.sessions[i].Status != s["status"] || h.sessions[i].Starting != (s["starting"] == true) {
				return false
			}
		}
		return true
	})
}

// runtimeWorkspaces is every workspace of the machine's engine runtimes (tests).
func (n *Node) runtimeWorkspaces() []runtimeWorkspace {
	runtimes, _ := n.catalogRuntimes()
	var out []runtimeWorkspace
	for _, h := range runtimes {
		out = append(out, h.workspacesCopy()...)
	}
	return out
}

func TestRuntimeSpaceRename(t *testing.T) {
	r := runtimeRepoNode(t)
	r.manageAnswers(t, nil)
	for _, id := range []string{"prj_1", "wks_main"} {
		res, err := request(t, r.n, "space.rename", map[string]any{"workspace_id": id, "name": "App"})
		if err != nil || res["name"] != "space App" {
			t.Fatalf("space.rename %s: %v %v", id, res, err)
		}
		var asked map[string]any
		_ = json.Unmarshal(r.rt.got("workspace.rename")[len(r.rt.got("workspace.rename"))-1], &asked)
		if asked["space_id"] != "prj_1" || asked["title"] != "App" || asked["workspace_id"] != nil {
			t.Fatalf("space.rename %s asked %v", id, asked)
		}
	}
	calls := len(r.rt.got("workspace.rename"))
	// coded, so the hub words it in the reader's language (rpc-errors.space_gone)
	if _, err := request(t, r.n, "space.rename", map[string]any{"workspace_id": "w1", "name": "x"}); err == nil || rpcCode(err) != "space_gone" {
		t.Fatalf("renamed a space no engine lists: %v (code %q)", err, rpcCode(err))
	}
	if len(r.rt.got("workspace.rename")) != calls {
		t.Fatal("an unknown id reached the runtime")
	}
}

// With herdr enabled beside Paseo as the default engine, a herdr space is renamed in herdr: the one host that renames,
// though it is not the default engine's, is asked once it lists the space (it used to look for a "paseo" host and fail).
func TestRenameSpaceInHerdrBesidePaseo(t *testing.T) {
	n, _ := testNode(t)
	hh := &renameHost{bareHost: bareHost{kind: host.Herdr}, spaces: []string{"w1"}}
	n.hosts = host.NewRegistry(hh)
	c := proto.EngineChoice{Hosts: []string{"herdr", "paseo"}, Default: "paseo"}
	n.eng.name, n.eng.set = proto.LegacyEngine(c), &c
	res, err := request(t, n, "space.rename", map[string]any{"workspace_id": "w1", "name": "work"})
	if err != nil || res["name"] != "work" || !slices.Equal(hh.renamed, []string{"w1=work"}) {
		t.Fatalf("renamed %v %v, herdr %q", res, err, hh.renamed)
	}
	if _, err := request(t, n, "space.rename", map[string]any{"workspace_id": "w9", "name": "work"}); err == nil ||
		strings.Contains(err.Error(), "no paseo host") || len(hh.renamed) != 1 {
		t.Fatalf("a space herdr does not list: %v, herdr %q", err, hh.renamed)
	}
}

// Closing an Agent whose own worktree (#6115) Paseo made offers to remove it, as for herdr: worktree.remove has Paseo
// archive the workspace, which removes the folder, with Delete worktree's guards: never while a session in the
// workspace is running or an Agent of another engine runs in the folder, changes and an unmerged branch only with
// force; its branch goes after the folder when asked.
func TestRuntimeAgentWorktreeRemove(t *testing.T) {
	r := runtimeRepoNode(t)
	r.manageAnswers(t, nil)
	added, err := request(t, r.n, "worktree.add", map[string]any{"cwd": r.repo, "name_hint": "Fix it"})
	if err != nil {
		t.Fatal(err)
	}
	tree, id := str(added["path"]), str(added["workspace_id"])
	if st, err := request(t, r.n, "worktree.status", map[string]any{"cwd": tree, "kind": "paseo"}); err != nil || st["ours"] != true {
		t.Fatalf("status: %v %v", st, err)
	}
	// herdr's own folder does not hold it: not herdr's to remove
	if st, err := request(t, r.n, "worktree.status", map[string]any{"cwd": tree, "kind": "herdr"}); err != nil || st["ours"] == true {
		t.Fatalf("status as herdr's: %v %v", st, err)
	}
	remove := func(p map[string]any) (map[string]any, error) {
		p["path"], p["kind"] = tree, "paseo"
		return request(t, r.n, "worktree.remove", p)
	}
	// Another session still in the workspace: the archive would end it.
	r.tellSessions(t, map[string]any{"session_id": "paseo-other", "status": "idle", "workspace_id": id})
	if _, err := remove(map[string]any{}); rpcCode(err) != "worktree_in_use" {
		t.Fatalf("with another session in it: %v", err)
	}
	r.tellSessions(t, map[string]any{"session_id": "paseo-other", "status": "offline", "workspace_id": id})
	r.n.mu.Lock()
	r.n.agents = map[string]host.Agent{"herdr-1": {Agent: "claude", AgentStatus: proto.StatusIdle, Cwd: tree}}
	r.n.mu.Unlock()
	if _, err := remove(map[string]any{}); rpcCode(err) != "worktree_in_use" {
		t.Fatalf("with a herdr Agent in the folder: %v", err)
	}
	r.n.mu.Lock()
	r.n.agents = nil
	r.n.mu.Unlock()
	if err := os.WriteFile(filepath.Join(tree, "draft.txt"), []byte("draft\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := remove(map[string]any{}); rpcCode(err) != "worktree_dirty" {
		t.Fatalf("changes without force: %v", err)
	}
	if len(r.rt.got("workspace.archive")) != 0 {
		t.Fatal("a refused removal reached Paseo")
	}
	res, err := remove(map[string]any{"force": true, "delete_branch": true})
	if err != nil || res["removed"] != true || res["branch_deleted"] != true {
		t.Fatalf("remove: %v %v", res, err)
	}
	var asked map[string]any
	_ = json.Unmarshal(r.rt.got("workspace.archive")[0], &asked)
	if asked["workspace_id"] != id {
		t.Fatalf("workspace.archive %v", asked)
	}
	if _, err := os.Lstat(tree); !os.IsNotExist(err) {
		t.Fatal("the folder is still there")
	}
	if runPullGit(t, r.repo, "branch", "--list", str(added["branch"])) != "" {
		t.Fatal("the branch is still there")
	}
	if _, ok := catalog(t, r.n)[id]; ok {
		t.Fatal("still in the catalog")
	}
}

// A closed Agent's worktree Paseo does not remove is never called removed: Paseo's refusal in its own words, no answer
// in time said in the user's language, and an archive that kept the folder said as such.
func TestRuntimeAgentWorktreeRemoveFails(t *testing.T) {
	r := runtimeRepoNode(t)
	r.manageAnswers(t, nil)
	added, err := request(t, r.n, "worktree.add", map[string]any{"cwd": r.repo, "name_hint": "Fix it"})
	if err != nil {
		t.Fatal(err)
	}
	tree := str(added["path"])
	archive := func(answer func() (any, error)) {
		r.rt.mu.Lock()
		defer r.rt.mu.Unlock()
		prev := r.rt.answer
		r.rt.answer = func(method string, params json.RawMessage) (any, error) {
			if method == "workspace.archive" {
				return answer()
			}
			return prev(method, params)
		}
	}
	remove := func(what, want string) {
		t.Helper()
		res, err := request(t, r.n, "worktree.remove", map[string]any{"path": tree, "kind": "paseo", "locale": "en"})
		if rpcCode(err) != "worktree_failed" || !strings.Contains(err.Error(), want) || res["removed"] == true {
			t.Fatalf("%s: %v %v", what, res, err)
		}
		if _, err := os.Stat(tree); err != nil {
			t.Fatalf("%s: the folder is gone", what)
		}
	}
	archive(func() (any, error) {
		return nil, &rpc.Error{Code: "archive_failed", Message: "Paseo could not archive it"}
	})
	remove("refused", "Paseo could not archive it")

	old := runtimeRemoveDeadline
	t.Cleanup(func() { runtimeRemoveDeadline = old })
	runtimeRemoveDeadline = time.Second
	// no answer until the test ends (or after 4 s, so that a node that never gives up fails the test rather than hangs)
	held := make(chan struct{})
	t.Cleanup(func() { close(held) })
	archive(func() (any, error) {
		select {
		case <-held:
		case <-time.After(4 * time.Second):
		}
		return map[string]any{}, nil
	})
	start := time.Now()
	remove("no answer", "didn’t answer while removing the worktree")
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("no answer took %s", took)
	}
	runtimeRemoveDeadline = old

	archive(func() (any, error) { return map[string]any{"archived_at": "2026-11-12T00:00:00.000Z"}, nil }) // the folder left
	remove("folder kept", "archived the workspace but kept its folder")
}

// A closed Agent's worktree.remove waits for the runtime's archive at least as long as the Paseo bridge waits for
// Paseo's (its ARCHIVE_WAIT), as a confirmed delete does (#8135): a node that gave up first answered "no answer" while
// Paseo went on to remove the folder, and the branch it was asked to delete stayed.
func TestRuntimeRemoveWaitsAsLongAsTheBridge(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "integrations", "paseo", "lib", "bridge.js"))
	if err != nil {
		t.Skipf("no integrations checkout beside server/: %v", err)
	}
	m := regexp.MustCompile(`const ARCHIVE_WAIT = ([0-9_]+) // ms`).FindSubmatch(src)
	if m == nil {
		t.Fatal("ARCHIVE_WAIT not found in bridge.js")
	}
	ms, err := strconv.Atoi(strings.ReplaceAll(string(m[1]), "_", ""))
	if err != nil {
		t.Fatal(err)
	}
	if wait := time.Duration(ms) * time.Millisecond; runtimeRemoveDeadline < wait || runtimeRemoveDeadline != runtimeWorktreeDeadline {
		t.Fatalf("worktree.remove gives the runtime %s, the bridge waits %s for the archive (a delete: %s)", runtimeRemoveDeadline, wait, runtimeWorktreeDeadline)
	}
}
