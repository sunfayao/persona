package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The late-confirmation recheck reads the rollout itself: a goal event already written, whether or not a background
// scan has caught up, suppresses the text fallback.
func TestCodexRolloutGoalSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	since := time.Now()
	goalLine := func(updatedAt int64, null bool) string {
		var goal any
		if !null {
			goal = map[string]any{"objective": "CI is green", "status": "active", "updatedAt": updatedAt}
		}
		line, _ := json.Marshal(map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "type": "event_msg",
			"payload": map[string]any{"type": "thread_goal_updated", "goal": goal}})
		return string(line)
	}
	old := goalLine(since.Add(-time.Minute).Unix(), false) + "\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if codexRolloutGoalSet(path, since) {
		t.Fatal("a goal from before the send counted")
	}
	recent := goalLine(since.Unix(), false) + "\n"
	if err := os.WriteFile(path, []byte(old+recent), 0o600); err != nil {
		t.Fatal(err)
	}
	if !codexRolloutGoalSet(path, since) {
		t.Fatal("the goal the rollout already shows was not seen")
	}
	cleared := goalLine(since.Unix(), true) + "\n"
	if err := os.WriteFile(path, []byte(old+cleared), 0o600); err != nil {
		t.Fatal(err)
	}
	if codexRolloutGoalSet(path, since) {
		t.Fatal("a cleared goal counted")
	}
}

// The text goal's readback: a user message at or after the send counts, also inside Codex's own wrapper and with the
// terminal's whitespace; the task's own first message never counts, even when it has the same words as the goal and is
// less than a second old.
func TestCodexRolloutUserMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	since := time.Now()
	userLine := func(at time.Time, text string) string {
		line, _ := json.Marshal(map[string]any{"timestamp": at.UTC().Format(time.RFC3339Nano), "type": "event_msg",
			"payload": map[string]any{"type": "item_completed",
				"item": map[string]any{"type": "UserMessage", "id": "u", "content": []any{map[string]any{"type": "text", "text": text}}}}})
		return string(line)
	}
	goal := "CI is green"
	// The task's first message, just before the goal was typed and with the same words: never the goal's readback.
	before := userLine(since.Add(-100*time.Millisecond), goal) + "\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if codexRolloutUserMessage(path, goal, since) {
		t.Fatal("the task's own first message counted as the goal's delivery")
	}
	wrapped := "<user_instructions>\nCI  is\n green\n</user_instructions>"
	if err := os.WriteFile(path, []byte(before+userLine(since.Add(time.Second), wrapped)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !codexRolloutUserMessage(path, goal, since) {
		t.Fatal("the wrapped, re-wrapped message was not read back")
	}
	if codexRolloutUserMessage(path, "another goal", since) {
		t.Fatal("a different text counted")
	}
	if codexRolloutUserMessage(filepath.Join(t.TempDir(), "missing.jsonl"), goal, since) {
		t.Fatal("a missing rollout counted")
	}
}
