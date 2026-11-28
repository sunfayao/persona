package tooluse

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var dreamNow = time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

// failure is a failed call of tool, with code and skeleton, in conversation session, ago before dreamNow.
func failure(tool, code, skeleton, session string, ago time.Duration) Event {
	return Event{At: dreamNow.Add(-ago), Tool: tool, Runtime: "agy", Code: code, Skeleton: skeleton, Machine: "m",
		Session: session}
}

// cured is e when a retry shaped retry worked after it.
func cured(e Event, retry string) Event {
	e.RetrySkeleton = retry
	return e
}

// Only friction that repeats becomes a draft: three failures of one tool, code and skeleton (a fix, none cured), or two
// when a retry with other arguments cured one (a note). Another skeleton is another group.
func TestDreamDraftsOnlyRepeatedFriction(t *testing.T) {
	t.Parallel()
	events := []Event{
		failure("twice", "c", "{a:str}", "s1", 5*time.Hour), failure("twice", "c", "{a:str}", "s2", 4*time.Hour),
		failure("thrice", "c", "{a:str}", "s1", 5*time.Hour), failure("thrice", "c", "{a:str}", "s1", 4*time.Hour),
		failure("thrice", "c", "{a:str}", "s2", 3*time.Hour),
		cured(failure("paired", "c", "{a:str}", "s1", 5*time.Hour), "{b:str}"), failure("paired", "c", "{a:str}", "s3", 3*time.Hour),
		cured(failure("once", "c", "{a:str}", "s1", 2*time.Hour), "{b:str}"),
		failure("thrice", "c", "{other:num}", "s1", 2*time.Hour), failure("thrice", "c", "{other:num}", "s1", time.Hour),
	}
	drafts := DreamDrafts(nil, events, dreamNow)
	got := map[string]string{}
	ids := map[string]bool{}
	for _, d := range drafts {
		got[d.Tool+" "+d.Skeleton] = d.Kind
		ids[d.ID] = true
		if d.Status != DraftPending || !strings.HasPrefix(d.ID, "fd_") || !d.UpdatedAt.Equal(dreamNow) {
			t.Errorf("a new draft: %+v", d)
		}
	}
	if want := map[string]string{"thrice {a:str}": DraftFix, "paired {a:str}": DraftNote}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("drafts %v, want %v", got, want)
	}
	if len(ids) != len(drafts) {
		t.Fatalf("ids repeat: %v", ids)
	}
}

// A field note is one line naming what fails and what worked, from the skeletons alone: no value of the arguments is in
// any draft. One too long for 200 characters shows what nests deepest as "…", and is cut as a last resort.
func TestDreamNoteNamesWhatFailsAndWhatWorked(t *testing.T) {
	t.Parallel()
	launch := Skeleton(json.RawMessage(`{"steps":[{"launch_app":{"name":"Calculator"}}]}`))
	script := Skeleton(json.RawMessage(`{"script":"await cua.launch(\"Calculator\")"}`))
	drafts := DreamDrafts(nil, []Event{
		cured(failure("computer_run", "invalid_batch", launch, "s1", 3*time.Hour), script),
		failure("computer_run", "invalid_batch", launch, "s2", 2*time.Hour),
	}, dreamNow)
	want := "computer_run: {steps:[{launch_app:{name:str}}]} fails with invalid_batch; what worked: {script:str}"
	if len(drafts) != 1 || drafts[0].Kind != DraftNote || drafts[0].Text != want || drafts[0].RetrySkeleton != script {
		t.Fatalf("drafts %+v, want the note %q", drafts, want)
	}
	raw, _ := json.Marshal(drafts)
	for _, value := range []string{"Calculator", "await"} {
		if strings.Contains(string(raw), value) {
			t.Errorf("a draft holds %q: %s", value, raw)
		}
	}

	var keys []string
	for i := range 30 {
		keys = append(keys, fmt.Sprintf("k%02d:str", i))
	}
	wide := "{steps:[{launch_app:{" + strings.Join(keys, ",") + "}}]}"
	text := DreamDrafts(nil, []Event{
		cured(failure("computer_run", "invalid_batch", wide, "s1", 3*time.Hour), script),
		failure("computer_run", "invalid_batch", wide, "s2", 2*time.Hour),
	}, dreamNow)[0].Text
	if want := "computer_run: {steps:[{launch_app:…}]} fails with invalid_batch; what worked: {script:str}"; text != want {
		t.Fatalf("a long skeleton: %q, want %q", text, want)
	}

	// Wide at the top, where nothing nests to leave out: only cutting fits it.
	name, code, top := "Bad <name> "+strings.Repeat("n", 80), strings.Repeat("c", 80), "{"+strings.Join(keys, ",")+"}"
	text = DreamDrafts(nil, []Event{
		cured(failure(name, code, top, "s1", 3*time.Hour), top),
		failure(name, code, top, "s2", 2*time.Hour),
	}, dreamNow)[0].Text
	if utf8.RuneCountInString(text) > noteMax || !strings.HasPrefix(text, "Badname"+strings.Repeat("n", 57)+": ") ||
		!strings.HasSuffix(text, "…") {
		t.Fatalf("a long name and code: %q (%d characters)", text, utf8.RuneCountInString(text))
	}
}

