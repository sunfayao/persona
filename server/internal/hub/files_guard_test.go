package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The built-in manager's files are its own (managerconf.OwnFiles): the identity the
// computers trust over tailcat (the manager dials them). The hub opens none of them,
// in the data directory or the manager's; the copies left in the data directory are for an older build only. The hub's
// own secret.key (secretkey.go) is the hub's.
func TestTheHubOpensNoneOfTheManagersFiles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"tailcat-hub.json", "HubKeyPath", "LoadOrCreateHubKey", "managerconf.OwnFile",
		"managerconf.TailcatKey", "managerconf.SecretKey"}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range forbidden {
			if strings.Contains(string(b), f) {
				t.Errorf("%s mentions %s: the built-in manager's own file, not the hub's (AGENTS/04-database.md)", name, f)
			}
		}
	}
}
