package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/sunfayao/persona/server/"

// The device's program links the node and its clients, never the hub, the manager or the database, on any platform it
// ships for (the self-contained packaging plan, step 4).
func TestPersonaNodeDoesNotLinkTheHubOrTheManager(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool:", err)
	}
	// The hub's and the manager's own packages, and their sources and channels (they run in the manager).
	forbidden := []string{"internal/hub", "internal/hubedge", "internal/assistant", "internal/manager", "internal/devices",
		"internal/extmgr", "internal/db", "internal/voice", "internal/mail", "internal/lark",
		"internal/feishubot", "internal/bus",
		"internal/usagesource", "internal/admin"}
	// No database driver either, wherever it comes from.
	drivers := []string{"github.com/jackc/pgx", "github.com/lib/pq", "database/sql/driver"}
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/arm64", "darwin/amd64", "windows/amd64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		cmd := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list -deps (%s): %v", target, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, d := range drivers {
				if dep == d || strings.HasPrefix(dep, d+"/") {
					t.Errorf("persona-node (%s) links the database driver %s", target, dep)
				}
			}
			rel, ok := strings.CutPrefix(dep, modulePath)
			if !ok {
				continue
			}
			for _, f := range forbidden {
				if rel == f || strings.HasPrefix(rel, f+"/") {
					t.Errorf("persona-node (%s) depends on %s: the device's program reaches the manager only over the node "+
						"protocol; move what it needs into internal/node, internal/nodecmd or a contract package", target, dep)
				}
			}
		}
	}
}
