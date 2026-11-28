package ownerattention

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestProjectQuestionsAndApprovals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		managed   bool
		cards     Cards
		owner     bool
		wantState string
		wantFlag  bool
	}{
		{"managed structured question", true, Cards{Questions: 1}, false, proto.StateWorking, false},
		{"managed text-only wait", true, Cards{}, false, proto.StateWorking, false},
		{"manual agent question", false, Cards{Questions: 1}, false, proto.StateNeedsYou, false},
		{"permission or plan remains actionable", true, Cards{Questions: 1, OwnerOnly: 1}, false, proto.StateNeedsYou, false},
		{"main has an escalated decision", false, Cards{}, true, proto.StateNeedsYou, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := proto.Session{State: proto.StateNeedsYou, AwaitsReply: true, PendingInteractions: tc.cards.Questions + tc.cards.OwnerOnly}
			if tc.owner {
				row.State = proto.StateIdle
			}
			Project(&row, tc.managed, tc.cards, 0, tc.owner)
			if row.State != tc.wantState || row.OwnerNeedsYou != tc.wantFlag {
				t.Fatalf("attention = %q, owner flag = %v", row.State, row.OwnerNeedsYou)
			}
			if row.State == proto.StateWorking && (row.PendingInteractions != 0 || row.AwaitsReply) {
				t.Fatalf("supervisor's question remains in owner counters: %+v", row)
			}
		})
	}
}
