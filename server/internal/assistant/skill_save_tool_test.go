package assistant

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantskills"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
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

// startSkillSave calls skill_save from the conversation c1 and returns its result when it ends.
func startSkillSave(s *Service, args string) <-chan proto.ToolResult {
	done := make(chan proto.ToolResult, 1)
	go func() { done <- skillImportCall(context.Background(), s, "skill_save", args) }()
	return done
}

// skillSaveCard is the one card waiting once every goroutine of the bubble is blocked. Without one the test ends
// here: nothing then waits in the bubble.
func skillSaveCard(t *testing.T, s *Service) proto.Interaction {
	t.Helper()
	synctest.Wait()
	cards := s.pendingQuestions()
	if len(cards) != 1 {
		t.Fatalf("cards waiting: %+v", cards)
	}
	return cards[0]
}

func answerSkillSaveCard(t *testing.T, s *Service, card proto.Interaction, answer proto.Resolution) {
	t.Helper()
	raw, _ := json.Marshal(answer)
	if _, err := s.Action(context.Background(), Action{Op: "answer", InteractionID: card.ID, Answer: raw}); err != nil {
		t.Fatal(err)
	}
}

func TestSkillSaveToolInstallsAfterTheUsersApproval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := skillSaveFixture(t)
		installed := filepath.Join(assistantskills.UserDir(), "release-notes")
		done := startSkillSave(s, releaseNotes)
		card := skillSaveCard(t, s)
		want := "---\nname: release-notes\ndescription: Use when the user asks for release notes\n---\n\n# Release notes\n\n1. List the merged changes.\n2. Group them by area.\n"
		// The card is in the user's language: it names the skill, its size and the computer, not the procedure.
		if card.ToolName != "skill_save" || card.Kind != proto.IxPermission || !strings.Contains(card.Summary, "release-notes") ||
			!strings.Contains(card.Summary, "Example computer") || !strings.Contains(card.Summary, strconv.Itoa(len(want))) ||
			strings.Contains(string(card.ToolInput), "merged changes") {
			t.Errorf("card %q %s", card.Summary, card.ToolInput)
		}
		if _, err := os.Stat(installed); !os.IsNotExist(err) {
			t.Errorf("the skill was installed before the user approved it: %v", err)
		}
		allow := true
		answerSkillSaveCard(t, s, card, proto.Resolution{Allow: &allow})
		res := <-done
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

func TestSkillSaveToolChangesNothingUnlessTheUserApproves(t *testing.T) {
	allow, deny := true, false
	for _, c := range []struct {
		name   string
		answer *proto.Resolution // nil: the card is left until it expires
		want   string
	}{
		{"declined", &proto.Resolution{Allow: &deny}, "The user declined the change: nothing changed."},
		{"closed", &proto.Resolution{Dismiss: true}, prompts.RenderSettingUnanswered()},
		{"approved by the assistant", &proto.Resolution{Allow: &allow, AnsweredBy: "assistant"}, prompts.RenderSettingUnanswered()},
		{"expired", nil, prompts.RenderSettingUnanswered()},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := skillSaveFixture(t)
				done := startSkillSave(s, releaseNotes)
				card := skillSaveCard(t, s)
				if c.answer != nil {
					answerSkillSaveCard(t, s, card, *c.answer)
				} else {
					time.Sleep(11 * time.Minute) // sleep-ok: virtual synctest time runs out the card's ten minutes
				}
				res := <-done
				if res.IsError || res.Content[0].Text != c.want {
					t.Fatalf("result %+v, want %q", res, c.want)
				}
				if _, err := os.Stat(filepath.Join(assistantskills.UserDir(), "release-notes")); !os.IsNotExist(err) {
					t.Fatalf("a skill was installed: %v", err)
				}
				if ops := importOps(s); !slices.Equal(ops, []string{"preview", "cancel"}) {
					t.Fatalf("the computer was asked for %v; want the preview dropped", ops)
				}
				if len(s.pendingQuestions()) != 0 {
					t.Fatal("the card is still waiting")
				}
			})
		})
	}
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
		if !res.IsError || !strings.HasPrefix(res.Content[0].Text, `skill_conflict: the assistant's computer already has the user's skill "release-notes" ("The user's own release steps"); nothing was saved`) {
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
