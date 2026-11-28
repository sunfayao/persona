package node

import (
	"context"
	"encoding/json"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// The worktree.* methods (Node.handle, #8089); see handle_file.go.
const (
	methodWorktreePrepare = "worktree.prepare"
	methodWorktreeCreate  = "worktree.create"

	methodWorktreeAdd      = "worktree.add"
	methodWorktreeStatus   = "worktree.status"
	methodWorktreeRemove   = "worktree.remove"
	methodWorktreeSnapshot = "worktree.snapshot"
)

// worktreeLocale has the worktree method's refusals worded in the language its request names (locale; the hub sends
// the reader's).
func worktreeLocale(ctx context.Context, raw json.RawMessage) context.Context {
	var in struct {
		Locale string `json:"locale"`
	}
	_ = json.Unmarshal(raw, &in)
	return i18n.With(ctx, i18n.Resolve(in.Locale))
}

// worktreePrepareParams is worktree.prepare's request (worktree.go).
type worktreePrepareParams struct {
	Kind        string `json:"kind"`
	WorkspaceID string `json:"workspace_id"`
	Cwd         string `json:"cwd"`
	Branch      string `json:"branch"`
	// AllowUntracked applies only to an automatic task branch in the checkouts folder.
	AllowUntracked bool `json:"allow_untracked"`
}

// worktreeCreateParams is worktree.create's request (worktree.go): AutoName for a branch and path of the node's choosing.
type worktreeCreateParams struct {
	Kind        string `json:"kind"`
	WorkspaceID string `json:"workspace_id"`
	Cwd         string `json:"cwd"`
	Branch      string `json:"branch"`
	Path        string `json:"path"`
	AutoName    bool   `json:"auto_name"`
	// ForAgent: the assistant makes this worktree for an Agent it starts there, so its first shell is Persona's
	// (finished_close.go, #10624). The user's "New worktree…" never sends it.
	ForAgent bool `json:"for_agent"`
	// OpenWorkspace defaults to true. A herdr leader task sets false for a checkout whose Agent tab opens in the
	// task's shared workspace instead (#4786).
	OpenWorkspace *bool `json:"open_workspace,omitempty"`
}

// worktreeAddParams is worktree.add's request (agentworktree.go, #6115).
type worktreeAddParams struct {
	Kind     string `json:"kind"`
	Cwd      string `json:"cwd"`
	Branch   string `json:"branch"`
	NameHint string `json:"name_hint"`
}

// worktreeStatusParams is worktree.status's request.
type worktreeStatusParams struct {
	Kind string `json:"kind"`
	Cwd  string `json:"cwd"`
}

// worktreeRemoveParams is worktree.remove's request.
type worktreeRemoveParams struct {
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	DeleteBranch bool   `json:"delete_branch"`
	Force        bool   `json:"force"`
}

// worktreeSnapshotParams is worktree.snapshot's request (reviewsnapshot.go, #2173).
type worktreeSnapshotParams struct {
	Kind     string `json:"kind"`
	Cwd      string `json:"cwd"`
	NameHint string `json:"name_hint"`
}
