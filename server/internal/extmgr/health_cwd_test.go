package extmgr

import (
	"testing"

	"github.com/nwverse/persona/server/internal/connector"

	"github.com/nwverse/persona/server/internal/proto"
)

// A health conversation's connector writes are confirmed each time, a user's rule or not, also when its row's
// directory is a folder inside the health space (an older node lists where a `cd` left it, #2368). A directory that
// only starts the same is not the space.
func TestHealthConversationsWritesFromItsFoldersAreConfirmed(t *testing.T) {
	h, gh := chatRuleManager(t, nil, []connector.UserRule{ghUserRule(12)})
	box := h.machineByID("box")
	for cwd, asked := range map[string]bool{
		"/home/u/.persona/assistant":                 false, // the rule lets the chat through
		"/home/u/.persona/tracks/qa-private":         true,
		"/home/u/.persona/tracks/qa-private/reports": true,
		"/home/u/.persona/health-bin":                false,
	} {
		listSessions(h, box.cfg.ID, []proto.Session{{SessionID: "conv1", Cwd: cwd}})
		if got := chatWrite(t, h, gh, "github_comment", chatIssue12); got != asked {
			t.Errorf("a write from %s: asked %v, want %v", cwd, got, asked)
		}
	}
}
