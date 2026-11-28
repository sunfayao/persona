package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The Codex App keeps its threads in <CODEX_HOME>/state_<n>.sqlite (table threads). Since it moved their history into
// thread_history_1.sqlite (history_mode "paginated") it deletes their rollouts, so most chats in its sidebar have none and
// rollouts lists none of them (#3305: 12 of a sidebar's 21 chats were missing from Agents). codexAppThreads reads the rows of
// the chats the sidebar lists (codexAppState.sidebar): one the database has, unarchived, is listed as the sidebar lists it —
// through its rollout while the file is there, whatever its age, else from its row alone (title, directory, last update).
// An id in the sidebar's lists that the database lacks is a chat deleted in the App, which the sidebar does not show either.
//
// The database is read with the system sqlite3, read-only, as the Messages and Cindy readers are (cindybridge.go), also
// while the App has it closed (codexDBTarget). Without sqlite3, without the database, or with a schema these columns are
// not in, nothing more is listed than before.

// codexAppDBRefresh: how often the database is looked at; it is read again only when it or the sidebar's lists changed.
const codexAppDBRefresh = 5 * time.Second

// codexAppSidebarMax bounds the ids put in one query; a sidebar lists far fewer.
const codexAppSidebarMax = 2000

type codexAppDB struct {
	at    time.Time // when the database was last looked at
	stamp string    // the database's files and the sidebar's ids when last read
	byID  map[string]codex.AppThread
}

var codexAppDBs = struct {
	sync.Mutex
	byHome map[string]*codexAppDB
}{byHome: map[string]*codexAppDB{}}

// codexAppDBGlob is what codexAppDBPath globs with (a test counts the globs).
var codexAppDBGlob = filepath.Glob

// codexAppDBPath is the newest state database in home (state_5.sqlite in 2026-11): its number goes up with its schema.
func codexAppDBPath(home string) string {
	paths, _ := codexAppDBGlob(filepath.Join(home, "state_*.sqlite"))
	return codex.NewestStateDB(paths)
}

// codexAppThreads: the database's unarchived rows of the chats the Codex App's sidebar lists, by thread id; nil without
// the App's state or its database. The list is rebuilt every second and asks for each row: within codexAppDBRefresh the
// last answer is given without touching a file. A read that fails keeps the last rows read.
func codexAppThreads(home string) map[string]codex.AppThread {
	codexAppDBs.Lock()
	c := codexAppDBs.byHome[home]
	if c == nil {
		c = &codexAppDB{}
		codexAppDBs.byHome[home] = c
	}
	now := time.Now()
	if now.Sub(c.at) < codexAppDBRefresh {
		defer codexAppDBs.Unlock()
		return c.byID
	}
	c.at = now // one caller looks; the others keep the last answer meanwhile
	last, lastStamp := c.byID, c.stamp
	codexAppDBs.Unlock()

	app := loadCodexAppState(home)
	db := ""
	if app != nil { // the migrated App's sidebar is the database alone: read it even with no legacy ids (#6388)
		db = codexAppDBPath(home)
	}
	stamp := ""
	if db != "" {
		stamp = fileStamp(db) + fileStamp(db+"-wal") + strings.Join(app.sidebar, ",")
	}
	if stamp == lastStamp {
		return last
	}
	var rows map[string]codex.AppThread
	if db != "" {
		var err error
		if rows, err = readCodexAppThreads(context.Background(), db, app.sidebar); err != nil {
			return last
		}
	}
	codexAppDBs.Lock()
	defer codexAppDBs.Unlock()
	c.byID, c.stamp = rows, stamp
	return rows
}

// codexDBTarget is what sqlite3 -readonly is given to open the App's database at db. The App keeps its databases in WAL
// mode and removes their -wal and -shm when it closes one (it quit, or the database was idle); a read-only connection
// cannot open a WAL database whose -shm is gone ("unable to open database file", seen with macOS's sqlite3 3.51), so such
// a database is opened as immutable — without locks or a -shm — and read as it was last checkpointed: with the App's
// connection closed, the file holds every change it committed. The path otherwise (the App has it open, or it is not in
// WAL mode).
func codexDBTarget(db string) string {
	if _, err := os.Stat(db + "-shm"); err == nil {
		return db
	}
	f, err := os.Open(db)
	if err != nil {
		return db
	}
	defer f.Close()
	var header [20]byte // bytes 18 and 19, the file format's write and read versions, are 2 in WAL mode
	if _, err := io.ReadFull(f, header[:]); err != nil || header[18] != 2 || header[19] != 2 {
		return db
	}
	p := filepath.ToSlash(db)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/… on Windows: file:///C:/…
	}
	return (&url.URL{Scheme: "file", Path: p, RawQuery: "immutable=1"}).String()
}