// A fix says what fails and how often: how many times, in how many conversations, over which days. A failure whose code
// says the tool itself broke is a fix even when a retry worked. Its evidence is its conversations, newest first.
func TestDreamFixSaysWhatFailsAndHowOften(t *testing.T) {
	t.Parallel()
	drafts := DreamDrafts(nil, []Event{
		failure("computer_state", "internal", "{machine:str}", "s1", 50*time.Hour),
		failure("computer_state", "internal", "{machine:str}", "s1", 49*time.Hour),
		failure("computer_state", "internal", "{machine:str}", "s2", 26*time.Hour),
		failure("computer_state", "internal", "{machine:str}", "s3", time.Hour),
		cured(failure("agent_read", "internal", "{turns:num}", "s4", 2*time.Hour), "{}"),
		failure("agent_read", "internal", "{turns:num}", "s4", time.Hour),
	}, dreamNow)
	slices.SortFunc(drafts, func(a, b Draft) int { return strings.Compare(b.Tool, a.Tool) })
	if len(drafts) != 2 {
		t.Fatalf("drafts %+v", drafts)
	}
	state, read := drafts[0], drafts[1]
	want := "computer_state: {machine:str} fails with internal, 4 times in 3 conversations from 2026-11-26 to 2026-11-28 " +
		"(UTC); no retry with other arguments worked."
	if state.Kind != DraftFix || state.Text != want || state.Count != 4 || state.Runtime != "agy" ||
		!state.FirstAt.Equal(dreamNow.Add(-50*time.Hour)) || !state.LastAt.Equal(dreamNow.Add(-time.Hour)) {
		t.Errorf("computer_state's draft: %+v\nwant the text %q", state, want)
	}
	var sessions []string
	for _, e := range state.Evidence {
		sessions = append(sessions, e.Session)
	}
	if !slices.Equal(sessions, []string{"s3", "s2", "s1"}) || !state.Evidence[2].At.Equal(dreamNow.Add(-49*time.Hour)) {
		t.Errorf("evidence %+v", state.Evidence)
	}
	want = "agent_read: {turns:num} fails with internal, 2 times in 1 conversation on 2026-11-28 (UTC); the tool itself " +
		"failed, though a retry with {} worked."
	if read.Kind != DraftFix || read.Text != want {
		t.Errorf("agent_read's draft: %+v\nwant the text %q", read, want)
	}
}

