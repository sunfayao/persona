package criteria

import (
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	cur := []string{"a", "b", "c"}
	got, err := Apply(cur, Ops{Drop: []int{1}, Edit: []Edit{{Number: 3, Text: "c2"}}, Add: []string{"d"}})
	if err != nil || strings.Join(got.Next, ",") != "b,c2,d" || len(got.Dropped) != 1 || len(got.Edited) != 1 || got.Edited[0].From != "c" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []Ops{{Drop: []int{1, 2, 3}}, {Drop: []int{1}, Edit: []Edit{{Number: 1, Text: "z"}}},
		{Edit: []Edit{{Number: 1, Text: "b"}}}, {Add: []string{"1", "2", "3", "4"}}, {Drop: []int{0}}, {Add: []string{strings.Repeat("x", 201)}}} {
		if _, err := Apply(cur, bad); err == nil {
			t.Fatalf("applied %+v", bad)
		}
	}
	if same, err := Apply(cur, Ops{Edit: []Edit{{Number: 2, Text: "b"}}}); err != nil || len(same.Edited) != 0 {
		t.Fatalf("an edit to the same text is no change: %+v %v", same, err)
	}
}

func TestResolveAndComplete(t *testing.T) {
	list := []string{"build", "test"}
	checks, byNumber, err := ResolveChecks(list, []Check{{Number: 2, Met: true, Evidence: "ok"}, {Number: 1, Criterion: "build", Met: true, Evidence: "ok"}})
	if err != nil || !byNumber || checks[0].Criterion != "test" || checks[1].Number != 0 || Complete(list, checks) != nil {
		t.Fatalf("%+v %v %v", checks, byNumber, err)
	}
	for _, bad := range []Check{{Number: 3}, {Number: 1, Criterion: "test"}, {Number: -1}} {
		if _, _, err := ResolveChecks(list, []Check{bad}); err == nil {
			t.Fatalf("resolved %+v", bad)
		}
	}
	if err := Complete(list, checks[:1]); err == nil {
		t.Fatal("a criterion without a check")
	}
}

func TestTooLongAndFit(t *testing.T) {
	if err := TooLong("done_when", []string{strings.Repeat("字", 200)}); err != nil {
		t.Fatal(err)
	}
	if err := TooLong("done_when", []string{"ok", strings.Repeat("字", 201)}); err == nil || !strings.Contains(err.Error(), "item 2 is 201") {
		t.Fatalf("%v", err)
	}
	legacy := strings.Repeat("y", 200) + "…"
	if err := TooLong("done_when", []string{legacy}); err != nil {
		t.Fatalf("a stored criterion of the older clipped shape was refused: %v", err)
	}
	long := strings.Repeat("字", 230)
	if err := TooLongNew("done_when", []string{"new", long}, []string{long}); err != nil {
		t.Fatalf("an item already in the current list, unchanged, was refused: %v", err)
	}
	if err := TooLongNew("done_when", []string{long + "!"}, []string{long}); err == nil {
		t.Fatal("a changed over-long item must be refused")
	}
	if err := TooLong("done_when", []string{strings.Repeat("y", 201)}); err == nil {
		t.Fatal("a new 201-character criterion without the clip mark must be refused")
	}
	if err := TooLong("done_when", []string{strings.Repeat("y", 201) + "…"}); err == nil {
		t.Fatal("202 characters must be refused")
	}
	if Fit(legacy) != legacy || len([]rune(Fit(strings.Repeat("y", 300)))) != 201 {
		t.Fatal("Fit")
	}
	if got := Numbered([]string{"a", "b"}); got != "1. a\n2. b" {
		t.Fatalf("%q", got)
	}
}

func TestGuardReadsWorkerStatusNotCriteria(t *testing.T) {
	if got := Guard("- Status: blocked\n"); len(got) != 1 || got[0] != SignalCloseout {
		t.Fatalf("%v", got)
	}
	if got := Guard("仅当督办判定需要用户决定"); len(got) != 1 || got[0] != SignalOwnerOnly {
		t.Fatalf("the patterns still match such wording: %v", got)
	}
	if got := Guard("- Status: done\nconclusive"); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}
