package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/proto"
)

// spaceMDNode is a fake node's SPACE.md of track qa-plain: what it holds, the calls it was made, and whether its
// computer is away.
type spaceMDNode struct {
	content string
	calls   []assistantwire.SpaceMDCall
	offline bool
}

// spaceMDService is spaceTrackService with nodes that keep SPACE.md, a conversation in qa-plain (plain-00-0000), and
// a node answering assistant.space_md the way node/assistant_space_md.go does, its digest the content's length.
func spaceMDService(t *testing.T) (*Service, *fakeHub, *spaceMDNode) {
	t.Helper()
	s, f := spaceTrackService(t)
	f.sessions["server"] = append(f.sessions["server"], proto.Session{SessionID: "plain-00-0000", Cwd: "/home/me/.persona/tracks/qa-plain",
		Assistant: true, Space: "qa-plain", Title: "QA plain", LastText: "budget"})
	// With devices the manager answers session.get from its own nodes' lists, never the fake hub's: the server's node
	// lists the same conversations, so selfSpace finds plain-00-0000 in qa-plain.
	fd := newFleetDevs()
	fd.add("server", "server").home = "/home/me"
	fd.setSessions("server", append([]proto.Session(nil), f.sessions["server"]...))
	s.devs = spaceDevs{fleetDevs: fd}
	n := &spaceMDNode{content: "# QA plain\n\n" + spaceStateBegin + "\n" + spaceStateEnd + "\n"}
	digest := func(s string) string { return fmt.Sprint(len(s)) }
	f.answerNode(assistantwire.DeviceAssistantSpaceMD, func(_ string, raw json.RawMessage) (any, error) {
		var c assistantwire.SpaceMDCall
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		n.calls = append(n.calls, c)
		if n.offline {
			return nil, fmt.Errorf("server: %w", apierr.ErrOffline)
		}
		cur := assistantwire.SpaceMD{Content: n.content, Digest: digest(n.content)}
		switch c.Op {
		case assistantwire.SpaceMDPutState:
			cur.State = c.State
		}
		return cur, nil
	})
	return s, f, n
}

// space_state_update (#13472) is offered to a space track's conversations only, routed to its handler, which refuses
// every other caller and an oversized text before the node is asked.
func TestSpaceStateUpdateTool(t *testing.T) {
	ctx := context.Background()
	s, _, n := spaceMDService(t)
	if toolListed(s.Tools(ctx), spaceStateTool) || toolListed(s.ToolsFor(ctx, "server", "plain-00-0000"), spaceStateTool) {
		t.Fatal("space_state_update is in the assistant's shared list")
	}
	if !toolListed(s.sessionMCPTools(ctx, "server", "plain-00-0000", true, false), spaceStateTool) {
		t.Fatal("the space's conversation lacks space_state_update")
	}
	for _, sid := range []string{"main-0000-0000", "sleep-000-0000"} {
		if toolListed(s.sessionMCPTools(ctx, "server", sid, true, false), spaceStateTool) {
			t.Errorf("%s is offered space_state_update", sid)
		}
	}

	res := s.mcpCall(ctx, "server", "plain-00-0000", spaceStateTool, json.RawMessage(`{"text":"Budget drafted.\n"}`), true)
	if res.IsError || len(n.calls) != 1 {
		t.Fatalf("from the space: %+v, %d calls", res, len(n.calls))
	}
	if c := n.calls[0]; c.Op != assistantwire.SpaceMDPutState || c.State != "Budget drafted." || c.Slug != "qa-plain" ||
		c.Dir != path.Join(compat.StateDir, "tracks/qa-plain") {
		t.Errorf("node call: %+v", c)
	}

	for _, tc := range []struct{ sid, args string }{
		{"main-0000-0000", `{"text":"x"}`},
		{"sleep-000-0000", `{"text":"x"}`},
		{"plain-00-0000", `{"text":"` + strings.Repeat("x", assistantwire.SpaceMDStateMax+1) + `"}`},
		{"plain-00-0000", `{"text":"x","extra":1}`},
		{"plain-00-0000", `{"state":"x"}`},
	} {
		if out, isErr := spaceTool(t, s, tc.sid, spaceStateTool, tc.args); !isErr {
			t.Errorf("%s %.40s: not refused: %s", tc.sid, tc.args, out)
		}
	}
	if res := s.mcpCall(ctx, "server", "main-0000-0000", spaceStateTool, json.RawMessage(`{"text":"x"}`), true); !res.IsError {
		t.Error("the main chat's call: not refused")
	}
	if len(n.calls) != 1 {
		t.Errorf("refused calls reached the node: %+v", n.calls[1:])
	}
}
