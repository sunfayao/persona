package digestfmt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
)

// golden compares got with testdata/<name>.golden (UPDATE_GOLDEN=1 rewrites it).
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UPDATE_GOLDEN=1 writes it)", name, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s\n--- got ---\n%s\n--- want ---\n%s", name, path, got, want)
	}
}

// criteria are n criteria of 150 characters; the first two have a reading.
func criteria(tag string, n int) []prompts.SuperviseCriterion {
	var out []prompts.SuperviseCriterion
	for i := 1; i <= n; i++ {
		text := fmt.Sprintf("%s criterion %d: the change is covered by a focused test, the whole package passes, and the behaviour was checked against the real page in both light and dark themes", tag, i)[:150]
		c := prompts.SuperviseCriterion{Text: text, State: "pending"}
		switch i {
		case 1:
			c.State, c.Evidence, c.By = "met", "go test ./internal/example passes, 14 tests", "hub"
		case 2:
			c.State, c.Evidence, c.By = "met", "screenshot attached to the pull request", "agent:box/abc123"
		}
		out = append(out, c)
	}
	return out
}

// last is about 600 characters of an Agent's last words.
func last() string {
	return strings.Repeat("The work is finished and verified; here is what I changed and how I checked it. ", 8)[:600]
}

// replay is a real evening's batch: five signals on four to-dos: an estimate reminder for an Agent still making progress,
// an estimate reminder and a stop for an Agent that finished, a review with a done report for a to-do accepted meanwhile,
// and a plain review.
func replay() []prompts.SuperviseEntry {
	return []prompts.SuperviseEntry{
		{ID: "todo_1", Title: "Fix the login crash on the settings page", Kind: "estimate_due", Detail: "its estimate of 30 minutes ran out after 45",
			Criteria: criteria("A", 5), Last: last(), Machine: "box", Session: "s1"},
		{ID: "todo_2", Title: "Add the export button to the report view", Kind: "estimate_due", Detail: "its estimate of 20 minutes ran out after 30",
			Criteria: criteria("B", 4), Last: last(), Machine: "box", Session: "s2"},
		{ID: "todo_2", Title: "Add the export button to the report view", Kind: "idle",
			Criteria: criteria("B", 4), Last: last(), Machine: "box", Session: "s2"},
		{ID: "todo_3", Title: "Rework the onboarding copy", Kind: "review", Said: last(), Evidence: []string{"the pull request is merged"},
			Criteria: criteria("C", 5), Last: last(), Machine: "box", Session: "s3"},
		{ID: "todo_4", Title: "Upgrade the dependency and fix the fallout", Kind: "review",
			Criteria: criteria("D", 4), Last: last(), Machine: "box", Session: "s4"},
	}
}

// oldRenderedBytes is what the renderer before this one wrote for the same batch (every signal its own numbered entry,
// its criteria twice, evidence lines and all).
const oldRenderedBytes = 12816

func TestReplayedBatchIsSmall(t *testing.T) {
	out := Render(Input{Role: prompts.SuperviseForMain, Entries: replay(), Closed: map[string]string{"todo_3": ""}})
	t.Logf("replayed batch: %d bytes now, %d before", len(out.Text), oldRenderedBytes)
	if len(out.Text) >= 5000 {
		t.Errorf("the replayed batch is %d bytes, want under 5000:\n%s", len(out.Text), out.Text)
	}
	if strings.Contains(out.Text, "todo_3") {
		t.Errorf("the to-do accepted meanwhile is in the message:\n%s", out.Text)
	}
	for _, id := range []string{"todo_1", "todo_2", "todo_4"} {
		if n := strings.Count(out.Text, "("+id+","); n != 1 {
			t.Errorf("%s has %d blocks, want 1", id, n)
		}
	}
	golden(t, "main_replay", out.Text)
}

