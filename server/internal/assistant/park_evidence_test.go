package assistant

import (
	"context"
	"testing"
	"time"
)

// Use the public tool's JSON contract so these tests still compile against the pre-change production code.
func parkEvidenceArgs(kind string) map[string]any {
	e := map[string]any{"kind": kind, "next_actor": "external", "target": "reviewer-a", "wake": "The named reviewer replies in the linked thread"}
	a := map[string]any{"verdict": "parked", "reason": "The requested review is outside the Agent's control",
		"waiting_on": "someone_else", "until": "3d", "evidence": e}
	switch kind {
	case "person_reply", "merge_review":
		e["contact"] = map[string]any{"channel": "project discussion", "url": "https://example.org/messages/request-1",
			"sender": "requester-a", "sent_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
			"request": "Please review the ready change and reply in this thread", "status": "sent"}
		if kind == "merge_review" {
			e["ready"], e["url"] = true, "https://github.com/example/project/pull/42"
		}
	case "external_event":
		a["waiting_on"] = "external_event"
		e["target"], e["url"], e["wake"] = "upstream check run 42", "https://example.org/checks/42", "The bound check run completes"
	case "date":
		a["waiting_on"], a["until"] = "date", time.Now().UTC().Add(72*time.Hour).Format(time.RFC3339Nano)
		e["target"], e["wake"] = "the announced application window", "The recorded opening date arrives"
	}
	return a
}

func TestParkEvidenceAcceptsReadyGitLabReviewReceipt(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	a := parkEvidenceArgs("merge_review")
	a["evidence"].(map[string]any)["url"] = "https://git.example.org/group/project/-/merge_requests/42"
	if _, err := reviewErr(s, todo.ID, a); err != nil {
		t.Fatal(err)
	}
}

func TestParkEvidenceMechanicalWaitDoesNotInventReceipt(t *testing.T) {
	s, _, todo, c, now := mechanicalFixture(t)
	handled, retry := s.mechanicalSignal(context.Background(),
		superviseSignal{Todo: todo.ID, Kind: sigIdle, Machine: c.MachineID, Session: c.SessionID, Key: "turn-1"}, now)
	v := todoNow(s, todo.ID).Supervision
	if handled || !retry.IsZero() || v.State != "" || len(v.Reviews) != 0 {
		t.Fatalf("a PR alone silently parked the task without a contact receipt: handled=%v retry=%v ledger=%+v", handled, retry, v)
	}
}
