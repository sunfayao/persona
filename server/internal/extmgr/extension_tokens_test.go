package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/bus"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// tokenRecordOn is the manager's database for a test of its tokens' record (#1156), skipped without
// PERSONA_TEST_DATABASE_URL, and a manager id of the test's own: tests on the same database see only their own rows.
func tokenRecordOn(t *testing.T) (*db.DB, string) {
	t.Helper()
	url := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set (use scripts/test/server_test_env.py)")
	}
	database, err := db.Open(context.Background(), url, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	return database, "example-manager-" + randomHex(4)
}

// tokenRows are manager's records, by hash.
func tokenRows(t *testing.T, database *db.DB, manager string) map[string]db.ExtensionToken {
	t.Helper()
	rows, err := database.ExtensionTokens(context.Background(), manager)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]db.ExtensionToken{}
	for _, r := range rows {
		out[r.Hash] = r
	}
	return out
}

// liveTokens are the hashes of manager's records not revoked.
func liveTokens(rows map[string]db.ExtensionToken) []string {
	var out []string
	for hash, r := range rows {
		if r.RevokedAt == nil {
			out = append(out, hash)
		}
	}
	return out
}

// noPlaintext fails when a record holds tok, or the random part of it, anywhere.
func noPlaintext(t *testing.T, r db.ExtensionToken, tok string) {
	t.Helper()
	fields := append([]string{r.Hash, r.Manager, r.Extension, r.Name, r.Reason}, r.Scopes...)
	for _, f := range fields {
		if strings.Contains(f, tok) || strings.Contains(f, strings.TrimPrefix(tok, extTokenPrefix)) {
			t.Fatalf("the record holds the token: %+v", r)
		}
	}
	if r.Hash != hashToken(tok) {
		t.Fatalf("recorded hash %q, want the token's", r.Hash)
	}
}

// sessionsRead is whether tok opens the hub's session list on h, and whose h says it is.
func sessionsRead(h *Manager, tok string) extwire.Authorized {
	return h.authorize(extwire.Authorize{Hash: hashToken(tok), Pattern: "GET /api/v1/sessions", Method: "GET", Path: "/api/v1/sessions"})
}

// What a manager issues and revokes is recorded by hash, never the token, with whose it is, what it opens and why it
// stopped. Authorizing reads only what the manager issued since it started: a token the record still holds as live
// from this manager's last run opens nothing, nor does another manager's, and each start revokes only its own.
func TestExtensionTokenRecord(t *testing.T) {
	database, owner := tokenRecordOn(t)
	h := newTestManager(t)
	h.routes()
	h.extTokens.keepRecord(database, owner, t.Logf)

	tok := h.extTokens.issue("example-x", "Example X", []string{extension.ScopeAgentsRead, extension.ScopeNotify})
	all := h.extTokens.issueAll("example-y", "Example Y")
	rows := tokenRows(t, database, owner)
	r := rows[hashToken(tok)]
	noPlaintext(t, r, tok)
	if r.Extension != "example-x" || r.Name != "Example X" || r.All || r.RevokedAt != nil || r.IssuedAt.IsZero() ||
		!slices.Equal(r.Scopes, []string{extension.ScopeAgentsRead, extension.ScopeNotify}) {
		t.Fatalf("the scoped token's record: %+v", r)
	}
	if r := rows[hashToken(all)]; !r.All || len(r.Scopes) != 0 || r.Extension != "example-y" || r.RevokedAt != nil {
		t.Fatalf("the token of every scope: %+v", r)
	}

	tok2 := h.extTokens.issue("example-x", "Example X", []string{extension.ScopeAgentsRead}) // its next start, granted less
	h.extTokens.revoke("example-y", db.ExtensionTokenDisabled)
	rows = tokenRows(t, database, owner)
	if r := rows[hashToken(tok)]; r.RevokedAt == nil || r.Reason != db.ExtensionTokenReplaced {
		t.Fatalf("the replaced token: %+v", r)
	}
	if r := rows[hashToken(tok2)]; r.RevokedAt != nil || !slices.Equal(r.Scopes, []string{extension.ScopeAgentsRead}) {
		t.Fatalf("its new token: %+v", r)
	}
	if r := rows[hashToken(all)]; r.RevokedAt == nil || r.Reason != db.ExtensionTokenDisabled {
		t.Fatalf("the revoked token: %+v", r)
	}

	// Another manager on the same database starts: this one's record is not its to touch.
	other := newTestManager(t)
	other.routes()
	other.extTokens.keepRecord(database, "example-other-"+randomHex(4), t.Logf)
	other.extTokens.revokeLastRun(context.Background())
	if live := liveTokens(tokenRows(t, database, owner)); !slices.Equal(live, []string{hashToken(tok2)}) {
		t.Fatalf("after another manager's start, live: %v", live)
	}

	// This manager starts again (the last run ended without revoking tok2): the record's live token is revoked, and
	// nothing recorded opens anything on the new run or the other manager.
	next := newTestManager(t)
	next.routes()
	next.extTokens.keepRecord(database, owner, t.Logf)
	next.extTokens.revokeLastRun(context.Background())
	for _, m := range []*Manager{next, other} {
		for _, tk := range []string{tok, tok2, all} {
			if a := sessionsRead(m, tk); a.Extension != "" || a.OK {
				t.Fatalf("a recorded token opens on a manager that did not issue it this run: %+v", a)
			}
		}
	}
	rows = tokenRows(t, database, owner)
	if live := liveTokens(rows); len(live) != 0 {
		t.Fatalf("live after the restart: %v", live)
	}
	if rows[hashToken(tok2)].Reason != db.ExtensionTokenManagerRestart || rows[hashToken(all)].Reason != db.ExtensionTokenDisabled ||
		rows[hashToken(tok)].Reason != db.ExtensionTokenReplaced {
		t.Fatalf("why each stopped: %+v", rows)
	}
}

