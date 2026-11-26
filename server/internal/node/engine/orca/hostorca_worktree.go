package orca

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/gitworktree"
	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/orcart"
)

// "New worktree…" on Orca: Orca makes the checkout itself (worktree.create), so the new worktree is
// one of Orca's, listed under the workspace it came from, in Orca's sidebar and in Persona's workspaces alike.

var _ host.Worktreer = (*Host)(nil)

// CanWorktree: every Orca this node speaks to has worktree.create (its CLI's `orca worktree create` is that call).
func (h *Host) CanWorktree() bool { return true }

// WorktreesDir is Orca's default workspace directory: Orca puts a repository's worktrees in <dir>/<repo>/<name>. The
// runtime does not report the setting; when it was changed, Orca still uses its own and the result says where.
func (h *Host) WorktreesDir(home string) string { return filepath.Join(home, "orca", "workspaces") }

// CreateWorktree asks Orca for a worktree of space's repository on branch, named for path's last element: branch is
// checked out when it exists locally, else made from the space's HEAD (its branch, or the commit when detached), the
// herdr way. Orca does not fail where git would: a branch checked out elsewhere, or a folder that is taken (an empty
// one too), gets a "-2" branch and folder instead (Orca 1.4.250). So both are refused here before Orca is asked, and
// an answer on another branch (taken meanwhile, or a folder setting of Orca's own that is taken) is an error.
func (h *Host) CreateWorktree(ctx context.Context, space, branch, path string) (*host.Created, error) {
	return h.CreateWorktreeUnder(ctx, space, branch, path, space)
}

// CreateWorktreeUnder keeps the Git source separate from the sidebar parent, which can belong to another project.
func (h *Host) CreateWorktreeUnder(ctx context.Context, space, branch, path, parent string) (*host.Created, error) {
	return h.createWorktree(ctx, space, branch, path, parent, false)
}

func (h *Host) CreateGeneratedWorktree(ctx context.Context, space, parent string) (*host.Created, error) {
	return h.createWorktree(ctx, space, "", "", parent, true)
}

func (h *Host) createWorktree(ctx context.Context, space, branch, path, parent string, automatic bool) (*host.Created, error) {
	trees, _, _, err := h.c.Worktrees(ctx, "")
	if err != nil {
		return nil, RuntimeError(err)
	}
	var src *orcart.Worktree
	for i := range trees {
		if w := &trees[i]; w.WorktreeID == space && (w.HostID == "" || w.HostID == "local") {
			src = w
			break
		}
	}
	if src == nil {
		return nil, errors.New("Orca 里找不到这个工作区") // i18n-ignore: preserve existing Orca RPC error bytes
	}
	parentFound := false
	for _, w := range trees {
		if w.WorktreeID == parent && (w.HostID == "" || w.HostID == "local") {
			parentFound = true
			break
		}
	}
	if !parentFound {
		return nil, errors.New("The parent workspace is no longer available in Orca")
	}
	repoID, _, ok := strings.Cut(space, "::") // Orca's worktree ids are <repo id>::<path>
	if !ok || repoID == "" {
		return nil, errors.New("不知道这个工作区属于 Orca 的哪个项目") // i18n-ignore: preserve existing Orca RPC error bytes
	}
	if !automatic {
		if at, err := gitworktree.BranchCheckedOutAt(ctx, h.git, src.Path, branch); err != nil {
			return nil, fmt.Errorf("读不了这个仓库的 worktree 列表：%w", err) // i18n-ignore: preserve existing Orca RPC error bytes
		} else if at != "" {
			return nil, fmt.Errorf("分支 %s 已经在 %s 检出了", branch, at) // i18n-ignore: preserve existing Orca RPC error bytes
		}
		if _, err := os.Lstat(path); err == nil {
			return nil, errors.New("检出路径已存在：Orca 不用已有的目录（空目录也不行）") // i18n-ignore: preserve existing Orca RPC error bytes
		}
	}
	req := orcart.WorktreeCreate{Repo: "id:" + repoID, Name: filepath.Base(path), Branch: branch, Parent: "id:" + parent}
	if automatic {
		name, err := GeneratedWorktreeName()
		if err != nil {
			return nil, err
		}
		req.Name, req.Generated = name, true
	}
	_, branchErr := h.git(ctx, src.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if automatic || branchErr != nil {
		base, err := h.git(ctx, src.Path, "branch", "--show-current")
		if base = strings.TrimSpace(base); err != nil || base == "" {
			if base, err = h.git(ctx, src.Path, "rev-parse", "HEAD"); err != nil {
				return nil, fmt.Errorf("读不了这个工作区的 HEAD：%w", err) // i18n-ignore: preserve existing Orca RPC error bytes
			}
		}
		req.Base = strings.TrimSpace(base)
	}
	wt, err := h.c.CreateWorktree(ctx, req)
	if err != nil {
		return nil, RuntimeError(err)
	}
	got := strings.TrimPrefix(wt.Branch, "refs/heads/")
	if automatic && got == "" {
		return nil, errors.New("Orca did not report its generated branch")
	}
	if !automatic && got != branch {
		// Made all the same (nothing is deleted): the user sees what Orca did instead.
		return nil, fmt.Errorf("Orca 没有用分支 %s，而是建了 %s（在 %s）：%s 可能刚被别处检出，或者 Orca 的目录被占了", branch, got, // i18n-ignore: preserve existing Orca RPC error bytes
			wt.Path, branch)
	}
	c := &host.Created{SpaceID: wt.ID, Path: wt.Path, Branch: got}
	if t := h.terminalIn(ctx, wt.ID); t != nil {
		h.mu.Lock()
		h.track(*t) // found by its handle if Orca re-keys it (#5547)
		h.mu.Unlock()
		c.SlotID, c.Pane = t.TabID, host.PaneRef{Kind: host.Orca, ID: t.PaneKey()}
	}
	return c, nil
}

// terminalIn is the local terminal Orca opened in worktree, waiting up to h.worktreeWait for it; nil when
// none shows up (the worktree is there all the same).
func (h *Host) terminalIn(ctx context.Context, worktree string) *orcart.Terminal {
	deadline := h.clock.Now().Add(h.worktreeWait)
	for {
		if l, err := h.c.Terminals(ctx, false); err == nil {
			for _, t := range l.Terminals {
				if t.WorktreeID == worktree && t.IsLocal() && !t.Orphaned && t.TabID != "" {
					return &t
				}
			}
		}
		if h.clock.Now().After(deadline) || ctx.Err() != nil {
			return nil
		}
		<-h.clock.After(200 * time.Millisecond)
	}
}

// GeneratedWorktreeName picks a seed from Orca's creature-name pool.
func GeneratedWorktreeName() (string, error) {
	// These seeds belong to Orca's generated creature-name pool. Orca applies its retired-name registry, branch prefix
	// and collision retries; omitting branchNameOverride keeps the branch eligible for its first-work rename.
	orcaGeneratedNames := [...]string{"nautilus", "seahorse", "starfish", "coral", "narwhal", "jellyfish", "octopus", "manta",
		"dolphin", "manatee", "cuttlefish", "anemone", "urchin", "triton", "nudibranch", "coelacanth"}
	var seed [1]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return "", err
	}
	return orcaGeneratedNames[int(seed[0])%len(orcaGeneratedNames)], nil
}