func fileStamp(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "-;"
	}
	return fmt.Sprintf("%d.%d;", st.ModTime().UnixNano(), st.Size())
}

type codexGoalCache struct {
	mu     sync.Mutex
	byHome map[string]codexGoalDB
}

type codexGoalDB struct {
	at, checkedAt time.Time
	stamp         string
	goals         map[string]proto.GoalInfo
}

// The Codex CLI writes native goal-tool updates to goals_1.sqlite even when
// the rollout still has only the original thread_goal_updated event.
func (c *codexGoalCache) read(ctx context.Context, home string, now time.Time, query func(context.Context, string) (map[string]proto.GoalInfo, error)) codexGoalDB {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached := c.byHome[home]
	if now.Sub(cached.at) < codexAppDBRefresh {
		return cached
	}
	db := filepath.Join(home, "goals_1.sqlite")
	stamp := fileStamp(db)
	if stamp == "-;" {
		cached = codexGoalDB{at: now}
	} else {
		stamp += fileStamp(db + "-wal")
		if stamp == cached.stamp {
			cached.checkedAt = now
		} else {
			if goals, err := query(ctx, db); err == nil {
				cached.goals, cached.stamp = goals, stamp
				cached.checkedAt = now
			}
		}
		cached.at = now
	}
	if c.byHome == nil {
		c.byHome = map[string]codexGoalDB{}
	}
	c.byHome[home] = cached
	return cached
}

func readCodexGoals(ctx context.Context, db string) (map[string]proto.GoalInfo, error) {
	const q = `SELECT thread_id, substr(objective, 1, 16384) AS objective, status, tokens_used,
		token_budget, time_used_seconds, updated_at_ms FROM thread_goals`
	var rows []struct {
		Thread      string `json:"thread_id"`
		Objective   string `json:"objective"`
		Status      string `json:"status"`
		TokensUsed  int    `json:"tokens_used"`
		TokenBudget int    `json:"token_budget"`
		Seconds     int    `json:"time_used_seconds"`
		UpdatedMS   int64  `json:"updated_at_ms"`
	}
	if err := querySQLite(ctx, db, q, &rows); err != nil {
		return nil, err
	}
	goals := make(map[string]proto.GoalInfo, len(rows))
	for _, r := range rows {
		goals[r.Thread] = proto.GoalInfo{Objective: r.Objective, Status: r.Status, TokensUsed: r.TokensUsed,
			TokenBudget: r.TokenBudget, Seconds: r.Seconds, UpdatedAt: time.UnixMilli(r.UpdatedMS).UTC()}
	}
	return goals, nil
}

func (n *Node) applyCodexGoals(ctx context.Context, list []proto.Session, now time.Time, query func(context.Context, string) (map[string]proto.GoalInfo, error)) {
	if n.codices == nil {
		return
	}
	byHome := map[string]codexGoalDB{}
	for i := range list {
		s := &list[i]
		if s.Agent != "codex" {
			continue
		}
		src, known := n.codexSource(s.SessionID)
		if !known {
			if s.SourceKind != "" || !validID(s.SessionID) {
				continue
			}
			src = codexSource{home: n.codexPaneHome(s.PaneID), thread: s.SessionID}
		}
		db, read := byHome[src.home]
		if !read {
			db = n.codices.goals.read(ctx, src.home, now, query)
			byHome[src.home] = db
		}
		if g, ok := db.goals[src.thread]; ok {
			if s.Goal == nil || !g.UpdatedAt.Before(s.Goal.UpdatedAt) {
				s.Goal = &g
			}
		} else if s.Goal != nil && s.Goal.UpdatedAt.Before(db.checkedAt) {
			// A successful native read also observes /goal clear, which deletes the row.
			s.Goal = nil
		}
	}
}