// fakeTokenRecord keeps the order of the record's writes, and fails each one while fail is set.
type fakeTokenRecord struct {
	mu    sync.Mutex
	fail  bool
	calls [][3]string // what, the extension or hash, why
}

func (f *fakeTokenRecord) note(what, of, why string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, [3]string{what, of, why})
	if f.fail {
		return errors.New("example: the database does not answer")
	}
	return nil
}

func (f *fakeTokenRecord) IssueExtensionToken(_ context.Context, t db.ExtensionToken) error {
	return f.note("issue", t.Extension+" "+t.Hash, "")
}

func (f *fakeTokenRecord) RevokeExtensionToken(_ context.Context, _, hash, reason string) error {
	return f.note("revoke", hash, reason)
}

func (f *fakeTokenRecord) RevokeExtensionTokens(_ context.Context, _, reason string) (int64, error) {
	return 0, f.note("revoke_all", "", reason)
}

// A record that fails costs nothing but its log line: the token is issued, opens what it was granted and is revoked
// as without a record, and the log names what was not recorded, never the token.
func TestExtensionTokenRecordFailing(t *testing.T) {
	rec := &fakeTokenRecord{fail: true}
	var mu sync.Mutex
	var logs []string
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	h := newTestManager(t)
	h.routes()
	h.extTokens.keepRecord(rec, "example-manager", logf)
	h.extTokens.revokeLastRun(context.Background())
	tok := h.extTokens.issue("example-x", "Example X", []string{extension.ScopeAgentsRead})
	if a := sessionsRead(h, tok); !a.OK || a.Extension != "example-x" {
		t.Fatalf("a token issued while the record fails: %+v", a)
	}
	h.extTokens.revoke("example-x", db.ExtensionTokenDisabled)
	if a := sessionsRead(h, tok); a.Extension != "" {
		t.Fatalf("a token revoked while the record fails: %+v", a)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(rec.calls) != 3 || len(logs) != 3 {
		t.Fatalf("writes %v, logs %q", rec.calls, logs)
	}
	for _, l := range logs {
		if !strings.Contains(l, "does not answer") || strings.Contains(l, tok) || strings.Contains(l, strings.TrimPrefix(tok, extTokenPrefix)) {
			t.Fatalf("log line %q", l)
		}
	}
}

// Issues and revocations from many goroutines reach the record in the order they changed the tokens: a revocation
// comes after the issue of the token it revokes, and names the extension's live token, never one replaced before.
func TestExtensionTokenRecordOrder(t *testing.T) {
	rec := &fakeTokenRecord{}
	h := newTestManager(t)
	h.extTokens.keepRecord(rec, "example-manager", nil)
	h.extTokens.revokeLastRun(context.Background())
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("example-%d", g%4) // two goroutines on each extension
			for i := range 25 {
				if i%3 == 2 {
					h.extTokens.revoke(id, db.ExtensionTokenStopped)
				} else {
					h.extTokens.issue(id, "Example", []string{extension.ScopeAgentsRead})
				}
			}
		}()
	}
	wg.Wait()
	h.extTokens.flush()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) == 0 || rec.calls[0][0] != "revoke_all" {
		t.Fatalf("the last run's tokens are revoked first: %v", rec.calls[:min(len(rec.calls), 3)])
	}
	live := map[string]string{} // extension → its live token's hash, as the record has it
	whose := map[string]string{}
	for _, c := range rec.calls[1:] {
		switch c[0] {
		case "issue":
			id, hash, _ := strings.Cut(c[1], " ")
			live[id], whose[hash] = hash, id
		case "revoke":
			id, ok := whose[c[1]]
			if !ok || live[id] != c[1] {
				t.Fatalf("revocation of %s before its issue, or of a token already replaced (live %q)", c[1], live[id])
			}
			delete(live, id)
		default:
			t.Fatalf("unexpected write %v", c)
		}
	}
	h.extTokens.mu.Lock()
	defer h.extTokens.mu.Unlock()
	if len(live) != len(h.extTokens.byExt) {
		t.Fatalf("the record's live tokens %v, the manager's %v", live, h.extTokens.byExt)
	}
	for id, hash := range h.extTokens.byExt {
		if live[id] != hash {
			t.Fatalf("%s: the record's live token %q, the manager's %q", id, live[id], hash)
		}
	}
}

