package assistant

import (
	"encoding/json"
	"fmt"
	"github.com/nwverse/persona/server/internal/assistant/saved"
	"strings"
	"testing"
)

const savedPromptsPath = "/api/v1/assistant/saved-prompts"

func listedSavedPromptsOf(t *testing.T, s *Service) []saved.Prompt {
	t.Helper()
	rec := placeDo(s, "", "GET", savedPromptsPath, "")
	var out struct{ Prompts []saved.Prompt }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	return out.Prompts
}

func addedSavedPrompt(t *testing.T, s *Service, body string) saved.Prompt {
	t.Helper()
	rec := placeDo(s, "", "POST", savedPromptsPath, body)
	var out struct{ Prompt saved.Prompt }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("add %s: %d %s", body, rec.Code, rec.Body)
	}
	return out.Prompt
}

// The user adds and changes saved prompts from the app: one-word names unique ignoring case, a "/" typed before
// the name dropped, the text's line ends made "\n", and each change told to the apps.
func TestSavedPromptRoutes(t *testing.T) {
	hub := newFakeHub()
	s := newTestService(t, hub)
	if rec := placeDo(s, "", "GET", savedPromptsPath, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"prompts":[]`) {
		t.Fatalf("none yet is an empty list: %d %s", rec.Code, rec.Body)
	}
	review := addedSavedPrompt(t, s, `{"name":" /review ","text":"  Review this diff.\r\nList each finding with its file and line.\n\n"}`)
	if review.ID == "" || review.Name != "review" || review.Text != "Review this diff.\nList each finding with its file and line." || review.CreatedAt.IsZero() {
		t.Fatalf("trimmed, without the slash, with an id: %+v", review)
	}
	if got := hub.eventTypes(); !strings.Contains(got, "assistant_saved_prompts") {
		t.Fatalf("a change is told to the apps: %q", got)
	}
	addedSavedPrompt(t, s, `{"name":"发布说明","text":"Write the release notes\tfor the commits since the last tag."}`)
	addedSavedPrompt(t, s, `{"name":"pr-check_v2.1","text":"Run the PR checklist."}`)

	for _, bad := range []struct{ body, why string }{
		{`{"name":"REVIEW","text":"x"}`, "a name another prompt has, in other case"},
		{`{"name":"","text":"x"}`, "no name"},
		{`{"name":"/","text":"x"}`, "only a slash"},
		{`{"name":"two words","text":"x"}`, "a name with a space"},
		{`{"name":"a/b","text":"x"}`, "a slash inside the name"},
		{`{"name":"@me","text":"x"}`, "a mention's @"},
		{`{"name":"` + strings.Repeat("长", saved.PromptNameMax+1) + `","text":"x"}`, "a name too long"},
		{`{"name":"empty","text":"  \n "}`, "no text"},
		{`{"name":"bell","text":"ring\u0007"}`, "a control character in the text"},
		{`{"name":"long","text":"` + strings.Repeat("a", saved.PromptTextMax+1) + `"}`, "a text too long"},
		{`{"name":"x"`, "broken JSON"},
	} {
		if rec := placeDo(s, "", "POST", savedPromptsPath, bad.body); rec.Code < 400 {
			t.Errorf("%s is refused: %d %s", bad.why, rec.Code, rec.Body)
		}
	}
	if rec := placeDo(s, "", "POST", savedPromptsPath, `{"name":"Review","text":"x"}`); rec.Code != 409 || !strings.Contains(rec.Body.String(), "/Review") {
		t.Fatalf("the same name twice is a conflict that names it: %d %s", rec.Code, rec.Body)
	}
	if rec := placeDo(s, "", "POST", savedPromptsPath, `{"name":"long","text":"`+strings.Repeat("长", saved.PromptTextMax)+`"}`); rec.Code != 200 {
		t.Fatalf("the text limit counts characters, not bytes: %d %s", rec.Code, rec.Body)
	}
	if got := listedSavedPromptsOf(t, s); len(got) != 4 || got[0].Name != "review" || got[1].Name != "发布说明" || got[2].Name != "pr-check_v2.1" {
		t.Fatalf("kept in the order added: %+v", got)
	}

	rec := placeDo(s, "", "PUT", savedPromptsPath+"/"+review.ID, `{"name":"Review","text":"Review this diff for security issues."}`)
	var changed struct{ Prompt saved.Prompt }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &changed) != nil {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
	if p := changed.Prompt; p.ID != review.ID || p.Name != "Review" || p.Text != "Review this diff for security issues." || !p.CreatedAt.Equal(review.CreatedAt) {
		t.Fatalf("changed in place, its own name in another case is fine: %+v", p)
	}
	if got := listedSavedPromptsOf(t, s); got[0].ID != review.ID {
		t.Fatalf("a change keeps its place in the list: %+v", got)
	}
	if rec := placeDo(s, "", "PUT", savedPromptsPath+"/"+review.ID, `{"name":"发布说明","text":"x"}`); rec.Code != 409 {
		t.Fatalf("renaming to another prompt's name is a conflict: %d", rec.Code)
	}
	if rec := placeDo(s, "", "PUT", savedPromptsPath+"/nope", `{"name":"x","text":"x"}`); rec.Code != 404 {
		t.Fatalf("changing a prompt that is not there: %d", rec.Code)
	}

	for _, req := range [][3]string{{"POST", savedPromptsPath, `{"name":"x","text":"x"}`},
		{"PUT", savedPromptsPath + "/" + review.ID, `{"name":"x","text":"x"}`}} {
		if rec := placeDo(s, "read", req[0], req[1], req[2]); rec.Code != 403 {
			t.Errorf("a read-only device cannot %s %s: %d", req[0], req[1], rec.Code)
		}
	}
	if rec := placeDo(s, "read", "GET", savedPromptsPath, ""); rec.Code != 200 {
		t.Fatalf("a read-only device sees them: %d", rec.Code)
	}
}

func TestSavedPromptsNeedADatabase(t *testing.T) {
	s := newStorelessService(t, newFakeHub())
	if rec := placeDo(s, "", "GET", savedPromptsPath, ""); rec.Code != 503 {
		t.Fatalf("list without a database: %d", rec.Code)
	}
	if rec := placeDo(s, "", "POST", savedPromptsPath, `{"name":"x","text":"x"}`); rec.Code != 503 {
		t.Fatalf("add without a database: %d", rec.Code)
	}
}

func TestSavedPromptsAreLimited(t *testing.T) {
	s := newTestService(t, newFakeHub())
	ids := map[string]bool{}
	for i := range saved.PromptsMax {
		p := addedSavedPrompt(t, s, fmt.Sprintf(`{"name":"p%d","text":"text %d"}`, i, i))
		if ids[p.ID] {
			t.Fatalf("prompts added in a row get ids of their own: %s twice", p.ID)
		}
		ids[p.ID] = true
	}
	if rec := placeDo(s, "", "POST", savedPromptsPath, `{"name":"onemore","text":"x"}`); rec.Code != 400 {
		t.Fatalf("past the limit: %d %s", rec.Code, rec.Body)
	}
}
