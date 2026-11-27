package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	subParent = "aaaaaaaa-0000-0000-0000-000000000002"
	subAgent  = "a1b2c3d4e5f60718"
	subID     = subParent + "@" + subAgent
)

// subagentFixture lays out a parent transcript with one subagent file beside it and returns the subagent's path.
func subagentFixture(t *testing.T) (*Node, string) {
	t.Helper()
	n, home := testNode(t)
	tp := filepath.Join(home, ".claude", "projects", "-src-app", subParent+".jsonl")
	dir := filepath.Join(strings.TrimSuffix(tp, ".jsonl"), "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.paths[subParent] = tp
	return n, filepath.Join(dir, "agent-"+subAgent+".jsonl")
}

func appendSubagentText(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := `{"type":"assistant","timestamp":"2026-11-05T09:01:00Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func hasText(h *hubEnd, text string) bool {
	for _, it := range h.got() {
		if it.Text == text {
			return true
		}
	}
	return false
}

// Followed subagent transcripts are bounded; a watch past the bound is refused (the page falls back to fetching), and
// the others keep going.
func TestSubagentWatchesAreBounded(t *testing.T) {
	n, jp := subagentFixture(t)
	hub := withHub(t, n)
	dir := filepath.Dir(jp)
	for i := 0; i < maxSubagentWatches; i++ {
		agent := "agent" + string(rune('a'+i))
		appendSubagentText(t, filepath.Join(dir, "agent-"+agent+".jsonl"), "x")
		if err := n.watch(subParent+"@"+agent, 0); err != nil {
			t.Fatalf("watch %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		for i := 0; i < maxSubagentWatches; i++ {
			n.unwatch(subParent + "@agent" + string(rune('a'+i)))
		}
	})
	appendSubagentText(t, jp, "over")
	if err := n.watch(subID, 0); err == nil {
		t.Fatal("a watch past the bound was taken")
	}
	// A repeated watch of one followed is not a new one, and a freed place can be taken.
	if err := n.watch(subParent+"@agenta", 0); err != nil {
		t.Fatalf("watching a followed one again: %v", err)
	}
	n.unwatch(subParent + "@agentb")
	appendSubagentText(t, jp, "after")
	if err := n.watch(subID, 0); err != nil {
		t.Fatalf("a freed place was not taken: %v", err)
	}
	t.Cleanup(func() { n.unwatch(subID) })
	appendSubagentText(t, jp, "live")
	waitFor(t, "the new watch to push", func() bool { return hasText(hub, "live") })
}