// Each run merges into the stored drafts: a draft keeps its id and its review state, and gets the new count, last
// failure and evidence. A pending draft's kind and text follow its friction; a proposed, filed or dismissed one keeps
// what was reviewed, and a dismissed one is not reopened.
func TestDreamKeepsEachDraftsIDAndReviewState(t *testing.T) {
	t.Parallel()
	group := func(tool string) []Event {
		return []Event{failure(tool, "c", "{a:str}", "s1", 5*time.Hour), failure(tool, "c", "{a:str}", "s2", 4*time.Hour),
			failure(tool, "c", "{a:str}", "s3", 3*time.Hour)}
	}
	events := slices.Concat(group("proposed"), group("filed"), group("dismissed"), group("pending"))
	first := DreamDrafts(nil, events, dreamNow)
	reviewed := slices.Clone(first)
	for i := range reviewed {
		switch reviewed[i].Tool {
		case "proposed":
			reviewed[i].Status, reviewed[i].NoteID = DraftProposed, "tn_1"
		case "filed":
			reviewed[i].Status, reviewed[i].TodoID = DraftFiled, "td_1"
		case "dismissed":
			reviewed[i].Status = DraftDismissed
		}
	}
	later := dreamNow.Add(DreamEvery)
	for _, tool := range []string{"proposed", "filed", "dismissed", "pending"} {
		events = append(events, cured(Event{At: later.Add(-time.Hour), Tool: tool, Runtime: "agy", Code: "c",
			Skeleton: "{a:str}", Machine: "m", Session: "s9"}, "{b:num}"))
	}
	second := DreamDrafts(reviewed, events, later)
	if len(second) != len(first) {
		t.Fatalf("%d drafts after %d", len(second), len(first))
	}
	for i, d := range second {
		was := reviewed[i]
		if d.ID != was.ID || d.Status != was.Status || d.NoteID != was.NoteID || d.TodoID != was.TodoID {
			t.Errorf("%s lost its id or review state: %+v, was %+v", d.Tool, d, was)
		}
		if d.Count != 4 || !d.LastAt.Equal(later.Add(-time.Hour)) || d.Evidence[0].Session != "s9" || !d.UpdatedAt.Equal(later) {
			t.Errorf("%s was not brought up to date: %+v", d.Tool, d)
		}
		reviewedText := d.Kind == was.Kind && d.Text == was.Text
		if d.Tool == "pending" {
			if d.Kind != DraftNote || d.RetrySkeleton != "{b:num}" || !strings.Contains(d.Text, "what worked: {b:num}") {
				t.Errorf("the pending draft did not follow its friction: %+v", d)
			}
		} else if !reviewedText || d.Kind != DraftFix {
			t.Errorf("%s changed what was reviewed: %+v, was %+v", d.Tool, d, was)
		}
	}
	if again := DreamDrafts(second, events, later.Add(time.Minute)); !slices.EqualFunc(again, second, func(a, b Draft) bool {
		return a.ID == b.ID && a.UpdatedAt.Equal(b.UpdatedAt) && a.Text == b.Text
	}) {
		t.Errorf("the same friction again moved the drafts: %+v", again)
	}
}

// A draft names at most MaxEvidence conversations, the newest; a call that names none is no evidence.
func TestDreamEvidenceIsTheNewestConversations(t *testing.T) {
	t.Parallel()
	var events []Event
	for i := range 7 {
		events = append(events, failure("t", "c", "{}", fmt.Sprintf("s%d", i), time.Duration(10-i)*time.Hour))
	}
	events = append(events, failure("t", "c", "{}", "", time.Hour/2))
	stored := []Draft{{ID: draftID(groupKey{"t", "c", "{}"}), Tool: "t", Code: "c", Skeleton: "{}", Status: DraftPending,
		Evidence: []Evidence{{Machine: "m", Session: "s6", At: dreamNow.Add(-20 * time.Minute)}}}}
	d := DreamDrafts(stored, events, dreamNow)[0]
	var sessions []string
	for _, e := range d.Evidence {
		sessions = append(sessions, e.Session)
	}
	if !slices.Equal(sessions, []string{"s6", "s5", "s4", "s3", "s2"}) || !d.Evidence[0].At.Equal(dreamNow.Add(-20*time.Minute)) {
		t.Fatalf("evidence %+v", d.Evidence)
	}
	if !strings.Contains(d.Text, "8 times in 8 conversations") {
		t.Fatalf("text %q", d.Text)
	}
}

