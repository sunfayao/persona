//go:build darwin || linux

package node

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/osproc"
	"golang.org/x/sys/unix"
)

// Codex's writer-lock directory coordinates all native app-servers in this home.
// Never create, unlink or modify the files. Hold the coordinator briefly so an
// absent thread lock cannot be a gap while a writer opens or removes it.
func codexDesktopWriterReleased(src codexSource) bool {
	if !uuidRe.MatchString(src.thread) {
		return false
	}
	dir, err := os.OpenRoot(filepath.Join(src.home, "thread-writer-locks"))
	if err != nil {
		return false
	}
	defer dir.Close()
	open := func(name string) (*os.File, error) {
		f, err := dir.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || !osproc.OwnedByMe(info) || info.Mode().Perm()&0o022 != 0 {
			_ = f.Close()
			return nil, fs.ErrPermission
		}
		return f, nil
	}
	coordination, err := open(".coordination.lock")
	if err != nil {
		return false
	}
	defer coordination.Close()
	if unix.Flock(int(coordination.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return false
	}
	writer, err := open(src.thread + ".lock")
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	defer writer.Close()
	return unix.Flock(int(writer.Fd()), unix.LOCK_SH|unix.LOCK_NB) == nil
}
