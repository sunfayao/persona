package gitworktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/pathutil"
)

const projectCacheTTL = 30 * time.Second
const projectCacheLimit = 512

type projectCacheEntry struct {
	root string
	at   time.Time
}

// ProjectClock supplies cache timestamps and bounds Git probes.
type ProjectClock interface {
	Now() time.Time
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

// ProjectRoots resolves Git worktrees for Codex and Claude desktop sessions.
type ProjectRoots struct {
	mu      sync.Mutex
	entries map[string]projectCacheEntry
	git     func(context.Context, string, ...string) (string, error)
	clock   ProjectClock
}

func NewProjectRoots(git func(context.Context, string, ...string) (string, error), clock ProjectClock) *ProjectRoots {
	return &ProjectRoots{git: git, clock: clock, entries: make(map[string]projectCacheEntry)}
}

// repositoryMarker says whether cwd or an ancestor has a .git entry. An uncertain read stays on the Git path.
// Ordinary Codex App folders are not repositories: starting Git for each of them on every cache expiry turns an
// unreadable working directory into a two-second stall without producing any useful project information.
func repositoryMarker(cwd string) bool {
	for {
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil || !os.IsNotExist(err) {
			return true
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return false
		}
		cwd = parent
	}
}

// Resolve Git's shared metadata on the source machine, never by directory name.
func (p *ProjectRoots) Resolve(cwd string, cache map[string]string) string {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return ""
	}
	cwd = pathutil.Real(filepath.Clean(cwd))
	if root, ok := cache[cwd]; ok {
		return root
	}
	p.mu.Lock()
	entry, cached := p.entries[cwd]
	p.mu.Unlock()
	if cached && p.clock.Now().Sub(entry.at) < projectCacheTTL {
		cache[cwd] = entry.root
		return entry.root
	}
	root := cwd
	if repositoryMarker(cwd) {
		ctx, cancel := p.clock.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if common, err := p.git(ctx, cwd, "rev-parse", "--git-common-dir"); err == nil {
			common = strings.TrimSpace(common)
			if !filepath.IsAbs(common) {
				common = filepath.Join(cwd, common)
			}
			common = pathutil.Real(filepath.Clean(common))
			if filepath.Base(common) == ".git" {
				root = filepath.Dir(common)
			} else if top, err := p.git(ctx, cwd, "rev-parse", "--show-toplevel"); err == nil {
				root = pathutil.Real(strings.TrimSpace(top))
			}
		}
	}
	// Failures use the same short TTL: a later git init or restored directory
	// must become visible without restarting the node.
	p.mu.Lock()
	if len(p.entries) >= projectCacheLimit {
		clear(p.entries)
	}
	p.entries[cwd] = projectCacheEntry{root: root, at: p.clock.Now()}
	p.mu.Unlock()
	cache[cwd] = root
	return root
}
