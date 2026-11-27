package node

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	repository "github.com/sunfayao/persona/server/internal/repositorywire"
	"github.com/sunfayao/persona/server/internal/taskworkspace"
	"github.com/sunfayao/persona/server/internal/workspacenaming"
)

func workspaceGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = taskworkspace.GitEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
func workspaceRemote(t *testing.T, branch string) string {
	t.Helper()
	dir := workspaceTemp(t)
	workspaceGit(t, dir, "init", "-b", branch)
	workspaceGit(t, dir, "config", "user.name", "Example")
	workspaceGit(t, dir, "config", "user.email", "me@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	workspaceGit(t, dir, "add", ".")
	workspaceGit(t, dir, "commit", "-m", "Initial")
	return dir
}
func workspacePlan(t *testing.T, home string, layout repository.Layout, urls ...string) workspacenaming.Plan {
	t.Helper()
	rows := []repository.PreparationRepository{}
	for i, url := range urls {
		id := []string{"web", "api"}[i]
		identity, _ := repository.Parse("https://github.com/example/"+id, "")
		rows = append(rows, repository.PreparationRepository{RepoID: id, RequestedRef: repository.RequestedRef{Kind: "default"}, SourceSnapshot: repository.SourceSnapshot{Identity: identity, CloneURL: url}})
	}
	source := workspacenaming.Source{Manager: "example", TodoID: "todo_example", MachineID: "node_example", Title: "Example", PlannedAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), Timezone: "UTC", Rules: workspacenaming.Defaults(), Layout: layout, Repositories: repository.PreparationSnapshot{SchemaVersion: 1, Manager: "example", TodoID: "todo_example", Repositories: rows}}
	preview := workspacenaming.Resolve(source, workspacenaming.NodeContext{Home: home, PersonaHome: filepath.Join(home, ".persona")})
	if preview.Plan == nil {
		t.Fatal(preview)
	}
	return *preview.Plan
}
func workspaceCall(t *testing.T, n *Node, method string, req taskworkspace.Request) taskworkspace.Record {
	t.Helper()
	raw, _ := json.Marshal(req)
	out, err := n.handle(t.Context(), "task.workspace."+method, raw)
	if err != nil {
		t.Fatal(err)
	}
	return out.(taskworkspace.Record)
}

func TestBackendWorkspaceRejectsCredentialsBeforeReceipt(t *testing.T) {
	home := workspaceTemp(t)
	n := &Node{home: home, dir: filepath.Join(home, ".persona")}
	p := workspacePlan(t, home, repository.LayoutRoot, workspaceRemote(t, "trunk"))
	identity, _ := repository.Parse("https://code.example.com/example/web", "gitlab")
	p.Source.Repositories.Repositories[0].SourceSnapshot.Identity = identity
	p.Source.Repositories.Repositories[0].SourceSnapshot.CloneURL = "https://example:secret-value@code.example.com/example/web.git"
	p = *workspacenaming.Resolve(p.Source, p.Node).Plan
	req := taskworkspace.Request{WorkspaceID: "ws_secret", OperationID: "op_secret", Plan: p}
	r := workspaceCall(t, n, "prepare", req)
	if r.Error == nil || r.Error.Code != "source_invalid" {
		t.Fatal(r)
	}
	if _, err := os.Stat(n.dir); !os.IsNotExist(err) {
		t.Fatal("invalid transport persisted before validation", err)
	}
}

