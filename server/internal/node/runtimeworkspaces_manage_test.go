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
