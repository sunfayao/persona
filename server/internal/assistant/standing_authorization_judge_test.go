package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

type standingJudgeTestModel struct {
	reply        string
	err          error
	calls        int
	system, user string
}

func (m *standingJudgeTestModel) JSON(_ context.Context, system, user string, out any) error {
	m.calls++
	m.system, m.user = system, user
	if m.err != nil {
		return m.err
	}
	return json.Unmarshal([]byte(m.reply), out)
}

func standingJudgeReply(t *testing.T, actions []string, answers map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"within_task": true, "complete": true, "proceed": true, "actions": actions, "answers": answers,
		"reason": "The confirmation only requests the listed task actions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func naturalStandingQuestion() proto.Interaction {
	return proto.Interaction{Kind: proto.IxQuestion, ToolName: "AskUserQuestion",
		Questions: []proto.Question{{
			Question: "PR #123 CI 已绿，要合入 main 吗？", Header: "合入确认", // i18n-ignore: synthetic card
			Options: []proto.QuestionOption{
				{Label: "直接合入 main（推荐）", Description: "按已有授权合入当前 PR，并核对结果。"}, // i18n-ignore: synthetic card
				{Label: "暂不合入", Description: "保留当前分支，稍后处理。"},                 // i18n-ignore: synthetic card
			},
		}}}
}

func TestStandingAuthorizationJudgeReadsNaturalCard(t *testing.T) {
	ix := naturalStandingQuestion()
	ix.Summary = "Current pull request is ready for its merge confirmation."
	m := &standingJudgeTestModel{reply: standingJudgeReply(t, []string{"merge"}, map[string]string{
		ix.Questions[0].Question: ix.Questions[0].Options[0].Label,
	})}
	a, err := assessStandingWith(t.Context(), m, ix, "Merge the current pull request after its checks pass.", "/code/app")
	if err != nil || !a.Complete || !a.Proceed || len(a.Actions) != 1 || a.Actions[0] != "merge" {
		t.Fatalf("natural-language assessment: %+v %v", a, err)
	}
	if a.Answers[ix.Questions[0].Question] != ix.Questions[0].Options[0].Label {
		t.Fatalf("the offered affirmative option was lost: %+v", a.Answers)
	}
	if m.calls != 1 {
		t.Fatalf("model calls = %d, want 1", m.calls)
	}
	if !strings.Contains(m.system, "A standing grant never expands the assigned task") {
		t.Fatal("judge prompt omitted the task scope boundary")
	}
	for _, text := range []string{ix.Questions[0].Question, ix.Questions[0].Header,
		ix.Questions[0].Options[0].Label, ix.Questions[0].Options[0].Description,
		ix.Questions[0].Options[1].Label, ix.Questions[0].Options[1].Description,
		ix.Summary, "Merge the current pull request after its checks pass.", "/code/app"} {
		if !strings.Contains(m.user, text) {
			t.Errorf("judge input omitted %q", text)
		}
	}
}

func TestStandingAuthorizationJudgeMultiStepCoverage(t *testing.T) {
	for _, test := range []struct {
		name, plan string
		actions    []string
		granted    []string
		allowed    bool
	}{
		{"merge_then_local_verification", "合并后跑本地验证。", []string{"merge", "verify"}, []string{"merge"}, true}, // i18n-ignore: synthetic plan
		{"all_external_steps_granted", "Push the branch, merge its pull request, deploy to staging, and run local tests.", []string{"push", "merge", "deploy-test", "verify"}, []string{"merge", "push", "deploy-test"}, true},
		{"uncovered_push", "Push the branch and merge its pull request.", []string{"push", "merge"}, []string{"merge"}, false},
		{"uncovered_test_deployment", "Merge its pull request and deploy to staging.", []string{"merge", "deploy-test"}, []string{"merge"}, false},
		{"unknown_step", "Merge its pull request and perform an unspecified privileged action.", []string{"merge", "unknown"}, []string{"merge"}, false},
		{"local_only_is_not_a_merge_confirmation", "Read the source, edit a file, and run local tests.", []string{"read", "edit", "verify"}, []string{"merge"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ix := proto.Interaction{Kind: proto.IxPlan, ToolName: "ExitPlanMode", Plan: test.plan}
			m := &standingJudgeTestModel{reply: standingJudgeReply(t, test.actions, nil)}
			a, err := assessStandingWith(t.Context(), m, ix, "Finish the task within its existing authorization.")
			if err != nil {
				if test.allowed {
					t.Fatal(err)
				}
				return
			}
			grant := routeStandingGrant()
			grant.Actions = test.granted
			if got := standingAssessmentAllowed(grant, a, ix); got != test.allowed {
				t.Fatalf("all-step coverage = %v, want %v; assessment=%+v", got, test.allowed, a)
			}
		})
	}
}