func TestFirstThenFollowUp(t *testing.T) {
	first := replay()[:1]
	in := Input{Role: prompts.SuperviseForMain, Entries: first}
	one := Render(in)
	golden(t, "main_first", one.Text)
	if strings.Contains(one.Text, "unchanged") {
		t.Errorf("a first block says something is unchanged:\n%s", one.Text)
	}

	// The same to-do again: a criterion was met, a new signal came, the last words changed.
	next := replay()[:1]
	next[0].Criteria = criteria("A", 5)
	next[0].Criteria[2] = prompts.SuperviseCriterion{Text: next[0].Criteria[2].Text, State: "met", Evidence: "the page was checked in both themes", By: "supervisor:conv1"}
	next[0].Last = "A new last word: the fix is in and the tests pass."
	next[0].Kind, next[0].Detail = "idle", ""
	in.Entries, in.Told = next, one.Told
	two := Render(in)
	golden(t, "main_follow_up", two.Text)
	if len(two.Text) >= len(one.Text) {
		t.Errorf("the follow-up (%d bytes) is not shorter than the first message (%d)", len(two.Text), len(one.Text))
	}
	for _, kept := range []string{"3. [met]", "A new last word", "unchanged since the previous message"} {
		if !strings.Contains(two.Text, kept) {
			t.Errorf("the follow-up lacks %q:\n%s", kept, two.Text)
		}
	}
	for _, left := range []string{"A criterion 1", "A criterion 4", "estimate"} {
		if strings.Contains(two.Text, left) {
			t.Errorf("the follow-up repeats %q:\n%s", left, two.Text)
		}
	}

	// Identical again: the block is its header and the line.
	in.Entries, in.Told = next, two.Told
	three := Render(in)
	golden(t, "main_follow_up_nothing_new", three.Text)
	if !strings.Contains(three.Text, "unchanged since the previous message") || strings.Contains(three.Text, "A criterion") {
		t.Errorf("a repeated block still carries its parts:\n%s", three.Text)
	}

	// A scope change adds a criterion: they are told whole again.
	more := replay()[:1]
	more[0].Criteria = append(criteria("A", 5), prompts.SuperviseCriterion{Text: "a sixth criterion", State: "pending"})
	in.Entries, in.Told = more, one.Told
	four := Render(in)
	if !strings.Contains(four.Text, "A criterion 1") || !strings.Contains(four.Text, "6. [pending] a sixth criterion") || strings.Contains(four.Text, "only the changed ones") {
		t.Errorf("a block with a new criterion does not tell them whole:\n%s", four.Text)
	}
}

func TestSupervisorAgentGetsTheSameBlocks(t *testing.T) {
	entries := replay()[1:3]
	entries[0].Own, entries[1].Own = true, true
	one := Render(Input{Role: prompts.SuperviseForLeader, Entries: entries})
	golden(t, "leader_first", one.Text)
	main := Render(Input{Role: prompts.SuperviseForMain, Entries: entries})
	for _, shared := range []string{"1. [met] B criterion 1", "go test ./internal/example passes, 14 tests (by hub)", "(by the Agent)", "signals=estimate_due, idle"} {
		if !strings.Contains(one.Text, shared) || !strings.Contains(main.Text, shared) {
			t.Errorf("%q is not in both messages:\n%s\n----\n%s", shared, one.Text, main.Text)
		}
	}
	two := Render(Input{Role: prompts.SuperviseForLeader, Entries: entries, Told: one.Told})
	golden(t, "leader_follow_up", two.Text)
	if strings.Contains(two.Text, "B criterion 3") {
		t.Errorf("the supervisor Agent's follow-up repeats a criterion:\n%s", two.Text)
	}
}

func TestClosedToDoIsDroppedOrOneLine(t *testing.T) {
	entries := replay()[3:]
	dropped := Render(Input{Role: prompts.SuperviseForMain, Entries: entries, Closed: map[string]string{"todo_3": ""}})
	if strings.Contains(dropped.Text, "Rework") || !strings.Contains(dropped.Text, "1 of the tasks") {
		t.Errorf("a to-do that ended is not dropped:\n%s", dropped.Text)
	}
	golden(t, "main_dropped", dropped.Text)
	line := Render(Input{Role: prompts.SuperviseForMain, Entries: entries, Closed: map[string]string{"todo_3": "accepted"}})
	golden(t, "main_closed_one_line", line.Text)
	if strings.Contains(line.Text, "C criterion") || !strings.Contains(line.Text, "it was accepted since this signal was raised") {
		t.Errorf("a to-do that ended is not one line:\n%s", line.Text)
	}
	if _, told := line.Told["todo_3"]; told {
		t.Errorf("a to-do that was told in one line counts as told whole")
	}
}

