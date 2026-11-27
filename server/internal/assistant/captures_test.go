package assistant

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// The sorting prompt (#13502), byte for byte; the note goes as data in the user message, cut at 2,000 characters; an
// answer keeps only known categories, a known track and action, a confidence between 0 and 1.
func TestCaptureSortPrompt(t *testing.T) {
	ctx := context.Background()
	m := &infoTagModel{answer: `{"space":"qa-plain","confidence":1.7,"action":"later","title":"  A placeholder\ntitle ","deep":true,"categories":["Finance","someday"]}`}
	note := captureNote(db.Capture{Text: strings.Repeat("x", captureNoteMax+10), URL: "https://example.com/qa"})
	if len([]rune(note)) != captureNoteMax {
		t.Fatalf("note of %d characters", len([]rune(note)))
	}
	in := captureSortInput{Note: "qa: <a placeholder> & note", Source: assistantwire.CaptureSourceWatch,
		Spaces: []captureSpace{{Slug: "qa-plain", Title: "QA plain"}}}
	a, err := sortCaptureWith(ctx, m, in)
	if err != nil || !slices.Equal(a.Categories, []string{categoryFinance}) {
		t.Fatalf("answer %+v, %v", a, err)
	}
	golden(t, "capture_sort", m.system[0])
	if m.system[0] != prompts.RenderCaptureSort() {
		t.Error("the system prompt is not the template")
	}
	if want := `{"note":"qa: <a placeholder> & note","source":"watch","spaces":[{"slug":"qa-plain","title":"QA plain"}]}`; m.user[0] != want {
		t.Errorf("user message = %s, want %s", m.user[0], want)
	}
	targets := []trackEntry{{Slug: "qa-plain"}, {Slug: "qa-finance"}}
	got := captureSortOf(a, targets, "")
	if got.Track != "qa-plain" || got.Confidence != 1 || got.Action != "keep" || got.Title != "A placeholder title" || !got.Deep || !got.AutoFile {
		t.Errorf("sorted = %+v", got)
	}
	unsure := "qa-plain"
	if got := captureSortOf(captureSortAnswer{Space: &unsure, Confidence: 0.79, Action: "research"}, targets, ""); got.AutoFile || got.Track != "qa-plain" || got.Action != "research" {
		t.Errorf("an unsure answer = %+v: sorted, left for the user", got)
	}
	other := "qa-elsewhere"
	if got := captureSortOf(captureSortAnswer{Space: &other, Confidence: 0.99}, targets, ""); got.Track != "" || got.AutoFile {
		t.Errorf("a track that takes no captures = %+v", got)
	}
	if got := captureSortOf(captureSortAnswer{Confidence: 0.1}, targets, "qa-finance"); got.Track != "qa-finance" || !got.AutoFile {
		t.Errorf("the user's choice = %+v: filed there", got)
	}
}

// The briefing's quick-capture section, as the model is given it (#13502).
func TestGoldenBriefFactsCaptures(t *testing.T) {
	lines := []string{"2 条待归类", "示例标题 → qa-plain", "1 条随手记"}
	golden(t, "routine_facts.captures", prompts.RenderBriefFacts(prompts.BriefFacts{Hours: 12, Captures: lines}))
	golden(t, "routine_facts.captures_card", prompts.RenderBriefFacts(prompts.BriefFacts{Card: true, Hours: 12, Captures: lines}))
}
