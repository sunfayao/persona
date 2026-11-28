package assistant

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/sunfayao/persona/server/internal/assistantskills"
)

const releaseNotes = `{"name":"release-notes","description":"Use when the user asks for release notes","body":"# Release notes\n\n1. List the merged changes.\n2. Group them by area.\n"}`

// skillSaveFixture is mcpSkillFixture whose computer also lists its skills (skills.list), made inside the test's bubble.
func skillSaveFixture(t *testing.T) *Service {
	t.Helper()
	s, _, _ := mcpSkillFixture(t)
	s.hub.(*fakeHub).answerNode("skills.list", func(string, json.RawMessage) (any, error) {
		return map[string]any{"skills": assistantskills.List(assistantskills.UserDir(), assistantskills.Selection{})}, nil
	})
	return s
}

// importOps are the skills.import operations the computer was asked for, in order.
func importOps(s *Service) []string {
	f := s.hub.(*fakeHub)
	f.mu.Lock()
	defer f.mu.Unlock()
	var ops []string
	for _, c := range f.calls {
		var in assistantskills.ImportRequest
		if c.Method == "skills.import" && json.Unmarshal(c.Params, &in) == nil {
			ops = append(ops, in.Op)
		}
	}
	return ops
}

func TestSkillSaveToolInstallsDirectlyWithoutCard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := skillSaveFixture(t)
		installed := filepath.Join(assistantskills.UserDir(), "release-notes")
		res := skillImportCall(context.Background(), s, "skill_save", releaseNotes)
		if len(s.pendingQuestions()) != 0 {
			t.Fatalf("unexpected approval card: %+v", s.pendingQuestions())
		}
		want := "---\nname: release-notes\ndescription: Use when the user asks for release notes\n---\n\n# Release notes\n\n1. List the merged changes.\n2. Group them by area.\n"
		var out skillImportOutput
		if res.IsError || json.Unmarshal([]byte(res.Content[0].Text), &out) != nil || out.Status != "installed" || out.Reference == "" || out.Environment == "" {
			t.Fatalf("result %+v", res)
		}
		if b, err := os.ReadFile(filepath.Join(installed, "SKILL.md")); err != nil || string(b) != want {
			t.Fatalf("SKILL.md %q (%v), want %q", b, err, want)
		}
		if ops := importOps(s); !slices.Equal(ops, []string{"preview", "install"}) {
			t.Fatalf("the computer was asked for %v", ops)
		}
		// The receipt is skill_import's: its status, and its undo, take it.
		status := skillToolResult(t, s, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "status", Reference: out.Reference}, Environment: out.Environment})
		if status.Status != "installed" || status.Name != "release-notes" {
			t.Fatalf("status %+v", status)
		}
	})
}

func TestSkillSaveToolRefusesANameInUseNamingTheSkill(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := skillSaveFixture(t)
		existing := filepath.Join(assistantskills.UserDir(), "release-notes", "SKILL.md")
		const kept = "---\nname: release-notes\ndescription: The user's own release steps\n---\n# Steps\n"
		if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(existing, []byte(kept), 0o644); err != nil {
			t.Fatal(err)
		}
		res := skillImportCall(context.Background(), s, "skill_save", releaseNotes)
		if !res.IsError || !strings.HasPrefix(res.Content[0].Text, `skill_conflict: the main conversation's computer already has the user's skill "release-notes" ("The user's own release steps"); nothing was saved`) {
			t.Fatalf("result %+v", res)
		}
		if b, _ := os.ReadFile(existing); string(b) != kept || len(s.pendingQuestions()) != 0 {
			t.Fatalf("the existing skill changed (%q) or a card waits", b)
		}
	})
}

func TestSkillSaveToolRefusesAComputersNameBeforeAskingAnyone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := skillSaveFixture(t)
		res := skillImportCall(context.Background(), s, "skill_save",
			`{"name":"release-notes","description":"Release notes","body":"1. Build on the Example computer.\n2. Mail them to team@example.com."}`)
		if !res.IsError || !strings.HasPrefix(res.Content[0].Text, "skill_save_private: ") ||
			!strings.Contains(res.Content[0].Text, "\nbody line 1: 1. Build on the <machine>.\nbody line 2: 2. Mail them to <email>.\n") {
			t.Fatalf("result %+v", res)
		}
		if ops := importOps(s); len(ops) != 0 || len(s.pendingQuestions()) != 0 {
			t.Fatalf("the computer was asked for %v, or a card waits", ops)
		}
	})
}