func TestBackendWorkspaceResumesBranchCreatedBeforeCheckout(t *testing.T) {
	home := workspaceTemp(t)
	n := &Node{home: home, dir: filepath.Join(home, ".persona")}
	req := taskworkspace.Request{WorkspaceID: "ws_checkout", OperationID: "op_checkout", Plan: workspacePlan(t, home, repository.LayoutRoot, workspaceRemote(t, "trunk"))}
	r := workspaceCall(t, n, "prepare", req)
	if r.State != "ready" {
		t.Fatal(r)
	}
	instance := r.Repositories[0]
	// Reconstruct a real Git failure boundary: the owned branch exists, while
	// checkout was rolled back before a successful materialization receipt.
	workspaceGit(t, instance.CommonDir, "worktree", "remove", "--", instance.Path)
	if err := os.Mkdir(instance.Path, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(n.dir, "task-workspaces", taskworkspace.Hash(req.WorkspaceID), "receipt.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal workspaceJournal
	if err = json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	journal.State = "preparing"
	journal.Repositories[0].State = "materializing"
	journal.Identities[instance.Path], err = workspaceFileID(instance.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err = workspaceWrite(path, &journal); err != nil {
		t.Fatal(err)
	}
	n = &Node{home: home, dir: filepath.Join(home, ".persona")}
	r = workspaceCall(t, n, "prepare", req)
	if r.State != "ready" || r.Repositories[0].ResolvedCommit != instance.ResolvedCommit {
		t.Fatal(r)
	}
}

func workspaceTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBackendWorkspaceRepeatedCorrectionsKeepPhysicalOperationIdentity(t *testing.T) {
	home := workspaceTemp(t)
	n := &Node{home: home, dir: filepath.Join(home, ".persona")}
	req := taskworkspace.Request{WorkspaceID: "ws_correction", OperationID: "op_first", Plan: workspacePlan(t, home, repository.LayoutRoot, workspaceRemote(t, "trunk"))}
	first := workspaceCall(t, n, "prepare", req)
	if first.State != "ready" {
		t.Fatal(first)
	}
	workspaceCall(t, n, "cancel", req)
	incompatible := req
	incompatible.OperationID = "op_incompatible"
	incompatible.PreviousOperationID = req.OperationID
	source := req.Plan.Source
	source.Rules.BranchTemplate = "different/{task-id}"
	incompatible.Plan = *workspacenaming.Resolve(source, req.Plan.Node).Plan
	refused := workspaceCall(t, n, "prepare", incompatible)
	if refused.Error == nil || refused.Error.Code != "resource_conflict" || refused.OperationID != incompatible.OperationID || refused.NodeOperationID != req.OperationID {
		t.Fatal(refused)
	}
	corrected := req
	corrected.OperationID = "op_corrected"
	corrected.PreviousOperationID = refused.NodeOperationID
	r := workspaceCall(t, n, "prepare", corrected)
	if r.State != "ready" || r.NodeOperationID != corrected.OperationID || r.Repositories[0].ResolvedCommit != first.Repositories[0].ResolvedCommit {
		t.Fatal(r)
	}
}

func TestBackendWorkspaceIgnoresInheritedRepositoryLocators(t *testing.T) {
	external := workspaceRemote(t, "existing")
	head := workspaceGit(t, external, "rev-parse", "HEAD")
	home := workspaceTemp(t)
	n := &Node{home: home, dir: filepath.Join(home, ".persona")}
	req := taskworkspace.Request{WorkspaceID: "ws_environment", OperationID: "op_environment", Plan: workspacePlan(t, home, repository.LayoutRoot, workspaceRemote(t, "trunk"))}
	t.Setenv("GIT_DIR", filepath.Join(external, ".git"))
	t.Setenv("GIT_WORK_TREE", external)
	r := workspaceCall(t, n, "prepare", req)
	if r.State != "ready" {
		t.Fatal(r)
	}
	if workspaceGit(t, external, "rev-parse", "HEAD") != head || strings.TrimSpace(workspaceGit(t, external, "status", "--porcelain")) != "" {
		t.Fatal("preparation altered the existing checkout")
	}
}

func TestBackendWorkspaceCorrectsUnresolvedDefaultToExplicitBranch(t *testing.T) {
	remote := workspaceRemote(t, "trunk")
	workspaceGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/missing")
	home := workspaceTemp(t)
	n := &Node{home: home, dir: filepath.Join(home, ".persona")}
	req := taskworkspace.Request{WorkspaceID: "ws_head", OperationID: "op_default", Plan: workspacePlan(t, home, repository.LayoutRoot, remote)}
	failed := workspaceCall(t, n, "prepare", req)
	if failed.Error == nil || failed.Error.Code != "default_branch_unresolved" {
		t.Fatal(failed)
	}
	workspaceCall(t, n, "cancel", req)
	req.PreviousOperationID = req.OperationID
	req.OperationID = "op_explicit"
	req.Plan.Source.Repositories.Repositories[0].RequestedRef = repository.RequestedRef{Kind: "branch", Value: "trunk"}
	req.Plan = *workspacenaming.Resolve(req.Plan.Source, req.Plan.Node).Plan
	ready := workspaceCall(t, n, "prepare", req)
	if ready.State != "ready" || ready.Repositories[0].ResolvedRef != "refs/heads/trunk" {
		t.Fatal(ready)
	}
}
