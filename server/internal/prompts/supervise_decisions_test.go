package prompts

import (
	"strings"
	"testing"
)

// A decision signal's rule is in the message that carries the signal, once however many of its entries there are, and
// in no static instructions: the rules of signals that did not fire are in no request.
func TestSuperviseDecisionInstructionsAppearOnce(t *testing.T) {
	policy := RenderSupervisionInstructions()
	for _, instruction := range []string{"- `recheck`:", "- `undecidable`:", "- `owner_cannot_answer`:", "`todo_decision_add`"} {
		if strings.Contains(policy, instruction) {
			t.Errorf("static policy still carries %q", instruction)
		}
	}
	for kind, instruction := range map[string]string{"recheck": "- `recheck`:", "undecidable": "- `undecidable` / `delivery_hold`:",
		"owner_cannot_answer": "Never take an irreversible/externally visible step on the owner's behalf"} {
		entry := SuperviseEntry{ID: "todo_example", Title: "Example task", Kind: kind}
		text := RenderSupervise(Supervise{Decisions: true, Entries: []SuperviseEntry{entry, entry}})
		if got := strings.Count(text, instruction); got != 1 {
			t.Errorf("%s: the message carries %q %d times, want 1:\n%s", kind, instruction, got, text)
		}
	}
	for _, role := range []struct {
		name string
		data Supervise
	}{
		{"main", Supervise{}},
		{"round", Supervise{Fresh: true}},
	} {
		t.Run(role.name, func(t *testing.T) {
			for _, decisions := range []bool{false, true} {
				data := role.data
				data.Decisions = decisions
				data.Entries = []SuperviseEntry{{ID: "todo_example", Title: "Example task", Kind: "recheck"}}
				text := RenderSupervise(data)
				// i18n-ignore: old handling instructions must not return in event payloads.
				for _, instruction := range []string{
					"- 决定交回你核对的：", "- 用户那边没有能回答的东西的：",
					"- 用户回「你来定」的：", "- 用户回「这不是我要的」的：", "- 用户回「先讲清背景」的：",
				} {
					if got := strings.Count(text, instruction); got != 0 {
						t.Errorf("Decisions=%v: event repeats %q %d times", decisions, instruction, got)
					}
				}
				if got := strings.Count(text, "todo_decision_add"); got != 0 {
					t.Errorf("Decisions=%v: a recheck repeats the decision recording of owner_cannot_answer %d times", decisions, got)
				}
				// i18n-ignore: approval handling also belongs only in the static fragment.
				approval := "标着「不会默认执行」的（不可撤回，或别人看得到）你不能替用户走这一步"
				if got := strings.Count(text, approval); got != 0 {
					t.Errorf("Decisions=%v: event repeats approval handling %d times", decisions, got)
				}
			}
		})
	}
}

func TestSuperviseReviewDoesNotAssertCompletion(t *testing.T) {
	const source = "https://github.com/example/app/pull/12"
	criteria := []string{
		"Revalidate and merge the original delivery PR after the prerequisite repair",
		"Deploy and verify the delivery in production",
	}
	for _, role := range []struct {
		name string
		data Supervise
	}{
		{"main", Supervise{}},
		{"round", Supervise{Fresh: true}},
	} {
		t.Run(role.name, func(t *testing.T) {
			for _, event := range []struct {
				name, detail string
				evidence     []string
			}{
				{"prerequisite merged", "PR merged: " + source, nil},
				{"PR closed", "PR closed: " + source, nil},
				{"changes requested", "PR changes requested: " + source, []string{"criterion 1 [blocked]: changes requested"}},
				{"judged done candidate", "The judge found the reported work finished", []string{
					"criterion 1 [satisfied]: original PR revalidated and merged",
					"criterion 2 [satisfied]: deployed version and live reading recorded",
				}},
				{"recovered review without a reason", "", nil},
			} {
				t.Run(event.name, func(t *testing.T) {
					data := role.data
					data.Entries = []SuperviseEntry{{
						ID: "todo_example", Title: "Deliver the original change", Kind: "review",
						Detail: event.detail, DoneWhen: criteria, Evidence: event.evidence,
					}}
					text := RenderSupervise(data)
					// i18n-ignore: review is a notification, not an acceptance verdict.
					wants := []string{"signal=review", "awaiting item-by-item review; this does not mean it is done", event.detail}
					wants = append(wants, criteria...)
					wants = append(wants, event.evidence...)
					for _, want := range wants {
						if !strings.Contains(text, want) {
							t.Errorf("review message lost %q:\n%s", want, text)
						}
					}
					// i18n-ignore: the old blanket completion claim must not return.
					if strings.Contains(text, "hub 判断做完了") {
						t.Errorf("a review wake still claims completion:\n%s", text)
					}
				})
			}
		})
	}
}
