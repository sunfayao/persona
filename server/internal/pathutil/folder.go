package pathutil

import (
	"path/filepath"
	"runtime"
	"strings"
)

// The Codex desktop App keeps a local message bus — <CODEX_HOME>/ipc/ipc.sock, on Windows the named pipe
// \\.\pipe\codex-ipc (codex_desktop_unix.go, codex_desktop_windows.go): its windows ask each other to run a turn in a
// thread one of them owns. Persona joins that bus as one more client, so a message from the phone runs in the App
// window that owns the thread — it shows in the App, and no second writer opens the rollout.
//
// Frames are a 4-byte little-endian length and a JSON object. Every request carries the version the App expects for
// its method; without it no client claims the request and the bus answers "no-client-found".

// pathWithin: dir is root or under it (both clean). Windows paths do not tell case apart (the App and a rollout may
// spell a drive or a folder differently), and a drive's root ends in its separator already.
func WithinFolder(dir, root string) bool {
	if runtime.GOOS == "windows" {
		dir, root = strings.ToLower(filepath.Clean(dir)), strings.TrimSuffix(strings.ToLower(root), `\`)
	}
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

// folderKey: Windows paths do not tell case apart (see pathWithin).
func FolderKey(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
