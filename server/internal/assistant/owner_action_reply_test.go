package assistant

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
)

func ownerReplyTestService(t *testing.T) (*Service, *fakeHub, *ownerActionTestStore, *ownerActionBook) {
	t.Helper()
	f, _ := feishuHub("ou_owner", time.Unix(100, 0))
	on := true
	f.settings = &Settings{MachineID: "server", Main: "m1", OwnerReplyApproval: &on, PersistentOwnerActionCards: &on}
	st := &ownerActionTestStore{}
	s := newServiceOn(t, f, st)
	book, ok := newOwnerActionBook(st)
	if !ok {
		t.Fatal("owner-action test store must support CAS")
	}
	return s, f, st, book
}

func putOwnerReplyPending(s *Service, ix proto.Interaction) {
	s.fleet.mu.Lock()
	if s.fleet.interactions == nil {
		s.fleet.interactions = map[string]*pending{}
	}
	s.fleet.interactions[ix.ID] = &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	s.fleet.mu.Unlock()
}

func keepOwnerReplyCard(s *Service, ix proto.Interaction, messageID string) {
	putOwnerReplyPending(s, ix)
	s.fbot.mu.Lock()
	s.keepBotCardLocked(ix.ID, &botCard{MessageID: messageID, Ix: ix, At: time.Unix(1000, 0)})
	s.fbot.mu.Unlock()
}

func ownerReplyBatchIX(batch ownerActionBatch) proto.Interaction {
	return proto.Interaction{ID: ownerActionInteractionID(batch.ID, ""), MachineID: batch.MachineID, SessionID: batch.SessionID,
		Kind: proto.IxPermission, ToolName: ownerActionBatchTool, ApprovalBatchID: batch.ID, OwnerProposal: true, Resumable: true, NoExpiry: true}
}

func ownerReplyItemIX(batch ownerActionBatch, item ownerActionItem) proto.Interaction {
	return proto.Interaction{ID: ownerActionInteractionID(batch.ID, item.ID), MachineID: batch.MachineID, SessionID: batch.SessionID,
		Kind: proto.IxPermission, ToolName: item.Tool, ApprovalBatchID: batch.ID, ApprovalItemID: item.ID, OwnerProposal: true, Resumable: true, NoExpiry: true}
}

func ownerReplyEvent(parent string) feishubot.MessageEvent {
	var ev feishubot.MessageEvent
	ev.Message.MessageID = "om_reply"
	ev.Message.ParentID = parent
	ev.Message.ChatID = "oc_owner"
	ev.Message.ChatType = "p2p"
	return ev
}

// answerOwnerProof emulates the hub boundary: it returns the authenticated
// event only for the expected one-use action and exact proposal target.
func answerOwnerProof(f *fakeHub, action, interaction string, calls *int, mutate func(*assistantwire.OwnerApprovalProof)) {
	f.answerApp(AppInteractions, assistantwire.InteractionsOwnerAction, func(raw json.RawMessage) (any, error) {
		*calls++
		var p struct {
			Action        string `json:"action"`
			InteractionID string `json:"interaction_id"`
			ApprovalReply bool   `json:"approval_reply"`
			MessageID     string `json:"message_id"`
			ParentID      string `json:"parent_id"`
			ChatID        string `json:"chat_id"`
			Text          string `json:"text"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Action != action || p.InteractionID != interaction || !p.ApprovalReply {
			return nil, errors.New("owner proof target mismatch")
		}
		proof := assistantwire.OwnerApprovalProof{MessageID: p.MessageID, ParentID: p.ParentID, ChatID: p.ChatID, Text: p.Text}
		if mutate != nil {
			mutate(&proof)
		}
		return map[string]any{"owner_approval_proof": proof}, nil
	})
}

func TestOwnerApprovalPhraseIsTrimmedAndCaseFoldedOnly(t *testing.T) {
	on := true
	settings := Settings{OwnerReplyApproval: &on, OwnerApprovalPhrases: []string{"发", "Agree", "Yes"}}
	for _, text := range []string{"发", " 发\n", "Agree", "agree", " AGREE\t"} {
		if !ownerApprovalPhrase(settings, text) {
			t.Errorf("exact phrase %q did not match", text)
		}
	}
	for _, text := range []string{"", "同意", "发吧", "please Agree", "Agree now", "同 意", "YEſ", "yeſ"} {
		if ownerApprovalPhrase(settings, text) {
			t.Errorf("inexact phrase %q matched", text)
		}
	}
	off := false
	if ownerApprovalPhrase(Settings{OwnerReplyApproval: &off}, "发") || ownerApprovalPhrase(Settings{}, "发") {
		t.Fatal("default-off setting matched an approval phrase")
	}
}
