//go:build darwin || linux

package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// The agy TUI holds the presence lock for its whole lifetime, including while
// idle. A file left by a closed TUI has no lock and still accepts ACP turns.
func (n *Node) agyPresenceHeld(sessionID string) bool {
	id, ok := strings.CutPrefix(sessionID, agyAgent+"-")
	if !ok || !uuidRe.MatchString(id) {
		return false
	}
	root, err := os.OpenRoot(n.agyDataRoot())
	if err != nil {
		return false
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.Join("presence", id+".lock"), os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	return err == nil && info.Mode().IsRegular() &&
		errors.Is(unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB), unix.EWOULDBLOCK)
}
