package files

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/pathutil"
)

// fs.find: every file under a directory, as paths relative to it, for the web client's ⌘K file search (docs/web/web.md
// "Command palette"). The client matches them itself; this only lists. In a git repository the list is what git tracks plus
// what it does not ignore, so node_modules and build output are not in it; elsewhere it is a bounded breadth-first
// walk. Both go through the path guard of fs.list: the directory is resolved with realPath, no name that is off limits
// is listed, and the walk never follows a link.

const (
	// maxFindFiles caps one listing; a bigger tree comes back truncated (walk: the shallowest files first).
	maxFindFiles = 50000
	// findWalkBudget bounds the walk of a directory that is not a repository.
	findWalkBudget = 5 * time.Second
	// findGitBudget bounds git ls-files.
	findGitBudget = 10 * time.Second
	// findCacheMax is how many directories' lists are kept.
	findCacheMax = 16
)

// findTTL is how long a directory's list is reused: the palette is opened again and again while someone works.
const findTTL = 10 * time.Second

// skipWalkDirectory reports version control folders and build caches the walk skips.
func skipWalkDirectory(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "__pycache__", ".cache", ".gradle", ".tox", ".mypy_cache", ".pytest_cache", ".ruff_cache", ".next", ".nuxt", ".turbo", ".parcel-cache", ".venv", "DerivedData", ".Trash":
		return true
	}
	return false
}

// FindResult is the list fs.find returns.
type FindResult struct {
	Cwd       string   `json:"cwd"`
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated"`
	Source    string   `json:"source"` // "git" or "walk"
}

type findEntry struct {
	done chan struct{}
	at   time.Time
	res  *FindResult
	err  error
}

// Find lists the files under dir (cwd as the conversation has it); limit <= 0 or above the cap means the cap.
// A directory asked again within findTTL, or while it is being listed, shares the one list.
func (n *Service) Find(ctx context.Context, dir string, limit int) (*FindResult, error) {
	if limit <= 0 || limit > maxFindFiles {
		limit = maxFindFiles
	}
	dir = strings.TrimSpace(dir)
	switch {
	case dir == "~" || strings.HasPrefix(dir, "~/"):
		dir = filepath.Join(n.config.Home, strings.TrimPrefix(dir, "~"))
	case dir == "":
		return nil, fmt.Errorf("empty path")
	}
	asked := filepath.Clean(dir)
	real, err := n.config.RealPath(pathutil.Abs(asked))
	if err != nil {
		return nil, n.config.ExplainDenied(asked, err)
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, n.config.ExplainDenied(asked, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", asked)
	}

	key := fmt.Sprintf("%s\x00%d", real, limit)
	n.findMu.Lock()
	e := n.finds[key]
	if e != nil && e.res != nil && n.config.Clock.Now().Sub(e.at) > findTTL {
		delete(n.finds, key)
		e = nil
	}
	if e == nil {
		e = &findEntry{done: make(chan struct{})}
		n.finds[key] = e
		n.evictFinds(key)
		go func() {
			// Not tied to the first caller's request: others may be waiting on this list.
			lctx, cancel := context.WithTimeout(context.Background(), findGitBudget+findWalkBudget)
			defer cancel()
			res, err := n.listFiles(lctx, real, limit)
			n.findMu.Lock()
			e.res, e.err, e.at = res, err, n.config.Clock.Now()
			if err != nil && n.finds[key] == e {
				delete(n.finds, key) // a failure is not kept
			}
			n.findMu.Unlock()
			close(e.done)
		}()
	}
	n.findMu.Unlock()

	select {
	case <-e.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if e.err != nil {
		return nil, e.err
	}
	out := *e.res
	out.Cwd = asked
	out.Files = slices.Clone(e.res.Files) // the list is shared with later callers
	return &out, nil
}

// evictFinds drops the oldest lists beyond findCacheMax (n.findMu held); keep is the one just added.
func (n *Service) evictFinds(keep string) {
	for len(n.finds) > findCacheMax {
		oldest, oldestAt := "", time.Time{}
		for k, e := range n.finds {
			if k == keep || e.res == nil {
				continue
			}
			if oldest == "" || e.at.Before(oldestAt) {
				oldest, oldestAt = k, e.at
			}
		}
		if oldest == "" {
			return
		}
		delete(n.finds, oldest)
	}
}

func (n *Service) listFiles(ctx context.Context, real string, limit int) (*FindResult, error) {
	if res := n.gitFiles(ctx, real, limit); res != nil {
		return res, nil
	}
	return n.walkFiles(ctx, real, limit), nil
}

// gitFiles is git ls-files for a directory inside a work tree, or nil when it is not one (or git is not there).
func (n *Service) gitFiles(ctx context.Context, real string, limit int) *FindResult {
	ctx, cancel := context.WithTimeout(ctx, findGitBudget)
	defer cancel()
	cmd := n.config.GitCommand(ctx, real, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	res := &FindResult{Source: "git", Files: []string{}}
	seen := map[string]bool{}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, 0); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for sc.Scan() {
		rel := sc.Text()
		if rel == "" || seen[rel] { // a file in a merge conflict is listed once per stage
			continue
		}
		seen[rel] = true
		full := filepath.Join(real, filepath.FromSlash(rel))
		if n.config.OffLimits(full) != nil {
			continue
		}
		// Not what git lists but is gone, nor a submodule's folder.
		if st, err := os.Lstat(full); err != nil || st.IsDir() {
			continue
		}
		if len(res.Files) >= limit {
			res.Truncated = true
			break
		}
		res.Files = append(res.Files, rel)
	}
	if res.Truncated || sc.Err() != nil {
		cancel() // stop git; the rest is not wanted
	}
	if err := cmd.Wait(); err != nil && !res.Truncated && sc.Err() == nil {
		return nil // not a repository (exit 128), or git failed
	}
	return res
}

// walkFiles lists real breadth first, so a tree too big for the caps keeps its shallow files.
func (n *Service) walkFiles(ctx context.Context, real string, limit int) *FindResult {
	ctx, cancel := context.WithTimeout(ctx, findWalkBudget)
	defer cancel()
	res := &FindResult{Source: "walk", Files: []string{}}
	queue := []string{""}
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		if ctx.Err() != nil {
			res.Truncated = true
			break
		}
		ents, err := os.ReadDir(filepath.Join(real, filepath.FromSlash(rel))) // sorted by name
		if err != nil {
			continue
		}
		var dirs []string
		for _, e := range ents {
			name := e.Name()
			child := name
			if rel != "" {
				child = rel + "/" + name
			}
			full := filepath.Join(real, filepath.FromSlash(child))
			if n.config.OffLimits(full) != nil {
				continue
			}
			if e.IsDir() { // a link to a directory is not IsDir: it is listed as a file and not entered
				if !skipWalkDirectory(name) && !(rel == "" && n.config.Home != "" && real == filepath.Clean(n.config.Home) && name == "Library") {
					dirs = append(dirs, child)
				}
				continue
			}
			if len(res.Files) >= limit {
				res.Truncated = true
				return res
			}
			res.Files = append(res.Files, child)
		}
		queue = append(queue, dirs...)
	}
	return res
}
