package membership

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func rows() []Member {
	return []Member{
		{Machine: "m1", Session: "sup-aaaa", Source: SourceLeader, Active: true},
		{Machine: "m1", Session: "work-1111", Source: "manual", Active: true, Assignment: &Assignment{Title: "backend"}},
		{Machine: "m2", Session: "work-2222", Source: "manual", Active: true, Assignment: &Assignment{Title: "web", State: StateDone}},
		{Machine: "m2", Session: "old-3333", Source: "manual"},
	}
}

func TestTheSupervisorAgentAndReleasedRowsAreNoMembers(t *testing.T) {
	got := Working(rows())
	if len(got) != 2 || got[0].Session != "work-1111" || got[1].Session != "work-2222" {
		t.Fatalf("working members: %+v", got)
	}
}

func TestEverySignalKindChoosesItsScopeOnce(t *testing.T) {
	want := []string{"review", "owner_choice", "idle", "unjudged", "needs_you", "timed_out", "stalled", "no_progress", "all_stopped",
		"interrupted", "failed", "started", "pr_conflict", "draft_pr", "pr_event", "report", "escalated", "orphaned", "ask", "decide",
		"decision_reply", "check", "todo_ready", "pr_merged", "parent_closeout", "recheck", "undecidable", "owner_cannot_answer",
		"parked_due", "park_decided", "parked_summary", "needs_user_external_info", "inbox", "delivery_hold", "cardless_wait",
		"overdue", "estimate_due", "handoff_result"}
	got := Kinds()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the table lists %v, want %v", got, want)
	}
	session := []string{"idle", "needs_you", "stalled", "no_progress", "interrupted", "report", "ask", "estimate_due"}
	for _, kind := range want {
		if _, ok := ScopeOf(kind); !ok {
			t.Errorf("%s has no scope", kind)
		}
		if SessionBound(kind) != slices.Contains(session, kind) {
			t.Errorf("%s: session-bound %v", kind, SessionBound(kind))
		}
	}
	if _, ok := ScopeOf("a_new_kind"); ok {
		t.Error("an unknown kind has a scope")
	}
}

func TestASingleMemberKeepsTheIdItAlwaysHad(t *testing.T) {
	id := "todo_1|report:done"
	if got := SignalID(id, "report", "s1", false); got != id {
		t.Fatalf("one member: %q", got)
	}
	a, b := SignalID(id, "report", "s1", true), SignalID(id, "report", "s2", true)
	if a == b {
		t.Fatalf("several members: %q %q", a, b)
	}
	stalled := SignalID("todo_1|stalled", "stalled", "s1", true)
	if !OfKind(stalled, "todo_1", "stalled") || !OfKind("todo_1|stalled", "todo_1", "stalled") || OfKind(stalled, "todo_1", "review") ||
		OfKind(stalled, "todo_2", "stalled") {
		t.Fatalf("of kind: %q", stalled)
	}
	if got := SignalID("todo_1|review", "review", "s1", true); got != "todo_1|review" {
		t.Fatalf("a to-do's signal took a session: %q", got)
	}
	if got := SignalID(id, "report", "", true); got != id {
		t.Fatalf("a signal that names no session took one: %q", got)
	}
}

