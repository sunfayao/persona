package node

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// codexDaemonHome is a Codex home whose 0.163 control alias points where Codex puts the daemon's socket (nothing is
// there: the test never makes it) and whose daemon.pid names pid.
func codexDaemonHome(t *testing.T, home string, pid int) {
	t.Helper()
	ctl := filepath.Join(home, ".codex", "app-server-control")
	if err := os.MkdirAll(ctl, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ctl, 0o700); err != nil {
		t.Fatal(err)
	}
	parent, _ := filepath.EvalSymlinks(ctl)
	tmp, _ := filepath.EvalSymlinks("/tmp")
	sum := sha256.Sum256([]byte(filepath.Join(parent, "app-server-control.sock")))
	target := filepath.Join(tmp, "codex-daemon-"+strconv.Itoa(os.Geteuid()), hex.EncodeToString(sum[:]))
	if err := os.Symlink(target, filepath.Join(ctl, "app-server-control.sock")); err != nil {
		t.Fatal(err)
	}
	daemon := filepath.Join(home, ".codex", "app-server-daemon")
	if err := os.MkdirAll(daemon, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(daemon, "daemon.pid"), fmt.Appendf(nil, `{"pid":%d,"processStartTime":"x"}`, pid), 0o600); err != nil {
		t.Fatal(err)
	}
}
