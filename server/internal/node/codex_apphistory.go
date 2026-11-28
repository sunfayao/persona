package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/node/harness/traex"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// codexAppHistoryWait bounds one read of the native SQLite history.
const codexAppHistoryWait = 30 * time.Second

// codexHistoryDBPath is the newest history database in home (thread_history_1.sqlite in 2026-11).
func codexHistoryDBPath(home string) string {
	paths, _ := filepath.Glob(filepath.Join(home, "thread_history_*.sqlite"))
	best, bestN := "", -1
	for _, p := range paths {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "thread_history_"), ".sqlite"))
		if err == nil && n > bestN {
			best, bestN = p, n
		}
	}
	return best
}

func codexHomeTag(home string) string {
	h := sha256.Sum256([]byte(home))
	return hex.EncodeToString(h[:6])
}

// codexAppHistoryDir identifies the retired disk projection directory for export checks.
func (n *Node) codexAppHistoryDir() string {
	if n.dir == "" || n.sessionScope != nil {
		return ""
	}
	return filepath.Join(n.dir, "codex-app-history")
}

// withoutGoneRollout clears a source's rollout when the file is gone: the registry keeps a path once known (the App
// deletes a rollout after moving its history into the database), as listedCodexSource does for the list.
func withoutGoneRollout(src codexSource, pass *codexListPass) codexSource {
	if src.path != "" {
		if _, err := pass.stat(src.path); err != nil && errors.Is(err, os.ErrNotExist) {
			src.path = ""
		}
	}
	return src
}

// codexAppHistoryChat: session id is an App chat with no rollout, whose transcript comes from the App's database.
func (n *Node) codexAppHistoryChat(id string) (codexSource, codex.AppThread, bool) {
	if n.sessionScope != nil {
		// Scoped nodes discover and validate the CWD of their own file-backed sessions.
		return codexSource{}, codex.AppThread{}, false
	}
	if t, ok := n.traexThread(id); ok {
		return codexSource{home: n.traexHome(), thread: id}, t, true
	}
	src, ok := n.codexSource(id)
	if !ok || n.codexBlocked(id) || n.ownedAssistantSource(id) { // as findTranscriptPath: no transcript of these here
		return codexSource{}, codex.AppThread{}, false
	}
	if t, paginated := codexPaginatedThreadOf(src); paginated {
		if _, thread, err := n.codexOwner(id); err == nil && thread.path != "" {
			t.RolloutPath = thread.path
		}
		if codex.IsContinuation(t.RolloutPath) {
			return src, t, true
		}
	}
	src = withoutGoneRollout(src, nil)
	if src.path != "" || rolloutPath(src.home, src.thread) != "" {
		return codexSource{}, codex.AppThread{}, false // a rollout is there: it is the transcript
	}
	t, ok := codexAppThreadOf(src)
	return src, t, ok
}

// indexedCodexFile keeps an in-memory paging cursor over a CLI's native SQLite history.
func (n *Node) indexedCodexFile(id string) (*transcript.File, error) {
	src, thread, ok := n.codexAppHistoryChat(id)
	if !ok {
		return nil, nil
	}
	db := codexHistoryDBPath(src.home)
	if db == "" {
		return nil, nil
	}
	source := transcript.IndexedSource{
		Revision: func() string {
			stamp := fileStamp(db) + fileStamp(db+"-wal")
			if thread.IncludeRunning {
				stamp = traexHistoryStamp(db, thread.ID, stamp)
			}
			return thread.RolloutPath + ":" + strconv.FormatInt(thread.UpdatedAtMs, 10) + ":" + stamp
		},
		Read: func() ([]transcript.CodexRecord, error) {
			ctx, cancel := context.WithTimeout(context.Background(), codexAppHistoryWait)
			defer cancel()
			return codex.ReadHistory(ctx, db, thread, querySQLiteUntil)
		},
	}
	identity := "history:" + codexHomeTag(db) + ":" + thread.ID
	n.mu.Lock()
	file := n.files[id]
	n.mu.Unlock()
	if file == nil {
		agent := "codex"
		if thread.IncludeRunning {
			agent = traexAgent
		}
		candidate := transcript.NewIndexedFile(identity, source, transcript.Sources{RolloutTools: traex.RolloutTools{}, Agent: agent})
		if err := candidate.PrefetchIndexed(); err != nil {
			return nil, err
		}
		n.mu.Lock()
		file = n.files[id]
		if file == nil {
			n.files[id] = candidate
			n.used(id)
			n.evictFiles()
			n.mu.Unlock()
			return candidate, nil
		}
		n.mu.Unlock()
	}
	if err := file.BindIndexed(identity, source); err != nil {
		return nil, err
	}
	n.mu.Lock()
	n.used(id)
	n.mu.Unlock()
	return file, nil
}

func (n *Node) refreshCodexAppHistory(id string) error {
	_, err := n.indexedCodexFile(id)
	return err
}

func (n *Node) indexedHistoryMeta(id string) (quickMeta, int64, bool) {
	n.mu.Lock()
	history := n.files[id]
	n.mu.Unlock()
	if history == nil || !history.Indexed() || history.LastSeq() == 0 {
		return quickMeta{}, 0, false
	}
	tm := history.Meta()
	m := quickMeta{lastText: tm.LastText, model: tm.Model, effort: tm.Effort,
		contextTokens: tm.ContextTokens, contextWindow: tm.ContextWindow,
		autoCompactThreshold: tm.AutoCompactThreshold, plan: tm.Plan}
	return m, int64(history.LastSeq()), true
}
