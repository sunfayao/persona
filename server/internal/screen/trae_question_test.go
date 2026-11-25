package screen

import (
	"strings"
	"testing"
)

const traeLastPage = `─────────────────────────────────────────────────
  Question 2/2 (1 unanswered)
  What color should the synthetic demo
  use for its preview?

    1. Blue   Use a cool accent on the header
              and the focus outline.
    2. Amber  Use a warm accent.
  ❯ 3. Type your answer

  type to answer | tab or esc clear answer | enter submit all
`

func TestReadTraeQuestionKeepsNativePageAndOptions(t *testing.T) {
	q := ReadTraeQuestion(traeLastPage)
	if q == nil || !q.SubmitsAll || q.Index != 2 || q.Total != 2 || q.Left != 1 || q.Dialog.Cursor != "3" ||
		q.Dialog.Question != "What color should the synthetic demo use for its preview?" {
		t.Fatalf("page = %+v", q)
	}
	if len(q.Dialog.Options) != 3 || q.Dialog.Options[0].Detail != "Use a cool accent on the header and the focus outline." ||
		q.Dialog.Options[2].Kind != KindTypeSomething || q.FreeText != "" {
		t.Fatalf("choices = %+v", q)
	}
	typed := ReadTraeQuestion(strings.Replace(traeLastPage, "Type your answer", "A teal  accent", 1))
	if typed == nil || typed.FreeText != "A teal  accent" || typed.Dialog.Rev != q.Dialog.Rev ||
		strings.Contains(typed.Dialog.Options[2].Label, "teal") {
		t.Fatalf("the answer changed the question or leaked into its shape: %+v", typed)
	}
	pickFirst := strings.NewReplacer("  ❯ 3.", "    3.", "    1.", "  › 1.").Replace(traeLastPage)
	if live := ReadTraeQuestion(pickFirst); live.Dialog.Cursor != "1" || live.Dialog.Rev != q.Dialog.Rev {
		t.Fatalf("cursor move changed the question: %+v", live)
	}
}

func TestReadTraeQuestionOffersTerminalWhenSubmissionIsAmbiguous(t *testing.T) {
	for _, text := range []string{
		strings.Replace(traeLastPage, "(1 unanswered)", "(2 unanswered)", 1),
		strings.Replace(traeLastPage, "enter submit all", "", 1),
		strings.Replace(traeLastPage, "    2. Amber", "    5. Amber", 1),
		strings.Replace(traeLastPage, "clear answer", "clear notes", 1),
	} {
		if q := ReadTraeQuestion(text); q == nil || len(q.Dialog.Options) != 0 {
			t.Fatalf("unsafe page offered choices: %+v", q)
		}
	}
	if ReadTraeQuestion("The docs say:\nQuestion 2/2 (1 unanswered)\nThat is the whole example.") != nil {
		t.Fatal("the transcript was mistaken for a question")
	}
	next := strings.NewReplacer("2/2 (1 unanswered)", "1/2 (2 unanswered)", "submit all", "submit answer").Replace(traeLastPage)
	if q := ReadTraeQuestion(next); q == nil || q.SubmitsAll || len(q.Dialog.Options) != 3 {
		t.Fatalf("first page can't be answered: %+v", q)
	}
}
