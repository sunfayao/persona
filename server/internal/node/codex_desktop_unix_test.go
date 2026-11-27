//go:build !windows

package node

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// listenFakeCodexBus serves a fake App bus where the App on macOS does: a socket in the home's ipc directory.
func listenFakeCodexBus(t *testing.T) (home string, l net.Listener) {
	t.Helper()
	// A short path: a unix socket path over ~104 bytes (macOS's t.TempDir()) cannot be bound.
	root, err := os.MkdirTemp("/tmp", "cxbus")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	home = filepath.Join(root, ".codex")
	os.MkdirAll(filepath.Join(home, "ipc"), 0o700)
	l, err = net.Listen("unix", codexDesktopSocket(home))
	if err != nil {
		t.Fatal(err)
	}
	return home, l
}

// noFakeCodexBus: no bus is served where the test looks (each test home has its own socket path).
func noFakeCodexBus(*testing.T) {}

func TestCodexDesktopAvailableRequiresAListener(t *testing.T) {
	home, l := listenFakeCodexBus(t)
	t.Cleanup(func() { l.Close() })
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	if !codexDesktopAvailable(home) {
		t.Fatal("a listening legacy bus is unavailable")
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	path := codexDesktopSocket(home)
	if st, err := os.Stat(path); err != nil || st.Mode()&os.ModeSocket == 0 {
		t.Fatalf("missing stale socket fixture: %v, %v", st, err)
	}
	if codexDesktopAvailable(home) {
		t.Fatal("a stale socket advertises App control")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if codexDesktopAvailable(home) {
		t.Fatal("a regular file advertises App control")
	}
}

func TestCodexDesktopUnavailableRefusalLanguages(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		err := codexDesktopUnavailable(i18n.With(context.Background(), lang))
		if code, data := refusalOf(t, err); code != "codex_read_only" || data["delivered"] != deliveredNone || err.Error() != i18n.T(lang, codexDesktopUnavailableKey) || err.Error() == codexDesktopUnavailableKey {
			t.Errorf("%s: %v, data %v", lang, err, data)
		}
	}
}
