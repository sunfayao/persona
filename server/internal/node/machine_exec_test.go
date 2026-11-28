package node

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// machine.exec's command may not name the files classifyMachineSensitivePath calls hub_config, wherever they are, nor
// the data directory's secrets by the path guard's rules; a project's file of the same name elsewhere is its own.
func TestMachineExecRefusesPersonasCredentialFiles(t *testing.T) {
	n := &Node{home: "/home/u", dir: "/home/u/.persona"}
	for path, want := range map[string]bool{
		"/home/u/.persona/hub.json":                        true,
		"/srv/app/hub.json.bak":                            true,
		"/srv/app/HANDLES.KEY":                             true,
		"/home/u/.persona/managers/assistant/manager.json": true,
		"/home/u/.persona/tailcat.json":                    true,
		"/home/u/.persona/tls/hub.pem":                     true,
		"/home/u/.persona/logs/node.log":                   false,
		"/home/u/.persona/assistant/CLAUDE.md":             false,
		"/srv/app/tailcat.json":                            false,
		"/srv/app/certs/dev.pem":                           false,
	} {
		if got := n.execRefused(path); got != want {
			t.Errorf("execRefused(%s) = %v, want %v", path, got, want)
		}
	}
}

// The method is in the node's table; a refused command answers before anything starts.
func TestMachineExecIsANodeMethod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("machine.exec refuses Windows")
	}
	if nodeMethods.handlers[proto.MethodMachineExec] == nil {
		t.Fatal("machine.exec is not in the method table")
	}
	n := &Node{home: t.TempDir()}
	n.dir = n.home + "/.persona"
	_, err := n.handle(t.Context(), proto.MethodMachineExec, json.RawMessage(`{"command":"cat ~/.persona/hub.json","timeout_ms":1000}`))
	if err == nil || !strings.Contains(err.Error(), "refused, nothing ran") {
		t.Fatalf("err %v", err)
	}
}
