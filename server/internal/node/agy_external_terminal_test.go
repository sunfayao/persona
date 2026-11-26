//go:build darwin || linux

package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
	"golang.org/x/sys/unix"
)

func TestAgyExternalTerminalTracksNativeLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", root)
	n := new(Node)
	const id = "01234567-89ab-cdef-0123-456789abcdef"
	modes := true
	caps := &proto.SessionCapabilities{Read: true, Send: true, Modes: &modes}
	base := proto.Session{SessionID: "agy-" + id, Capabilities: caps}
	if err := os.Mkdir(filepath.Join(root, "presence"), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root, "presence", id+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	s := base
	n.markAgyExternalTerminal(&s)
	if s.Capabilities.Send || s.Capabilities.Modes != nil || !s.Capabilities.Read || s.AgentSurface != transcript.SurfaceCLI || s.ReadOnlyReason == "" {
		t.Fatalf("idle terminal still holding the lock: %+v", s)
	}
	if !base.Capabilities.Send {
		t.Fatal("the runtime's shared capabilities were mutated")
	}
	s = base
	s.PaneID = "w1:p1"
	n.markAgyExternalTerminal(&s)
	if s.Capabilities != caps {
		t.Fatalf("a managed pane lost its controls: %+v", s)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	s = base
	n.markAgyExternalTerminal(&s)
	if s.Capabilities != caps || s.ReadOnlyReason != "" {
		t.Fatalf("a released lock file still made the chat read-only: %+v", s)
	}
}
