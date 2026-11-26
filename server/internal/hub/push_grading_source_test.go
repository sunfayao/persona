package hub

import (
	"context"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

// A merged row has only one destination. Never let it absorb a different push's source, including an older row with
// no source at all; identical sources keep the existing same-minute and same-key coalescing behavior.
func TestPushGradingP1MergesOnlyCompatibleSources(t *testing.T) {
	notification := func(title, key, machine, session string) assistantwire.Notification {
		n := gradingNotification(assistantwire.LevelP1, key, title)
		n.DeviceID, n.SessionID = machine, session
		return n
	}
	tests := []struct {
		name                               string
		first                              assistantwire.Notification
		second                             assistantwire.Notification
		wantRows                           int
		wantAlso                           int
		wantTodo, wantMachine, wantSession string
		moveMinute                         bool
	}{
		{
			name: "identical sources merge", wantRows: 1, wantAlso: 1, wantMachine: "mac", wantSession: "agent-a",
			first: notification("first", "subject-a", "mac", "agent-a"), second: notification("second", "subject-b", "mac", "agent-a"),
		},
		{
			name: "different sessions stay separate", wantRows: 2,
			first: notification("first", "subject-a", "mac", "agent-a"), second: notification("second", "subject-b", "mac", "agent-b"),
		},
		{
			name: "missing and present sources stay separate", wantRows: 2,
			first: notification("first", "subject-a", "", ""), second: notification("second", "subject-b", "mac", "agent-b"),
		},
		{
			name: "same key with different sources stays separate", wantRows: 2, moveMinute: true,
			first: notification("first", "shared", "mac", "agent-a"), second: notification("second", "shared", "mac", "agent-b"),
		},
		{
			name: "different todos stay separate", wantRows: 2,
			first: notification("first", "ask:todo_a", "mac", "agent-a"), second: notification("second", "ask:todo_b", "mac", "agent-a"),
		},
		{
			name: "different related URLs stay separate", wantRows: 2,
			first: notification("first", "https://example.test/a", "", ""), second: notification("second", "https://example.test/b", "", ""),
		},
		{
			name: "different web-special URLs stay separate", wantRows: 2,
			first: notification("first", "https:foo", "", ""), second: notification("second", "https:bar", "", ""),
		},
		{
			name: "legacy clickable and source-less rows stay separate", wantRows: 2,
			first: notification("first", "https://example.test/a", "", ""), second: notification("second", "subject-b", "", ""),
		},
		{
			name: "same related URL merges", wantRows: 1, wantAlso: 1,
			first: notification("first", "https://example.test/a", "", ""), second: notification("second", "https://example.test/a", "", ""),
		},
		{
			name: "same legacy todo merges across rounds", wantRows: 1, wantAlso: 1, wantTodo: "todo_same",
			first: notification("first", "todo:todo_same@round-1", "", ""), second: notification("second", "todo:todo_same@round-2", "", ""),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := gradingHub(t)
			if err := h.push.setPushGrading(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if err := h.deliverPush(tc.first); err != nil {
				t.Fatal(err)
			}
			if tc.moveMinute {
				moveGradingClock(h, h.push.now().Add(time.Minute))
			}
			if err := h.deliverPush(tc.second); err != nil {
				t.Fatal(err)
			}
			view := h.push.heldPushesView("", 0)
			if len(view) != tc.wantRows {
				t.Fatalf("got %d rows, want %d: %+v", len(view), tc.wantRows, view)
			}
			if tc.wantRows == 1 {
				got := view[0]
				if len(got.Also) != tc.wantAlso || got.TodoID != tc.wantTodo || got.MachineID != tc.wantMachine || got.SessionID != tc.wantSession {
					t.Fatalf("merged row = %+v, want the intact shared source and %d also titles", got, tc.wantAlso)
				}
			}
		})
	}
}
