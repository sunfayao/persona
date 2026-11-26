package node

// Renaming and deleting an engine runtime's workspaces (#6297; docs/integrations/runtimes.md "Workspaces"; Paseo:
// docs/integrations/paseo.md "Spaces and workspaces"): a workspace's title (worktree.rename), the space its sessions
// are grouped in (space.rename: Paseo's project), and a worktree the runtime made, deleted the way a terminal host's is
// (worktree.delete: a preview the user confirms with its token, then a reconciliation that trusts only the runtime's
// list and the disk), the runtime archiving it (workspace.archive). The runtime removes such a folder whatever is in
// it (Paseo forces it), so what the user confirmed is checked here first: the changes the preview showed, the sessions
// the archive ends, the checkout's identity. Its branch stays unless the user asked for it to go (and git agrees).

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// workspaceRenameTimeout bounds workspace.rename, within the 20 s the hub gives space.rename and the 30 s of
// worktree/rename.
var workspaceRenameTimeout = 15 * time.Second

// runtimeRemoveDeadline bounds a closed Agent's worktree.remove on a runtime engine, the checks and the runtime's
// archive together: at least as long as the runtime waits for the archive itself (the Paseo bridge gives Paseo 135 s,
// integrations/paseo/lib/bridge.js ARCHIVE_WAIT, for a worktree removal of up to 2 minutes), inside the 150 s the hub
// gives worktree/remove, as runtimeWorktreeDeadline is for a confirmed worktree.delete. A node that gave up first said
// "no answer" while Paseo went on to remove the folder, and the branch stayed (#8135).
var runtimeRemoveDeadline = 145 * time.Second

// dropWorkspace takes a workspace the runtime archived off its list at once: its next push says the same.
func (h *runtimeConn) dropWorkspace(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.workspaces = slices.DeleteFunc(h.workspaces, func(w runtimeWorkspace) bool { return w.ID == id })
}

// workspaceLive: one of h's sessions in workspace id is running (any state but stopped), which archiving the
// workspace would end.
func (h *runtimeConn) workspaceLive(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.ContainsFunc(h.sessions, func(s proto.Session) bool { return s.WorkspaceID == id && s.Status != proto.StatusOffline })
}

// ownedWorktreeAt is h's workspace whose folder is dir: a worktree h made (owned), which it removes with the
// workspace. h may be nil (the runtime away): none.
func (h *runtimeConn) ownedWorktreeAt(dir string) (runtimeWorkspace, bool) {
	if h == nil || !h.makesWorkspaces() {
		return runtimeWorkspace{}, false
	}
	d := realPath(filepath.Clean(dir))
	for _, w := range h.workspacesCopy() {
		if w.Kind == workspaceWorktree && w.Owned && realPath(w.Cwd) == d {
			return w, true
		}
	}
	return runtimeWorkspace{}, false
}

// runtimeSpace is the engine runtime a space id is one of: the space its sessions are grouped in (Paseo's project), or
// one of its workspaces, which names the space it is in (Android names a session's space by its workspace_id). space
// is the space's own id; h is nil when no engine runtime has it.
func (n *Node) runtimeSpace(id string) (h *runtimeConn, space string) {
	if id == "" {
		return nil, ""
	}
	runtimes, _ := n.catalogRuntimes()
	for _, h := range runtimes {
		for _, w := range h.workspacesCopy() {
			if w.SpaceID != "" && (w.SpaceID == id || w.ID == id) {
				return h, w.SpaceID
			}
		}
		h.mu.Lock()
		found := slices.ContainsFunc(h.sessions, func(s proto.Session) bool { return s.SpaceID == id })
		h.mu.Unlock()
		if found {
			return h, id
		}
	}
	return nil, ""
}

// runtimeRenameSpace is space.rename of runtime h's space: the name its sessions show, which the runtime answers
// (what the hub keys pins and to-do links by).
func (n *Node) runtimeRenameSpace(ctx context.Context, h *runtimeConn, space, name string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, workspaceRenameTimeout)
	defer cancel()
	var res struct {
		SpaceName string `json:"space_name"`
	}
	if err := h.peer.Call(cctx, "workspace.rename", map[string]any{"space_id": space, "title": name}, &res); err != nil {
		return "", runtimeErr(h, err)
	}
	return firstOf(strings.TrimSpace(res.SpaceName), name), nil
}

// runtimeAgentWorktreeRemove is worktree.remove of a closed Agent's own worktree (#6115) on a runtime engine, after
// worktreeRemove's checks (one of Persona's, no Agent in its folder, changes and an unmerged branch only with force):
// the worktree workspace the runtime made at st.Path, archived through the runtime, which removes the folder. As Delete
// worktree, never while a session in the workspace is running (the archive would end it; the closed Agent leaves the
// runtime's list a moment after, and the clients try again meanwhile). Its branch goes after the folder, when asked.
func (n *Node) runtimeAgentWorktreeRemove(ctx context.Context, h *runtimeConn, st proto.WorktreeStatus, deleteBranch, force bool) (proto.WorktreeRemoved, error) {
	var res proto.WorktreeRemoved
	w, ok := h.ownedWorktreeAt(st.Path)
	if !ok {
		return res, refusal("not_our_worktree", "this folder is not a worktree Persona made", nil)
	}
	if h.workspaceLive(w.ID) || n.agentRunsIn(st.Path) {
		return res, refusal("worktree_in_use", "an agent still runs in this worktree", nil)
	}
	if err := h.peer.Call(ctx, "workspace.archive", map[string]any{"workspace_id": w.ID}, nil); err != nil {
		if isRPCError(err) && ctx.Err() == nil {
			return res, refusal("worktree_failed", runtimeErr(h, err).Error(), nil) // the runtime's own words
		}
		// No answer: whether the runtime archived it is not known.
		return res, refusal("worktree_failed", composerError(ctx, "runtime_remove_unanswered", i18n.Args{"engine": h.label()}).Error(), nil)
	}
	h.dropWorkspace(w.ID)
	if _, err := os.Lstat(st.Path); !os.IsNotExist(err) { // the runtime archived it but kept the folder
		return res, refusal("worktree_failed", composerError(ctx, "runtime_folder_left",
			i18n.Args{"engine": h.label(), "path": st.Path}).Error(), nil)
	}
	res.Removed = true
	if deleteBranch && st.Branch != "" {
		flag := "-d"
		if force {
			flag = "-D"
		}
		if _, err := git(ctx, st.Root, "branch", flag, st.Branch); err != nil {
			return res, refusal("worktree_failed", gitFailure(err), map[string]any{"removed": true})
		}
		res.BranchDeleted = true
	}
	return res, nil
}
