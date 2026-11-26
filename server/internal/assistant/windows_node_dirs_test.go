package assistant

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// A Windows node's main chat, health space and meeting room conversations are the assistant's on a hub of any
// system (#6419).
func TestWindowsNodeConversationsOnAnyHub(t *testing.T) {
	m := fleetMachine{ID: "pc", Home: `C:\Users\me`}
	for _, c := range []struct {
		cwd  string
		want bool
	}{
		{`C:\Users\me\.persona\assistant`, true},
		{`C:\Users\me\code\app`, false},
	} {
		if got := m.isAssistant(proto.Session{SessionID: "s", Cwd: c.cwd}); got != c.want {
			t.Errorf("isAssistant(%q) = %v, want %v", c.cwd, got, c.want)
		}
	}
}
