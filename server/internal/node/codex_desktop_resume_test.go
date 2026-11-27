//go:build darwin || linux

package node

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