func TestCapCutsTheLastWordsFirstAndNeverTheCriteria(t *testing.T) {
	e := replay()[4]
	e.Last = strings.Repeat("A long last answer that goes on and on, with the conclusion at its very end. ", 120)
	e.Criteria = criteria("D", 5)
	out := Render(Input{Role: prompts.SuperviseForMain, Entries: []prompts.SuperviseEntry{e}})
	golden(t, "main_cap", out.Text)
	if !strings.Contains(out.Text, "agent_read with session_id=s4") || !strings.Contains(out.Text, "cut to keep this message short") {
		t.Errorf("a cut block does not say where the rest is:\n%s", out.Text)
	}
	if !strings.HasSuffix(strings.Split(out.Text, "\n   (cut")[0], "with the conclusion at its very end.") {
		t.Errorf("the cut kept the wrong end of the last words:\n%s", out.Text)
	}
	for i := 1; i <= 5; i++ {
		if !strings.Contains(out.Text, fmt.Sprintf("%d. [", i)) {
			t.Errorf("criterion %d was cut", i)
		}
	}
	if n := len(prompts.RenderSuperviseBlock(out.Whole[0])); n < BlockBytes {
		t.Logf("whole block is %d bytes", n)
	}
	if len(out.Text) > BlockBytes+1200 {
		t.Errorf("a one-to-do message is %d bytes, want about %d", len(out.Text), BlockBytes)
	}

	// Many to-dos: every block's last words are cut so the message stays bounded; the criteria stay.
	var many []prompts.SuperviseEntry
	for i := range 6 {
		x := replay()[3]
		x.ID, x.Title = fmt.Sprintf("todo_%d", i+10), fmt.Sprintf("Task number %d", i)
		x.Criteria = criteria(x.ID, 3)
		many = append(many, x)
	}
	big := Render(Input{Role: prompts.SuperviseForMain, Entries: many})
	if strings.Count(big.Text, "(cut to keep this message short") != 6 || strings.Contains(big.Text, "The work is finished") {
		t.Errorf("the last words of a long message were not cut to nothing:\n%s", big.Text)
	}
	if strings.Count(big.Text, "criterion 3") != 6 {
		t.Errorf("criteria were cut:\n%s", big.Text)
	}
}

func TestBookTellsOnlyWhatWentIn(t *testing.T) {
	b := NewBook()
	in := Input{Role: prompts.SuperviseForMain, Entries: replay()[:1]}
	one := Render(in)
	b.Offer("conv", one.Text, one)
	if got := b.Told("conv"); len(got) != 0 {
		t.Fatalf("a message that was only rendered counts as told: %v", got)
	}
	b.Sending("conv", one.Text, true)
	if going, judged := b.Going("conv"); !going || !judged {
		t.Fatalf("Going = %v, %v during a send", going, judged)
	}
	b.Done("conv", one.Text, false)
	if going, _ := b.Going("conv"); going || len(b.Told("conv")) != 0 {
		t.Fatalf("a refused message counts as told")
	}
	b.Sending("conv", one.Text, true)
	b.Done("conv", one.Text, true)
	if _, ok := b.Told("conv")["todo_1"]; !ok {
		t.Fatalf("a delivered message is not told")
	}
	if _, ok := b.Told("other")["todo_1"]; ok {
		t.Fatalf("another conversation was told")
	}

	// A follow-up is on its way when the conversation is compacted: it gets the whole blocks and is told afterwards.
	in.Told = b.Told("conv")
	in.Entries[0].Last = "new words"
	two := Render(in)
	b.Offer("conv", two.Text, two)
	b.Sending("conv", two.Text, true)
	whole := b.Compacted("conv")
	if len(whole) != 1 || !whole[0].ShowCriteria || len(whole[0].Criteria) != 5 {
		t.Fatalf("Compacted gave %+v", whole)
	}
	if len(b.Told("conv")) != 0 {
		t.Fatalf("what a compacted conversation was told is kept")
	}
	b.Done("conv", two.Text, true)
	if _, ok := b.Told("conv")["todo_1"]; !ok {
		t.Fatalf("the message that carried the whole blocks does not count as told")
	}
	if !strings.Contains(prompts.RenderDigestWhole(whole), "A criterion 5") {
		t.Fatalf("the whole blocks are not rendered")
	}
	golden(t, "digest_whole", prompts.RenderDigestWhole(whole))

	// A compaction with no message on its way only forgets.
	if got := b.Compacted("conv"); got != nil || len(b.Told("conv")) != 0 {
		t.Fatalf("Compacted with no send = %v, told %v", got, b.Told("conv"))
	}
}

func TestFreshRoundIsAlwaysWhole(t *testing.T) {
	in := Input{Role: prompts.SuperviseForMain, Entries: replay()[:1], Frame: prompts.Supervise{Fresh: true}}
	one := Render(in)
	in.Told = one.Told
	if two := Render(in); two.Text != one.Text {
		t.Errorf("a round that starts from a clean context was reduced:\n%s", two.Text)
	}
}