// A pending draft whose friction stopped a Window ago goes; a reviewed one stays, so a dismissal holds. Past MaxDrafts
// the drafts seen longest ago go.
func TestDreamLetsStoppedFrictionGo(t *testing.T) {
	t.Parallel()
	gone := dreamNow.Add(-Window - time.Hour)
	stored := []Draft{
		{ID: "fd_stopped", Tool: "a", Status: DraftPending, LastAt: gone},
		{ID: "fd_dismissed", Tool: "b", Status: DraftDismissed, LastAt: gone},
		{ID: "fd_recent", Tool: "c", Status: DraftPending, LastAt: dreamNow.Add(-time.Hour)},
	}
	var ids []string
	for _, d := range DreamDrafts(stored, nil, dreamNow) {
		ids = append(ids, d.ID)
	}
	if !slices.Equal(ids, []string{"fd_dismissed", "fd_recent"}) {
		t.Fatalf("kept %q", ids)
	}

	stored = nil
	for i := range MaxDrafts + 5 {
		stored = append(stored, Draft{ID: fmt.Sprintf("fd_%03d", i), Status: DraftFiled, LastAt: dreamNow.Add(time.Duration(i) * time.Minute)})
	}
	kept := DreamDrafts(stored, nil, dreamNow)
	if len(kept) != MaxDrafts || kept[0].ID != "fd_005" {
		t.Fatalf("%d kept, the first %s", len(kept), kept[0].ID)
	}
}

// The book's friction dream runs at most every DreamEvery, only when a failure came since its last run, and takes a
// failure in only once RetryWindow has passed; a restart remembers its last run.
func TestBookDreamRunsEveryFewHoursOnNewFriction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeHost()
	b := New(f.host())
	fail := func(b *Book, tool string) {
		for range draftMin {
			b.Record(Call{Tool: tool, Runtime: "agy", Machine: "m", Session: "s", Skeleton: "{}", Code: "c"})
		}
	}
	drafts := func() int {
		t.Helper()
		d, err := b.Drafts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(d)
	}
	dream := func(b *Book) {
		t.Helper()
		if err := b.Dream(ctx); err != nil {
			t.Fatal(err)
		}
	}
	fail(b, "a")
	dream(b)
	if n := drafts(); n != 0 {
		t.Fatalf("%d drafts from failures younger than RetryWindow", n)
	}
	f.advance(RetryWindow)
	if dream(b); drafts() != 1 {
		t.Fatalf("%d drafts, want a's", drafts())
	}
	fail(b, "b")
	f.advance(RetryWindow)
	if dream(b); drafts() != 1 {
		t.Fatalf("ran again within DreamEvery: %d drafts", drafts())
	}
	f.advance(DreamEvery)
	if dream(b); drafts() != 2 {
		t.Fatalf("%d drafts, want a's and b's", drafts())
	}
	f.drop(DraftsKey)
	f.advance(DreamEvery)
	if dream(b); f.has(DraftsKey) {
		t.Fatal("ran with no new failure")
	}
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	next := New(f.host())
	next.Load(ctx)
	if dream(next); f.has(DraftsKey) {
		t.Fatal("ran after a restart with no new failure")
	}
	fail(next, "c")
	f.advance(RetryWindow)
	if dream(next); drafts() != 3 {
		t.Fatalf("%d drafts after a new failure, want a's, b's and c's", drafts())
	}
}

func TestAbbreviate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		skeleton string
		depth    int
		want     string
	}{
		{"{steps:[{launch_app:{name:str}}]}", 3, "{steps:[{launch_app:…}]}"},
		{"{steps:[{launch_app:{name:str}}]}", 1, "{steps:…}"},
		{"{a:{b:{c:num}},d:[str|num]}", 1, "{a:…,d:…}"},
		{"{a:{b:{c:{d:{...}}}}}", skeletonDepth + 1, "{a:{b:{c:{d:{...}}}}}"},
		{"{}", 1, "{}"},
		{"invalid", 1, "invalid"},
	} {
		if got := abbreviate(tc.skeleton, tc.depth); got != tc.want {
			t.Errorf("abbreviate(%s, %d) = %s, want %s", tc.skeleton, tc.depth, got, tc.want)
		}
	}
}
