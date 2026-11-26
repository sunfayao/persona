package assistant

import (
	"context"
	"github.com/sunfayao/persona/server/internal/compat"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// A manager other than the built-in one keeps its assistant and health directories of its own on a computer
// (.persona/assistant-<id>, .persona/health-<id>); the built-in one's are as they were. Another manager's directory,
// left on the computer when it moved, is not its conversations'.
func TestAManagersOwnDirectories(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		id, assistant, health string
	}{
		{"", ".persona/assistant", ".persona/tracks/qa-private"},
		{"m-0b0b0b", ".persona/assistant-m-0b0b0b", ".persona/tracks-m-0b0b0b/qa-private"},
	} {
		f := spaceHub()
		s := withSettings(t, f, New(Options{Hub: f, Store: func() Store { return &memStore{} }, Logf: testLogf(t), ID: c.id}))
		for _, space := range []string{""} {
			if _, err := s.prepareSpace(ctx, "server", space); err != nil {
				t.Fatal(err)
			}
		}
		if len(f.prepared) != 1 || f.prepared[0]["dir"] != compat.StateDir+"/"+c.assistant[len(".persona/"):] {
			t.Fatalf("%q prepared %v", c.id, f.prepared)
		}
		d := Device{ID: "server", Home: "/home/me"}
		if got := s.assistantCwd(d); got != "/home/me/"+c.assistant {
			t.Fatalf("%q: assistant directory %q", c.id, got)
		}
		d.DataDir = "/data"
		if got := s.spaceCwd(context.Background(), d, testSpace); got != "/data/"+c.health[len(".persona/"):] {
			t.Fatalf("%q: health directory under a data directory %q", c.id, got)
		}
	}
	builtin := fleetMachine{ID: "server", Home: "/home/me", manager: ""}
	other := fleetMachine{ID: "server", Home: "/home/me", manager: "m-0b0b0b"}
	for cwd, want := range map[string][2]bool{
		"/home/me/.persona/assistant":                  {true, false},
		"/home/me/.persona/tracks/qa-private":          {true, false},
		"/home/me/.persona/assistant-m-0b0b0b":         {false, true},
		"/home/me/.persona/tracks-m-0b0b0b/qa-private": {false, true},
		"/home/me/code/app":                            {false, false},
	} {
		sess := proto.Session{SessionID: "s", Cwd: cwd}
		if got := [2]bool{builtin.isAssistant(sess), other.isAssistant(sess)}; got != want {
			t.Errorf("%s: the built-in's %v, m-0b0b0b's %v, want %v", cwd, got[0], got[1], want)
		}
	}
}
