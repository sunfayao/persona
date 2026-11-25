package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

const quotedWords = "please ship the export fix before Friday, and keep the old format working"

var quotedAt = time.Date(2026, 11, 17, 4, 52, 0, 0, time.UTC)

// ownerItem is a message the owner sent from the app (it has a client message id).
func ownerItem(id, text string) proto.Item {
	return proto.Item{ID: id, Seq: 1, TS: quotedAt, Role: "user", Kind: proto.KindText, Text: text, ClientMsgID: "c-" + id}
}

// received is the item a relay leaves in the receiving conversation: the text the hub sent, from conversation from.
func received(t *testing.T, f *fakeHub, from string) proto.Item {
	t.Helper()
	text, _ := f.lastSend(t)["text"].(string)
	return proto.Item{ID: "r1", Seq: 2, TS: quotedAt.Add(time.Minute), Role: "user", Kind: proto.KindText, Text: text,
		Source: &proto.MessageSource{Kind: proto.SourceConversation, ConversationID: from}}
}

// planBrief is who started work session hands out now, as task_plan, agent_start and the others record it.
func planBrief(t *testing.T, d *dispatchPolicyHarness, session string) briefRecord {
	t.Helper()
	return d.s.briefOrigin(context.Background(), "server", session)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
