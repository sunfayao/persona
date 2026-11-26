package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
)

// What an acceptance took is the rounds since the task was last accepted, not the ledger's running count: a task
// accepted, reopened and accepted again is not charged its earlier rounds twice.
func TestReworkedSinceAccepted(t *testing.T) {
	r := func(verdicts ...string) Supervision {
		var v Supervision
		for _, verdict := range verdicts {
			v.Reviews = append(v.Reviews, TaskReview{Verdict: verdict})
		}
		return v
	}
	for _, c := range []struct {
		v    Supervision
		want int
	}{
		{r(), 0},
		{r(reviewAccepted), 0},
		{r(reviewRework), 1},
		{r(reviewRework, reviewRework, reviewAccepted), 2},
		{r(reviewRework, reviewRework, reviewAccepted, reviewRework, reviewAccepted), 1},
		{r(reviewRework, reviewAccepted, reviewRework), 1},
		{r(reviewRework, reviewNeedsUser, reviewParked, reviewRework, reviewAccepted), 2},
	} {
		if got := c.v.reworkedSinceAccepted(); got != c.want {
			t.Errorf("%v: %d rounds, want %d", c.v.Reviews, got, c.want)
		}
	}
}

// planUpdateStore is planMemStore whose plans can also change state, so a task_plan starts on it.
type planUpdateStore struct{ *planMemStore }

func (m planUpdateStore) UpdateTaskPlan(_ context.Context, id, from, to string, tasks json.RawMessage, at time.Time) (bool, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	p, ok := m.plans[id]
	if !ok || p.State != from {
		return false, nil
	}
	p.State, p.Tasks, p.UpdatedAt = to, tasks, at
	m.plans[id] = p
	return true, nil
}

// The main chat's rule for a task's model (#5086) speaks of tiers and names no provider's model, and says what to do
// when the tiers cannot be told apart.
func TestTaskModelRuleNamesNoModel(t *testing.T) {
	md := prompts.RenderAssistantClaudeMD(prompts.AssistantClaudeMD{})
	_, rest, ok := strings.Cut(md, "**Model and effort**")
	if !ok {
		t.Fatal("CLAUDE.md has no rule for a task's model")
	}
	rule, _, ok := strings.Cut(rest, "in `why`.")
	if !ok {
		t.Fatal("the rule does not end with stating the choice in why")
	}
	for _, want := range []string{"one level below", "`medium`", "everyday Agent model is the baseline", "uses the highest", "raise `effort` first", "leave `effort` empty", "leave `tier`, `model` and `effort` empty", "the user's configured defaults", "always wins"} {
		if !strings.Contains(rule, want) {
			t.Errorf("the rule lacks %q", want)
		}
	}
	lower := strings.ToLower(rule)
	for _, name := range []string{"claude", "opus", "sonnet", "haiku", "fable", "gpt", "gemini", "codex", "deepseek"} {
		if strings.Contains(lower, name) {
			t.Errorf("the rule names %q", name)
		}
	}
}
