package node

import (
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// withShellLaunchers has the node's login shell list names (launchers.go's cache, a minute fresh), not run a real one.
func withShellLaunchers(t *testing.T, names ...string) {
	t.Helper()
	list := make([]proto.Launcher, 0, len(names))
	for _, name := range names {
		list = append(list, proto.Launcher{Name: name})
	}
	launcherCache.Lock()
	oldAt, oldList := launcherCache.at, launcherCache.list
	launcherCache.at, launcherCache.list = time.Now(), list
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = oldAt, oldList
		launcherCache.Unlock()
	})
}