func TestStandingAuthorizationJudgeConservativeFailures(t *testing.T) {
	for _, test := range []struct {
		name, reply string
		err         error
	}{
		{"incomplete", `{"within_task":true,"complete":false,"proceed":true,"actions":["merge"],"reason":"The rest of the plan is unclear."}`, nil},
		{"not_a_confirmation", `{"within_task":true,"complete":true,"proceed":false,"actions":["merge"],"reason":"This is a status report, not a request to execute."}`, nil},
		{"no_actions", `{"within_task":true,"complete":true,"proceed":true,"actions":[],"reason":"No operation identified."}`, nil},
		{"outside_task", `{"within_task":false,"complete":true,"proceed":true,"actions":["push"],"reason":"The plan pushes a different repository."}`, nil},
		{"invalid_json", `{"within_task":true,"complete":`, nil},
		{"model_failure", "", errors.New("judge unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ix := proto.Interaction{Kind: proto.IxPlan, Plan: "Merge the pull request and verify it locally."}
			m := &standingJudgeTestModel{reply: test.reply, err: test.err}
			a, err := assessStandingWith(t.Context(), m, ix, "Finish the existing task.")
			if err == nil && standingAssessmentAllowed(routeStandingGrant(), a, ix) {
				t.Fatalf("unclear or failed assessment was approved: %+v", a)
			}
		})
	}
}

func TestStandingAuthorizationJudgeQuestionAnswers(t *testing.T) {
	for _, test := range []string{"missing_answer", "invented_option", "negative_option", "english_no", "long_negative_option", "runtime_option_action", "missing_second_question"} {
		t.Run(test, func(t *testing.T) {
			ix := naturalStandingQuestion()
			answers := map[string]string{ix.Questions[0].Question: ix.Questions[0].Options[0].Label}
			switch test {
			case "missing_answer":
				answers = nil
			case "invented_option":
				answers[ix.Questions[0].Question] = "Execute an option the card did not offer."
			case "negative_option":
				answers[ix.Questions[0].Question] = ix.Questions[0].Options[1].Label
			case "english_no":
				ix.Questions[0].Options[1].Label = "No"
				answers[ix.Questions[0].Question] = ix.Questions[0].Options[1].Label
			case "long_negative_option":
				ix.Questions[0].Options[1].Label = "Do not merge this pull request yet."
				answers[ix.Questions[0].Question] = ix.Questions[0].Options[1].Label
			case "runtime_option_action":
				ix.Questions[0].Options[0].Action = "bypassPermissions"
			case "missing_second_question":
				ix.Questions = append(ix.Questions, proto.Question{Question: "Should I push the branch?", Options: []proto.QuestionOption{{Label: "Continue"}, {Label: "Not now"}}})
			}
			m := &standingJudgeTestModel{reply: standingJudgeReply(t, []string{"merge"}, answers)}
			a, err := assessStandingWith(t.Context(), m, ix, "Merge the current pull request after its checks pass.")
			if err == nil && standingAssessmentAllowed(routeStandingGrant(), a, ix) {
				t.Fatalf("invalid affirmative answer was accepted: %+v", a)
			}
		})
	}
}

func TestStandingAuthorizationLocalTaskProhibitions(t *testing.T) {
	for _, test := range []struct {
		action, instruction string
	}{
		{"read", "Do not read files outside the task."},
		{"edit", "Do not edit the generated files."},
		{"verify", "Do not run verification tests."},
	} {
		t.Run(test.action, func(t *testing.T) {
			if !standingTaskForbids(test.instruction, test.action) {
				t.Fatalf("task prohibition was ignored: %s", test.instruction)
			}
		})
	}
}

func TestStandingAuthorizationJudgeCannotOmitSensitiveEffects(t *testing.T) {
	for _, test := range []struct {
		name, plan, risk string
	}{
		{"production", "Merge the pull request, then deploy to production.", "deploy-production"},
		{"unqualified_deployment", "Merge the pull request, then deploy the service.", "deploy-production"},
		{"external_message", "Merge the pull request, then send a message to its maintainer.", "send"},
		{"deletion", "Merge the pull request, then delete the release branch.", "delete"},
		{"spending", "Merge the pull request, then buy extra build capacity.", "spend"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ix := proto.Interaction{Kind: proto.IxPlan, Plan: test.plan}
			// A model's omission cannot turn an external effect into an authorized local step.
			a := standingCardAssessment{WithinTask: true, Complete: true, Proceed: true, Actions: []string{"merge"}, Reason: "Merge only."}
			grant := routeStandingGrant()
			if standingAssessmentAllowed(grant, a, ix) {
				t.Fatal("the model omitted an ungranted sensitive effect")
			}
			grant.Actions = append(grant.Actions, test.risk)
			grant.Reversible = false
			if standingAssessmentAllowed(grant, a, ix) {
				t.Fatal("an irreversible sensitive grant was approved")
			}
		})
	}
	for _, field := range []string{"header", "option_label", "option_description", "summary"} {
		t.Run(field, func(t *testing.T) {
			ix := naturalStandingQuestion()
			const hidden = "Merge and send a message to the maintainer."
			switch field {
			case "header":
				ix.Questions[0].Header = hidden
			case "option_label":
				ix.Questions[0].Options[0].Label = hidden
			case "option_description":
				ix.Questions[0].Options[0].Description = hidden
			case "summary":
				ix.Summary = hidden
			}
			a := standingCardAssessment{WithinTask: true, Complete: true, Proceed: true, Actions: []string{"merge"}, Reason: "Merge only."}
			if standingAssessmentAllowed(routeStandingGrant(), a, ix) {
				t.Fatal("hidden external effect escaped the code boundary")
			}
		})
	}
	ix := proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", Summary: "Merge the pull request."}
	if standingAssessmentAllowed(routeStandingGrant(), standingCardAssessment{WithinTask: true, Complete: true, Proceed: true, Actions: []string{"merge"}}, ix) {
		t.Fatal("a model result approved a tool permission")
	}
}
