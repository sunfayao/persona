package screen

import (
	"os"
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

func TestReadTraeQuestionReadsTheOnlyQuestionWithItsOtherRow(t *testing.T) {
	b, err := os.ReadFile("testdata/traecli-question-single-127.txt")
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	q := ReadTraeQuestion(page)
	if q == nil || !q.SubmitsAll || q.Index != 1 || q.Total != 1 || q.Left != 1 || q.Dialog.Cursor != "1" || q.FreeText != "" ||
		q.Dialog.Header != "Question 1/1 (1 unanswered)" ||
		q.Dialog.Question != "是否仅更新新建 DemoWidget Workspace 的 API 配置，并写入原始草稿文件（Alpha/Beta 不变、不发布）？" {
		t.Fatalf("page = %+v", q)
	}
	o := q.Dialog.Options
	if len(o) != 3 || o[0].Label != "更新新项目配置（推荐）" || !strings.HasSuffix(o[0].Detail, "将新值与Item ID、demo 站点一起写入原始草稿文件并回读验证。") ||
		o[1].Label != "保留配置，暂缓草稿" || o[2].Kind != KindTypeSomething || o[2].Label != "Type your answer" || o[2].Detail != "" {
		t.Fatalf("choices = %+v", o)
	}
	onOther := strings.NewReplacer("  ❯ 1.", "    1.", "    3. Other", "  ❯ 3. Other").Replace(page)
	typed := ReadTraeQuestion(strings.Replace(onOther, "Type your answer.", "A synthetic  answer", 1))
	if typed == nil || typed.FreeText != "A synthetic  answer" || typed.Dialog.Rev != q.Dialog.Rev || typed.Dialog.Cursor != "3" {
		t.Fatalf("the typed answer was not read back on the same question: %+v", typed)
	}
	if left := ReadTraeQuestion(strings.Replace(page, "Type your answer.", "A synthetic answer", 1)); left == nil || len(left.Dialog.Options) != 0 {
		t.Fatalf("a draft the cursor left offered choices: %+v", left)
	}
	if two := ReadTraeQuestion(strings.Replace(page, "1/1 (1 unanswered)", "2/2 (1 unanswered)", 1)); two == nil || two.SubmitsAll || len(two.Dialog.Options) != 0 {
		t.Fatalf("the last of several pages saying submit answer was offered: %+v", two)
	}
}

func TestReadTraeQuestionOffersTerminalWhenSubmissionIsAmbiguous(t *testing.T) {
	for _, text := range []string{
		strings.Replace(traeLastPage, "(1 unanswered)", "(2 unanswered)", 1),
		strings.Replace(traeLastPage, "enter submit all", "", 1),
		strings.Replace(traeLastPage, "    2. Amber", "    5. Amber", 1),
		strings.Replace(traeLastPage, "clear answer", "clear notes", 1),
		strings.Replace(traeLastPage, "    1. Blue", "    [ ] 1. Blue", 1),
	} {
		if q := ReadTraeQuestion(text); q == nil || len(q.Dialog.Options) != 0 || strings.Contains(q.Dialog.Question, "Blue") {
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
