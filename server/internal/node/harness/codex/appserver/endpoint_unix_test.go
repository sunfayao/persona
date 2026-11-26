//go:build !windows

package appserver

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// 0.163's control sockets are aliases: only the target Codex itself computes is followed.
func TestCodexSocketAlias(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil { // as Codex makes app-server-control/ (a umask may leave it group-writable)
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "app-server-control.sock")
	parent, _ := filepath.EvalSymlinks(dir)
	tmp, _ := filepath.EvalSymlinks("/tmp")
	sum := sha256.Sum256([]byte(filepath.Join(parent, "app-server-control.sock")))
	want := filepath.Join(tmp, "codex-daemon-"+strconv.Itoa(os.Geteuid()), hex.EncodeToString(sum[:]))
	if err := os.Symlink(want, alias); err != nil {
		t.Fatal(err)
	}
	if got, err := SocketPath(alias); err != nil || got != want {
		t.Errorf("alias: %q %v, want %q", got, err, want)
	}
	other := filepath.Join(dir, "other.sock")
	if err := os.Symlink(filepath.Join(tmp, "somewhere-else"), other); err != nil {
		t.Fatal(err)
	}
	if _, err := SocketPath(other); err == nil {
		t.Error("a symlink to anywhere else was followed")
	}
	plain := filepath.Join(dir, "plain.sock")
	if got, err := SocketPath(plain); err != nil || got != plain {
		t.Errorf("plain: %q %v", got, err)
	}
}
