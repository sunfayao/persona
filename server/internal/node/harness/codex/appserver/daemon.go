package appserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// MissingDaemonSocket reports a live daemon whose recognized control alias has lost its target.
func MissingDaemonSocket(home string, alive func(int) bool) bool {
	if home == "" {
		return false
	}
	alias := filepath.Join(home, "app-server-control", "app-server-control.sock")
	st, err := os.Lstat(alias)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		return false // no alias: no 0.163 daemon here, or a plain socket whose absence codex reports itself
	}
	target, err := SocketPath(alias)
	if err != nil {
		return false // not the alias codex makes: not ours to judge
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	b, err := os.ReadFile(filepath.Join(home, "app-server-daemon", "daemon.pid"))
	if err != nil {
		return false
	}
	var pid struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(b, &pid) != nil || pid.PID <= 0 || !alive(pid.PID) {
		return false // no daemon running: codex starts one and makes the socket again
	}
	return true
}
