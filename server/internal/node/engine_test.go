package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func hostKinds(n *Node) []string {
	var out []string
	for _, h := range n.hosts.All() {
		out = append(out, h.Kind())
	}
	return out
}

// node.info reports herdr, the one engine: available with the version its server said, or when its binary is on PATH
// while it is not running; not available when neither is there.
func TestEngineDetection(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(t.TempDir(), "no-herdr.sock"))
	t.Setenv("PATH", t.TempDir())
	if got := engines("0.9.3"); len(got) != 1 || got[0] != (proto.Engine{ID: "herdr", Name: "herdr", Available: true, Version: "0.9.3"}) {
		t.Errorf("answering herdr: %+v", got)
	}
	if got := engines(""); len(got) != 1 || got[0] != (proto.Engine{ID: "herdr", Name: "herdr"}) {
		t.Errorf("no herdr: %+v", got)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "herdr"), []byte("#!/bin/sh\necho herdr 0.9.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if got := engines(""); len(got) != 1 || got[0] != (proto.Engine{ID: "herdr", Name: "herdr", Available: true}) {
		t.Errorf("herdr installed, not running: %+v", got)
	}
}
