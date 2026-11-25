package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

func routeStandingGrant() StandingAuthorization {
	return StandingAuthorization{ID: "sa_route", MachineID: "mac", Directory: "/code/app", Actions: []string{"merge"},
		Conditions: []string{}, Quote: "Merge changes in this repository without asking again.", Source: "Main conversation", GrantedAt: time.Now().UTC(), Reversible: true}
}

func TestStandingCardPathsPunctuation(t *testing.T) {
	for _, test := range []struct {
		name, separator string
		quotedAllowed   bool
	}{
		{"semicolon", ";", false}, {"comma", ",", true}, {"exclamation", "!", true}, {"question", "?", false},
		{"ideographic_period", "。", true}, {"fullwidth_semicolon", "；", true}, {"fullwidth_comma", "，", true},
		{"fullwidth_exclamation", "！", true}, {"fullwidth_question", "？", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			const first, second = "/example/first.txt", "/example/second"
			got, ok := standingCardPaths(proto.Interaction{Plan: first + test.separator + second})
			if want := []string{first, second}; !ok || !slices.Equal(got, want) {
				t.Fatalf("unquoted paths = %q, ok = %v; want %q", got, ok, want)
			}
			for _, quote := range []string{"\"", "'", "`"} {
				path := first + test.separator + "part"
				got, ok := standingCardPaths(proto.Interaction{Plan: quote + path + quote})
				if test.quotedAllowed {
					if want := []string{path}; !ok || !slices.Equal(got, want) {
						t.Errorf("quoted with %q: paths = %q, ok = %v; want %q", quote, got, ok, want)
					}
				} else if ok || len(got) != 0 {
					t.Errorf("quoted with %q: paths = %q, ok = %v; want rejection", quote, got, ok)
				}
			}
		})
	}
}

type failingStandingStore struct{ *db.DB }

func TestStandingAuthorizationConditions(t *testing.T) {
	for _, test := range []string{"pass", "pending", "empty", "incomplete", "conflict", "unknown", "wrong_pr", "unavailable", "draft", "closed"} {
		t.Run(test, func(t *testing.T) {
			s, f := newQuietService(t)
			f.answerNode("git.pr", func(_ string, _ json.RawMessage) (any, error) {
				if test == "unavailable" {
					return nil, errors.New("offline")
				}
				pr := map[string]any{"url": "https://github.com/example/app/pull/12", "head_oid": "abc123", "state": "OPEN", "mergeable": "MERGEABLE", "checks_complete": true, "checks": []map[string]string{{"state": "pass"}}}
				switch test {
				case "pending":
					pr["checks"] = []map[string]string{{"state": "pending"}}
				case "empty":
					pr["checks"] = []any{}
				case "incomplete":
					pr["checks_complete"] = false
				case "conflict":
					pr["mergeable"] = "CONFLICTING"
				case "draft":
					pr["draft"] = true
				case "closed":
					pr["state"] = "MERGED"
				}
				return map[string]any{"pr": pr}, nil
			})
			conditions := []string{"ci_green", "no_conflicts"}
			if test == "unknown" {
				conditions = append(conditions, "trust me")
			}
			target := "https://github.com/example/app/pull/12"
			if test == "wrong_pr" {
				target = "https://github.com/example/another/pull/12"
			}
			got := s.standingConditions(t.Context(), "box", "/code/app", "merge", target, conditions)
			if got != (test == "pass") {
				t.Fatalf("verified=%v", got)
			}
		})
	}
}

func TestStandingAuthorizationRejectsHiddenInstructions(t *testing.T) {
	for _, part := range []string{"option_label", "option_description", "header", "summary", "tool_input", "multiple", "secret", "form"} {
		t.Run(part, func(t *testing.T) {
			ix := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "Merge?", Options: []proto.QuestionOption{{Label: "Continue"}, {Label: "Not now"}}}}}
			switch part {
			case "option_label":
				ix.Questions[0].Options[0].Label = "Merge and deploy to production"
			case "option_description":
				ix.Questions[0].Options[0].Description = "Delete the branch"
			case "header":
				ix.Questions[0].Header = "Send a message after merging"
			case "summary":
				ix.Summary = "Merge and spend money"
			case "tool_input":
				ix.ToolInput = json.RawMessage(`{"questions":[{"question":"Merge?"}],"command":"danger"}`)
			case "multiple":
				ix.Questions = append(ix.Questions, proto.Question{Question: "Spend money?"})
			case "secret":
				ix.Secret = true
			case "form":
				ix.Fields = []proto.Field{{Key: "confirmation"}}
			}
			if _, _, _, ok := standingInteractionAction(ix); ok {
				t.Fatal("hidden instructions passed confirmation parser")
			}
		})
	}
}

func TestStandingAuthorizationTaskProhibitions(t *testing.T) {
	for _, test := range []struct {
		text, action string
		forbids      bool
	}{
		{"不发布到线上", "deploy-production", true}, // i18n-ignore: task fixture
		{"No production deployment.", "deploy-production", true},
		{"Do not merge; stop at a PR.", "merge", true},
		{"Merge once there are no merge conflicts.", "merge", false},
		{"Merge without asking again.", "merge", false},
		{"Do not deploy. Merge after checks pass.", "merge", false},
		{"Never delete files.", "delete", true},
		{"Do not spend money.", "spend", true},
		{"Do not send messages.", "send", true},
		{"Read-only investigation.", "push", true},
	} {
		if got := standingTaskForbids(test.text, test.action); got != test.forbids {
			t.Errorf("%q %s: forbids=%v", test.text, test.action, got)
		}
	}
}
