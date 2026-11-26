//go:build darwin || linux

package node

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"golang.org/x/sys/unix"
)

func releasedDesktopFixture(t *testing.T) (*Node, codexSource, string, string) {
	t.Helper()
	n := remoteTestNode(t)
	// A Unix domain socket has a short maximum path even with a private test directory.
	home, err := os.MkdirTemp("", "pdc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	cwd := t.TempDir()
	writeMigratedCodexApp(t, home, codexAppSchema+
		codexAppRow(appChat, "", cwd, "Saved desktop chat", time.Now(), 0)+
		`ALTER TABLE threads ADD COLUMN history_mode TEXT DEFAULT 'paginated';`)
	writeCodexHistory(t, home, []historyTurn{{thread: appChat, id: "saved-turn", status: "completed", ordinal: 0,
		items: [][2]string{userItem("saved", "Keep this original history")}}})
	lockDir := filepath.Join(home, "thread-writer-locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, ".coordination.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id := n.registerCodex(home, appChat, "")
	src, ok := n.codexSource(id)
	if !ok || src.path != "" || id == src.thread {
		t.Fatalf("expected a rolloutless chat with a home-scoped identity: %+v", src)
	}
	return n, src, id, cwd
}

func desktopResumeWriter(t *testing.T, src codexSource) *os.File {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(src.home, "thread-writer-locks", src.thread+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return f
}

func releasedDesktopRemote(t *testing.T, n *Node, src codexSource, cwd string) *remoteFixture {
	t.Helper()
	path := filepath.Join(src.home, "app-server-control", "app-server-control.sock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	f := &remoteFixture{home: src.home}
	f.response = func(method string, p map[string]any) (any, bool) {
		if method != "thread/resume" && method != "thread/read" {
			return nil, false
		}
		if method == "thread/resume" {
			f.mu.Lock()
			f.threads = []string{src.thread}
			f.mu.Unlock()
		}
		return map[string]any{"thread": map[string]any{
			"id": src.thread, "name": "Saved desktop chat", "cwd": cwd,
			"status": map[string]any{"type": "idle"},
		}}, true
	}
	server := &http.Server{Handler: http.HandlerFunc(f.serve)}
	go func() { _ = server.Serve(listener) }()
	ctx, cancel := context.WithCancel(context.Background())
	spec := proto.CodexServer{ID: "released-desktop", Home: src.home, Endpoint: "unix://" + path}
	n.ensureCodexRemote(ctx, spec)
	t.Cleanup(func() { cancel(); _ = server.Close() })
	waitFor(t, "same-home local server ready", func() bool {
		n.codices.mu.Lock()
		c := n.codices.remotes[spec.ID]
		n.codices.mu.Unlock()
		if c == nil {
			return false
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.ready && c.verifiedHome
	})
	return f
}

func TestReleasedCodexDesktopContinuesPaginatedOriginal(t *testing.T) {
	n, src, id, cwd := releasedDesktopFixture(t)
	writer := desktopResumeWriter(t, src)
	row := rowsByThread(n.codexSessions(map[string]bool{}))[src.thread]
	if row.Capabilities == nil || row.Capabilities.Send || row.Capabilities.Takeover != nil {
		t.Fatalf("held Desktop writer was advertised as writable: %+v", row)
	}
	if _, err := n.prepareCodexTakeover(t.Context(), id); refusalCode(err) != "codex_takeover_writer_busy" {
		t.Fatalf("held writer preparation returned %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	row = rowsByThread(n.codexSessions(map[string]bool{}))[src.thread]
	if row.Capabilities == nil || row.Capabilities.Takeover == nil || !row.Capabilities.Takeover.ReleasedDesktop || row.Capabilities.Send {
		t.Fatalf("released thread did not expose Continue: %+v", row)
	}
	plan, err := n.prepareCodexTakeover(t.Context(), id)
	if err != nil || !plan.ReleasedDesktop || plan.Running || plan.Cwd != cwd || plan.ContinuationPrompt != "" {
		t.Fatalf("prepare returned %+v, %v", plan, err)
	}
	remote := releasedDesktopRemote(t, n, src, cwd)
	n.takeovers.stop = func(context.Context, codexWriter) error {
		t.Error("Desktop continuation should never stop a writer")
		return nil
	}
	got, err := n.commitCodexTakeover(t.Context(), id, plan.Token, false)
	if err != nil || got.SessionID != id || got.SourceThreadID != src.thread || got.Cwd != cwd ||
		got.SpaceKind != "codex_app" || !got.Capabilities.Send {
		t.Fatalf("commit returned %+v, %v", got, err)
	}
	resumes := remote.sent("thread/resume")
	if len(resumes) != 1 || remote.count("turn/start") != 0 || remote.count("thread/start") != 0 {
		t.Fatalf("resume must load one old thread without sending: %v", resumes)
	}
	params := resumes[0]
	if len(params) != 2 || params["threadId"] != src.thread || params["excludeTurns"] != true {
		t.Fatalf("wrong native resume parameters: %v", params)
	}
	if path, err := n.file(id); err != nil || path == nil {
		t.Fatalf("original paginated history no longer readable: %v", err)
	}

	owner, thread, err := n.codexOwner(id)
	if err != nil || !thread.releasedDesktop || n.codexInventory().Joined[id] != owner.spec.ID {
		t.Fatalf("Desktop join was not remembered: %+v, %v", thread, err)
	}
	if err := owner.refresh(); err != nil {
		t.Fatal(err)
	}
	_, thread, _ = n.codexOwner(id)
	if !thread.releasedDesktop {
		t.Fatal("ordinary metadata refresh lost Desktop ownership")
	}
	// A new registry refresh after reconnect has the remembered join and the
	// local server's loaded list, but no previous in-memory thread metadata.
	owner.mu.Lock()
	delete(owner.threads, src.thread)
	owner.mu.Unlock()
	if err := owner.refresh(); err != nil {
		t.Fatal(err)
	}
	_, thread, _ = n.codexOwner(id)
	if !thread.releasedDesktop {
		t.Fatal("the same local server's loaded writer lost its continuation on reconnect")
	}
	if err := owner.join(t.Context(), id, false); err != nil {
		t.Fatal(err)
	}

	// An App chat originally created in Persona used the old Desktop IPC path.
	// A confirmed continuation owns its sends now; a stale shared row with no loaded thread is refused.
	n.state.put(id, sessionState{Launcher: codexAppLauncher, Cwd: cwd})
	remote.mu.Lock()
	remote.threads = nil
	remote.mu.Unlock()
	if err := n.sendLocked(t.Context(), id, "Follow-up while unloaded", nil, sendOpts{}); refusalCode(err) != "codex_takeover_writer_busy" {
		t.Fatalf("unloaded thread send returned %v", err)
	}
	if remote.count("turn/start") != 0 {
		t.Fatal("a turn was sent after the native writer released this thread")
	}
	remote.mu.Lock()
	remote.threads = []string{src.thread}
	remote.mu.Unlock()
	if err := n.sendLocked(t.Context(), id, "Follow-up to the original", nil, sendOpts{}); err != nil {
		t.Fatal(err)
	}
	if remote.count("turn/start") != 1 || remote.count("thread/start") != 0 {
		t.Fatal("follow-up did not use exactly one turn on the original")
	}
}

func TestReleasedCodexDesktopRechecksWriterAndArchive(t *testing.T) {
	for _, change := range []string{"new writer", "archived"} {
		t.Run(change, func(t *testing.T) {
			n, src, id, _ := releasedDesktopFixture(t)
			plan, err := n.prepareCodexTakeover(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			want := "codex_takeover_writer_busy"
			if change == "new writer" {
				desktopResumeWriter(t, src)
			} else {
				want = "codex_takeover_desktop_resume_unavailable"
				if err := exec.Command(sqlite3WithJSON(t), codexAppDBPath(src.home),
					`UPDATE threads SET archived = 1 WHERE id = '`+src.thread+`'`).Run(); err != nil {
					t.Fatal(err)
				}
			}
			remote, err := n.commitCodexTakeover(t.Context(), id, plan.Token, false)
			if remote != nil || refusalCode(err) != want {
				t.Fatalf("commit after %s returned %+v, %v", change, remote, err)
			}
		})
	}
}

func TestReleasedCodexDesktopIndexedRolloutWithoutSidebarMembership(t *testing.T) {
	n, src, id, cwd := releasedDesktopFixture(t)
	stamp := time.Now().Add(-2 * time.Hour)
	path := writeAppRollout(t, src.home, src.thread, cwd, stamp)
	query := `ALTER TABLE threads ADD COLUMN project_id TEXT;
		UPDATE threads SET rollout_path = '` + strings.ReplaceAll(path, "'", "''") + `' WHERE id = '` + src.thread + `';`
	if err := exec.Command(sqlite3WithJSON(t), codexAppDBPath(src.home), query).Run(); err != nil {
		t.Fatal(err)
	}
	n.registerCodex(src.home, src.thread, path)
	src, _ = n.codexSource(id)
	if codexDesktopChat(src) {
		t.Fatal("fixture unexpectedly has legacy Desktop control identity")
	}
	// Bundled Codex can also produce a Desktop-looking rollout with no
	// corresponding App index row, which must stay view-only.
	other := writeAppRollout(t, src.home, remoteOther, cwd, stamp)
	n.registerCodex(src.home, remoteOther, other)

	writer := desktopResumeWriter(t, src)
	row := rowsByThread(n.codexSessions(map[string]bool{}))[src.thread]
	if row.Capabilities == nil || row.Capabilities.Send || row.Capabilities.Takeover != nil {
		t.Fatalf("held indexed Desktop rollout was advertised for continuation: %+v", row)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rows := rowsByThread(n.codexSessions(map[string]bool{}))
	row = rows[src.thread]
	if row.Capabilities == nil || row.Capabilities.Send || row.Capabilities.Takeover == nil || !row.Capabilities.Takeover.ReleasedDesktop {
		t.Fatalf("unheld indexed Desktop rollout did not offer continuation: %+v", row)
	}
	if r := rows[remoteOther]; r.Capabilities != nil && r.Capabilities.Takeover != nil {
		t.Fatalf("rollout alone established Desktop ownership: %+v", r)
	}
	n.takeovers.writers = func(codexSource) ([]codexWriter, error) { return nil, nil }
	plan, err := n.prepareCodexTakeover(t.Context(), id)
	if err != nil || !plan.ReleasedDesktop || plan.Running {
		t.Fatalf("prepare returned %+v, %v", plan, err)
	}
	remote := releasedDesktopRemote(t, n, src, cwd)
	got, err := n.commitCodexTakeover(t.Context(), id, plan.Token, false)
	if err != nil || got.SessionID != id || got.SourceThreadID != src.thread || !got.Capabilities.Send {
		t.Fatalf("commit returned %+v, %v", got, err)
	}
	if remote.count("thread/resume") != 1 || remote.count("thread/start") != 0 || remote.count("turn/start") != 0 {
		t.Fatal("continue must resume exactly the original thread without creating a new thread or turn")
	}
}
