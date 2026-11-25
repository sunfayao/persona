package assistant

import (
	"testing"
	"time"
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
