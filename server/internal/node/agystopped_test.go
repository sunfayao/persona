package node

import (
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// What the conversation does in the minute after the close is the close's own; after it, work, a change or a live
// pane says it runs again, and its mark goes. A pane the close did not take away is one after that minute.
func TestAgyStopMarkGoesWhenItRunsAgain(t *testing.T) {
	const conv = "agy-" + agyTestID
	for _, c := range []struct {
		name    string
		ago     time.Duration
		status  string
		changed bool
		live    map[string]string
		stopped bool
	}{
		{"done after the close", 0, proto.StatusDone, false, nil, true},
		{"working in the close's minute", 0, proto.StatusWorking, false, nil, true},
		{"changed in the close's minute", 0, proto.StatusDone, true, nil, true},
		{"its pane still there in that minute", 0, proto.StatusDone, false, map[string]string{agyTestRow: conv}, true},
		{"working after it", 2 * time.Minute, proto.StatusWorking, false, nil, false},
		{"waiting on the user after it", 2 * time.Minute, proto.StatusBlocked, false, nil, false},
		{"changed after it", 2 * time.Minute, proto.StatusDone, true, nil, false},
		{"its pane still there after it", 2 * time.Minute, proto.StatusDone, false, map[string]string{agyTestRow: conv}, false},
		{"another pane shows it", 0, proto.StatusDone, false, map[string]string{"agy-w1-p2": conv}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			closed := time.Now().Add(-c.ago)
			n := &Node{agyStopped: map[string]agyStop{agyTestID: {row: agyTestRow, at: closed}}}
			row := proto.Session{SessionID: conv, Agent: agyAgent, Status: c.status, UpdatedAt: closed.Add(-time.Hour)}
			if c.changed {
				row.UpdatedAt = time.Now()
			}
			list := []proto.Session{row}
			n.markAgyStopped(list, c.live)
			stopped := list[0].Status == proto.StatusOffline && list[0].Capabilities != nil && list[0].Capabilities.StopCause == proto.StopUser
			if _, kept := n.agyStopped[agyTestID]; stopped != c.stopped || kept != c.stopped {
				t.Fatalf("stopped %v (row %+v), mark kept %v; want %v", stopped, list[0], kept, c.stopped)
			}
		})
	}
}

// Antigravity keeps no archive of its own: the node says so for an agy row no runtime takes, a pane's or a
// conversation's, instead of refusing its id (#14520).
func TestNativeArchiveOfAnAgyRow(t *testing.T) {
	n := &Node{}
	for _, id := range []string{agyTestRow, "agy-" + agyTestID} {
		got, err := n.nativeArchive(t.Context(), id, true)
		if err != nil || got.Native != nativeArchiveUnsupported {
			t.Fatalf("%s: %+v %v", id, got, err)
		}
	}
	if _, err := n.nativeArchive(t.Context(), "not a session", true); err == nil {
		t.Fatal("an invalid id was taken")
	}
}
