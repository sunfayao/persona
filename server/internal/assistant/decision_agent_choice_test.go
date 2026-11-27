package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
)

func TestExactDecisionOption(t *testing.T) {
	options := []DecisionOption{{Label: "Small rollout"}, {Label: "Full rollout"}}
	for _, tc := range []struct {
		name, text, want string
	}{
		{"first letter", "A", "Small rollout"},
		{"second letter", "B", "Full rollout"},
		{"lowercase letter", "b", "Full rollout"},
		{"letter punctuation", "\u300c B!\u300d", "Full rollout"},
		{"full label", "Full rollout", "Full rollout"},
		{"label spacing", " Full \trollout! ", "Full rollout"},
		{"label punctuation", "Full-rollout", "Full rollout"},
		{"incomplete label", "Full", ""},
		{"label case is exact", "full rollout", ""},
		{"prose", "I choose B", ""},
		{"two letters", "A/B", ""},
		{"negation", "Not B, A", ""},
		{"out of range", "C", ""},
		{"number", "2", ""},
		{"empty", " \t\n", ""},
		{"punctuation only", "!?...", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exactDecisionOption(tc.text, options); got != tc.want {
				t.Fatalf("exactDecisionOption(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
	t.Run("four options preserve displayed order", func(t *testing.T) {
		four := append(options, DecisionOption{Label: "Wait"}, DecisionOption{Label: "Cancel"})
		if got := exactDecisionOption("D.", four); got != "Cancel" {
			t.Fatalf("fourth letter chose %q", got)
		}
	})
	for _, tc := range []struct {
		name, text string
		options    []DecisionOption
	}{
		{"duplicate normalized labels", "Ship now", []DecisionOption{{Label: "Ship now"}, {Label: "Ship-now"}}},
		{"letter conflicts with label", "A", []DecisionOption{{Label: "B"}, {Label: "A"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exactDecisionOption(tc.text, tc.options); got != "" {
				t.Fatalf("ambiguous answer %q chose %q", tc.text, got)
			}
		})
	}
}

func agentChoiceMessage(t *testing.T, s *Service, r SessionRef, text string) {
	t.Helper()
	raw, err := json.Marshal(assistantwire.UserSpokeParams{SessionRef: r, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	b := &BusHub{svc: s}
	if err := b.event(context.Background(), busEvent{name: assistantwire.EventUserSpoke, params: raw}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentDecisionExactLabelSettlesAfterEarlierUnclearReply(t *testing.T) {
	for _, first := range []string{"", "I have a question about the options."} {
		t.Run(first, func(t *testing.T) {
			s, _, todo := decisionAgentReplyService(t)
			a := askOf(s, todo.ID)
			ref := SessionRef{MachineID: a.MachineID, SessionID: a.SessionID}
			agentChoiceMessage(t, s, ref, first)
			if got := askOf(s, todo.ID); got.AgentReply == nil || !got.AcceptedAt.IsZero() {
				t.Fatalf("the unclear reply was not left paused: %+v", got)
			}
			label := a.Options[1].Label
			agentChoiceMessage(t, s, ref, "\u300c "+label+"!\u300d")
			if got := askOf(s, todo.ID); got.AcceptedAt.IsZero() || got.Chosen != label || got.ByDefault {
				t.Fatalf("a later exact label did not settle the decision: %+v", got)
			}
			if todoNow(s, todo.ID).Supervision.State == reviewNeedsUser {
				t.Fatal("the later exact label still waits for a supervisor")
			}
		})
	}
}

func TestAgentDecisionUnclearTextDoesNotChoose(t *testing.T) {
	for _, text := range []string{"I choose B", "A/B", "Not A", "Please explain the full rollout."} {
		t.Run(text, func(t *testing.T) {
			s, _, todo := decisionAgentReplyService(t)
			a := askOf(s, todo.ID)
			agentChoiceMessage(t, s, SessionRef{MachineID: a.MachineID, SessionID: a.SessionID}, text)
			if got := askOf(s, todo.ID); got.AgentReply == nil || got.DueAt.IsZero() || !got.AcceptedAt.IsZero() ||
				got.Chosen != "" || got.ByDefault || todoNow(s, todo.ID).Supervision.State != reviewNeedsUser {
				t.Fatalf("unclear text was guessed or left the default enabled: %+v", got)
			}
		})
	}
}

type failingAgentDecisionStore struct{ *db.DB }

func (s failingAgentDecisionStore) InsertTodoDecision(context.Context, db.TodoDecisionRow) (bool, error) {
	return false, errors.New("decision write unavailable")
}

func TestAgentDecisionWriteFailureKeepsTheAskPaused(t *testing.T) {
	database := todoTestDB(t)
	s, f, todo, _, _ := conclusionServiceOn(t, failingAgentDecisionStore{database}, false)
	waitsForReview(t, s, f, todo.ID, "The owner needs to choose a rollout.")
	reviewTask(t, s, todo.ID, rolloutDecision())
	a := askOf(s, todo.ID)
	agentChoiceMessage(t, s, SessionRef{MachineID: a.MachineID, SessionID: a.SessionID}, "B")
	if got := askOf(s, todo.ID); got.AgentReply == nil || got.DueAt.IsZero() || !got.AcceptedAt.IsZero() || got.ByDefault ||
		todoNow(s, todo.ID).Supervision.State != reviewNeedsUser {
		t.Fatalf("a failed decision write ended the ask: %+v", got)
	}
	if rows, err := database.ListTodoDecisions(context.Background(), todo.ID, true); err != nil || len(rows) != 0 {
		t.Fatalf("unexpected stored decision: %+v %v", rows, err)
	}
	waitFor(t, "the recoverable supervisor handoff", func() bool {
		return strings.Contains(strings.Join(f.notices(), "\n"), "task_decided")
	})
}