func TestAnAssignmentNamesCriteriaByNumberOrText(t *testing.T) {
	todo := []string{"tests pass", "PR merged", "screenshots"}
	a, err := NewAssignment(" web ", []string{"#3", "tests pass", "3"}, 20, todo)
	if err != nil || a.Title != "web" || !slices.Equal(a.Criteria, []string{"screenshots", "tests pass"}) || a.State != StateWorking {
		t.Fatalf("%+v %v", a, err)
	}
	if got := a.Numbers(todo); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("numbers %v", got)
	}
	for _, bad := range []struct {
		title  string
		crit   []string
		expect int
		want   string
	}{
		{"", nil, 0, "needs a title"},
		{"x", []string{"4"}, 0, "none of the to-do's"},
		{"x", []string{"nope"}, 0, "1. tests pass"},
		{"x", nil, -1, "expect_minutes"},
		{strings.Repeat("a", 61), nil, 0, "at most 60"},
	} {
		if _, err := NewAssignment(bad.title, bad.crit, bad.expect, todo); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestAMessageNeedsTheMemberNamedWhenThereAreSeveral(t *testing.T) {
	title := func(m Member) string { return "row-" + m.Session }
	if _, found, refusal := Pick(rows(), "", title); found || !strings.Contains(refusal, "work-111") || !strings.Contains(refusal, "(backend, working)") ||
		!strings.Contains(refusal, "(web, done)") || strings.Contains(refusal, "sup-aaaa") || !strings.Contains(refusal, "nothing was done") {
		t.Fatalf("unnamed: %v %q", found, refusal)
	}
	m, found, _ := Pick(rows(), "work-2", title)
	if !found || m.Session != "work-2222" {
		t.Fatalf("named by prefix: %+v", m)
	}
	if _, found, refusal := Pick(rows(), "work", title); found || !strings.Contains(refusal, "more than one") {
		t.Fatalf("ambiguous: %q", refusal)
	}
	if _, found, refusal := Pick(rows(), "zzzz", title); found || !strings.Contains(refusal, "none of the to-do's members") {
		t.Fatalf("unknown: %q", refusal)
	}
	one := rows()[:2]
	if m, found, refusal := Pick(one, "", title); !found || m.Session != "work-1111" || refusal != "" {
		t.Fatalf("one member: %+v %q", m, refusal)
	}
	if _, found, refusal := Pick(rows()[:1], "", title); found || refusal != "" {
		t.Fatalf("only a supervisor: %v %q", found, refusal)
	}
}

func TestOthersAreThoseStillGoing(t *testing.T) {
	if n := Others(rows(), "m1", "work-1111", nil); n != 0 { // work-2222 reported its part done
		t.Fatalf("others %d", n)
	}
	r := rows()
	r[2].Assignment.State = ""
	if n := Others(r, "m1", "work-1111", nil); n != 1 {
		t.Fatalf("others %d", n)
	}
	if n := Others(r, "m1", "work-1111", func(Member) bool { return false }); n != 0 {
		t.Fatalf("others, none alive: %d", n)
	}
}

func TestDetachMoveAndAttachRules(t *testing.T) {
	two := View{ID: "a", Open: true, Supervised: true, InProgress: true, Members: rows()}
	one := View{ID: "b", Open: true, Supervised: true, InProgress: true, Members: rows()[:2]}
	other := View{ID: "c", Open: true, Supervised: true, InProgress: true}
	cases := []struct {
		name   string
		action Action
		agent  string
		target View
		from   *View
		want   string // "" allowed
	}{
		{"detach one of two", Detach, "work-2222", two, nil, ""},
		{"detach the last of a to-do in progress", Detach, "work-1111", one, nil, "last Agent working"},
		{"detach the last of a to-do waiting for review", Detach, "work-1111", func() View { v := one; v.InProgress = false; return v }(), nil, ""},
		{"detach a stranger", Detach, "nobody", two, nil, "no working member"},
		{"detach the supervisor", Detach, "sup-aaaa", two, nil, "no working member"},
		{"attach a new one", Attach, "new-4444", two, nil, ""},
		{"attach a member again", Attach, "work-1111", two, nil, "member of to-do a already"},
		{"attach to a closed to-do", Attach, "new-4444", View{ID: "z", Supervised: true}, nil, "closed"},
		{"attach to one not supervised", Attach, "new-4444", View{ID: "z", Open: true}, nil, "not supervised from here"},
		{"move to a to-do", Move, "work-2222", other, &two, ""},
		{"move the last one", Move, "work-1111", other, &one, "last Agent working"},
		{"move from a to-do not supervised", Move, "work-2222", other, func() *View { v := two; v.Supervised = false; return &v }(), "not supervised from here"},
		{"move to the same", Move, "work-2222", two, &two, "move takes"},
		{"move with no source", Move, "work-2222", other, nil, "move takes"},
	}
	for _, c := range cases {
		m := "m1"
		if c.agent == "work-2222" {
			m = "m2"
		}
		err := Check(c.action, m, c.agent, c.target, c.from)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	full := View{ID: "f", Open: true, Supervised: true}
	for i := range Most {
		full.Members = append(full.Members, Member{Machine: "m", Session: string(rune('a'+i)) + "-session", Active: true, Source: "manual"})
	}
	if err := Check(Attach, "m", "ninth", full, nil); err == nil || !strings.Contains(err.Error(), "the most") {
		t.Errorf("the bound: %v", err)
	}
}

func TestTheBoundCountsMembers(t *testing.T) {
	if got := Load([]int{0, 2, 1}, 3); got != 3+1+2+1 {
		t.Fatalf("load %d", got)
	}
}

func TestAMemberHasItsOwnClockOnlyWhenItHasAnEstimate(t *testing.T) {
	at := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	m := Member{LinkedAt: at, Active: true, Session: "s", Assignment: &Assignment{Title: "x", ExpectMinutes: 30}}
	if !m.OwnClock() || !m.DueAt().Equal(at.Add(30*time.Minute)) || m.Expect(90) != 30 {
		t.Fatalf("own clock: %v %v", m.OwnClock(), m.DueAt())
	}
	m.Assignment.State = StateDone
	if !m.DueAt().IsZero() {
		t.Fatal("a member that reported done is still due")
	}
	plain := Member{LinkedAt: at, Active: true, Session: "s"}
	if plain.OwnClock() || !plain.DueAt().IsZero() || plain.Expect(90) != 90 {
		t.Fatal("a member with no assignment has the to-do's clock")
	}
}

func TestParseAction(t *testing.T) {
	for _, ok := range []string{"", "attach", "detach", "move"} {
		if _, err := ParseAction(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	if _, err := ParseAction("remove"); err == nil {
		t.Error("an unknown action")
	}
}