// The hub is told the hash of each token the manager drops, revoked or replaced by the extension's next one, so that it
// ends what that token has open through it (extwire.HubTokenRevoked); nothing when there was no token to drop.
func TestDroppedTokensAreToldToTheHub(t *testing.T) {
	h := newTestManager(t)
	told := make(chan string, 4)
	th := hubOf(h)
	th.mu.Lock()
	th.app = func(method string, params json.RawMessage) (any, error) {
		if method == extwire.HubTokenRevoked {
			var a struct {
				Hash string `json:"hash"`
			}
			_ = json.Unmarshal(params, &a)
			told <- a.Hash
		}
		return map[string]any{}, nil
	}
	th.mu.Unlock()
	want := func(tok, what string) {
		t.Helper()
		select {
		case got := <-told:
			if got != hashToken(tok) {
				t.Fatalf("%s: told of %q, want the dropped token's hash", what, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the hub was not told", what)
		}
	}
	first := h.extTokens.issue("example", "Example", []string{extension.ScopeAgentsRead})
	second := h.extTokens.issue("example", "Example", []string{extension.ScopeAgentsRead})
	want(first, "replaced")
	h.extTokens.revoke("example", db.ExtensionTokenStopped)
	want(second, "revoked")
	h.extTokens.revoke("example", db.ExtensionTokenStopped)
	select {
	case got := <-told:
		t.Fatalf("told of %q with no token left to drop", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// A notice that fails on the bus (it timed out, the connection went, the manager was off the bus a moment) is told
// again, a bounded number of times, until the manager stops; one the hub answered (an older hub without the method) is
// not.
func TestDroppedTokenNoticeIsToldAgain(t *testing.T) {
	h := newTestManager(t)
	h.droppedWait = time.Millisecond
	th := hubOf(h)
	failing := make([]error, tellHubDroppedTries+2)
	for i := range failing {
		failing[i] = context.DeadlineExceeded
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	for i, c := range []struct {
		name    string
		fails   []error
		stopped bool
		want    int
	}{
		{"failed twice", []error{context.DeadlineExceeded, rpc.ErrDropped}, false, 3},
		{"answered", []error{&rpc.Error{Message: "unknown method"}}, false, 1},
		{"off the bus a moment", []error{bus.ErrOffline, bus.ErrOffline}, false, 3},
		{"never told", failing, false, tellHubDroppedTries},
		{"manager stopped", failing, true, 1},
	} {
		h.ctx = nil
		if c.stopped {
			h.ctx = stopped
		}
		hash := hashToken(fmt.Sprintf("pext_%d", i))
		var mu sync.Mutex
		tries := 0
		th.mu.Lock()
		th.app = func(method string, params json.RawMessage) (any, error) {
			var a struct {
				Hash string `json:"hash"`
			}
			_ = json.Unmarshal(params, &a)
			if method != extwire.HubTokenRevoked || a.Hash != hash {
				return map[string]any{}, nil
			}
			mu.Lock()
			defer mu.Unlock()
			tries++
			if tries <= len(c.fails) {
				return nil, c.fails[tries-1]
			}
			return map[string]any{}, nil
		}
		th.mu.Unlock()
		h.noticeDropped(hash)
		mu.Lock()
		n := tries
		mu.Unlock()
		if n != c.want {
			t.Fatalf("%s: asked %d times, want %d", c.name, n, c.want)
		}
	}
}

// A manager with no hub at all has no one to tell, and does not try again (which would end in a log line).
func TestDroppedTokenNoticeWithNoHub(t *testing.T) {
	h := newTestManager(t)
	h.droppedWait = time.Millisecond
	h.opts.Hub = nil
	logged := make(chan string, 1)
	h.opts.Logf = func(format string, args ...any) {
		if line := fmt.Sprintf(format, args...); strings.Contains(line, "dropped token") {
			select {
			case logged <- line:
			default:
			}
		}
	}
	h.noticeDropped(hashToken("pext_none"))
	select {
	case got := <-logged:
		t.Fatalf("tried again with no hub: %s", got)
	default:
	}
}

// The notice is told in the background: a hub slow to answer it holds up no revoke, nor the token record after it.
func TestDroppedTokenNoticeHoldsUpNoRevoke(t *testing.T) {
	h := newTestManager(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	th := hubOf(h)
	th.mu.Lock()
	th.app = func(method string, params json.RawMessage) (any, error) {
		if method == extwire.HubTokenRevoked {
			once.Do(func() { close(entered) })
			<-release
		}
		return map[string]any{}, nil
	}
	th.mu.Unlock()
	defer close(release)
	rec := &fakeTokenRecord{}
	h.extTokens.keepRecord(rec, "example-manager", nil)
	tok := h.extTokens.issue("example", "Example", []string{extension.ScopeAgentsRead})
	done := make(chan struct{})
	go func() {
		h.extTokens.revoke("example", db.ExtensionTokenStopped)
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the hub was not told")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("revoking waited for the hub to answer the notice")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if n := len(rec.calls); n == 0 || rec.calls[n-1] != [3]string{"revoke", hashToken(tok), db.ExtensionTokenStopped} {
		t.Fatalf("record writes %v", rec.calls)
	}
}