// readCodexAppThreads reads the unarchived rows of ids (validID only: they are put in the query as they are), plus
// the migrated App's sidebar chats and their subagents, which have no legacy sidebar key (#6388). Exec runs are not
// sidebar chats; subagent rows must stay available so their origin and guardian filters still work once the rollout
// is gone (#8191). That second read is a separate query so a schema without the originator column keeps the explicit
// sidebar answer instead of failing it.
func readCodexAppThreads(ctx context.Context, db string, ids []string) (map[string]codex.AppThread, error) {
	byID := map[string]codex.AppThread{}
	if len(ids) > 0 {
		rows, err := readCodexThreads(ctx, db, `archived = 0 AND id IN ('`+strings.Join(ids, "', '")+`')`)
		if err != nil {
			return nil, err
		}
		byID = rows
	}
	migrated, err := readCodexThreads(ctx, db, `archived = 0 AND source != '' AND source != 'exec' AND originator IN ('Codex Desktop', 'codex_work_desktop')`)
	if err == nil {
		for id, t := range migrated {
			// Subagent metadata is needed for parent folding and guardian suppression. Other inferred sources are not
			// desktop conversations; explicit sidebar IDs above remain authoritative for legacy rows.
			if strings.TrimSpace(t.Source) != "vscode" && !t.Origin().Subagent {
				continue
			}
			if _, ok := byID[id]; !ok {
				byID[id] = t
			}
		}
	}
	return byID, nil
}

func readCodexThreads(ctx context.Context, db, where string) (map[string]codex.AppThread, error) {
	sqlite, err := codexSQLiteProgram()
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	q := func(settings string) string {
		// Native threads can store the whole opening prompt in title. Project unnamed titles before caching or sending them.
		return `SELECT id, rollout_path AS rolloutPath, COALESCE(NULLIF(name, ''), substr(title, 1, 256)) AS title, cwd, source, ` +
			// A native CLI may put a prefix of its opening prompt in name, even when the title
			// contains the complete request. Only recover that clipped opening tag. Keep user-chosen names.
			`CASE WHEN instr(name, '<persona-context kind="') = 1 AND instr(name, '>') = 0 ` +
			`AND name = substr(title, 1, length(name)) AND length(name) < length(title) THEN substr(title, 1, 8192) END AS contextPrompt, ` +
			`COALESCE(updated_at_ms, updated_at * 1000) AS updatedAtMs` + settings + ` FROM threads WHERE ` + where
	}
	target := codexDBTarget(db)
	query := func(settings string) ([]byte, error) {
		cmd := exec.CommandContext(cctx, sqlite, "-readonly", "-bail", "-json", "-cmd", ".timeout 2000", target, q(settings))
		hideCodexSQLiteWindow(cmd)
		return cmd.Output()
	}
	const settings = `, COALESCE(model, '') AS model, COALESCE(reasoning_effort, '') AS effort`
	out, err := query(settings + `, COALESCE(model_provider, '') AS modelProvider`)
	if err != nil {
		out, err = query(settings)
	}
	if err != nil { // a schema without those columns: the rest as before
		out, err = query("")
	}
	if err != nil {
		return nil, err
	}
	return codex.DecodeThreads(out, promptTitle)
}

// querySQLite runs q against a SQLite database with the system sqlite3 and decodes the json rows into into
// (read-only, as readCodexThreads; a WAL database nobody has open is read as it is on disk, codexDBTarget): the Codex
// App's state database, opencode's (usage_opencode.go).
func querySQLite(ctx context.Context, db, q string, into any) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return querySQLiteUntil(ctx, db, q, into)
}

func querySQLiteUntil(ctx context.Context, db, q string, into any) error {
	sqlite, err := codexSQLiteProgram()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, sqlite, "-readonly", "-bail", "-json", "-cmd", ".timeout 2000", codexDBTarget(db), q)
	hideCodexSQLiteWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil
	}
	return json.Unmarshal(out, into)
}

// codexAppThreadOf: src is a chat the Codex App's sidebar lists that has no rollout, and its row in the App's database.
func codexAppThreadOf(src codexSource) (codex.AppThread, bool) {
	if src.path != "" {
		return codex.AppThread{}, false
	}
	t, ok := codexAppThreads(src.home)[src.thread]
	return t, ok
}

// registerCodexAppThreads registers the chats the Codex App's sidebar lists, which rollouts misses when their rollout is
// older than its two weeks or gone: with the rollout the App's database names while it exists, else with none.
func (n *Node) registerCodexAppThreads(home string, pass *codexListPass) {
	threads := codexAppThreads(home)
	if len(threads) == 0 {
		return
	}
	canonical := codex.CanonicalHome(home, n.home)
	for _, t := range threads {
		path := ""
		if t.RolloutPath != "" {
			if _, err := pass.stat(t.RolloutPath); err == nil {
				path = t.RolloutPath
				if !pathWithin(path, canonical) {
					// The App names the rollout as it reached its home, which may be through a link (macOS's /var, a
					// linked ~/.codex). The rollout scan registers the same file under the resolved home, so register
					// it resolved too, or the two spellings of one file contend for the chat's path.
					path = realPath(path)
				}
			}
		}
		n.registerCanonicalCodex(canonical, t.ID, path)
	}
}
