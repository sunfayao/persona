package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// setQuietForTest makes quietModeOn answer on without the store; nil goes back to reading it.
func (s *Service) setQuietForTest(on *bool) {
	s.pushQuiet.mu.Lock()
	s.pushQuiet.force = on
	s.pushQuiet.mu.Unlock()
}

func pushQuietFor(t *testing.T, s *Service, on bool) {
	t.Helper()
	s.setQuietForTest(&on)
	t.Cleanup(func() { s.setQuietForTest(nil) })
}

func TestEscalateLevel(t *testing.T) {
	for why, want := range map[string]string{"irreversible": "P0", "visible_to_others": "P0", "only_owner_knows": "P0",
		"priority": "P1", "acceptance": "P1", "": "P1"} {
		if got := escalateLevel(why, false); got != want {
			t.Errorf("%q: %s, want %s", why, got, want)
		}
		if got := escalateLevel(why, true); got != "P0" {
			t.Errorf("%q with p0: %s", why, got)
		}
	}
}

// With the switch on a supervised task's conclusion (task_review accepted) posts no card: the briefing and the board
// keep it.
func TestQuietConclusionPostsNoCard(t *testing.T) {
	s, f, todo, posts, _ := conclusionService(t, false)
	pushQuietFor(t, s, true)
	waitsForReview(t, s, f, todo.ID, "查了，对方接口不对外开放，直连做不了。")
	waitFor(t, "the review state", func() bool { return todoNow(s, todo.ID).Supervision.State == reviewPending })
	before := len(posts())
	if before != 0 {
		t.Fatalf("progress cards with the switch on:\n%s", strings.Join(posts(), "\n---\n"))
	}
	reviewTask(t, s, todo.ID, map[string]any{"verdict": "accepted", "outcome": "cannot", "reason": "对方接口不对外开放",
		"link": "https://docs.example.com/api"})
	waitFor(t, "the accepted conclusion", func() bool { a := askOf(s, todo.ID); return a != nil && a.Conclusion && a.NoCard })
	if after := posts(); len(after) != 0 {
		t.Fatalf("cards with the switch on:\n%s", strings.Join(after, "\n---\n"))
	}
	if a := askOf(s, todo.ID); a == nil || !a.Conclusion || !a.NoCard || a.MessageID != "" {
		t.Fatalf("ask %+v", a)
	}
	for _, p := range f.pushed() {
		t.Fatalf("pushed %+v", p)
	}
}

// With the switch on a "Please verify" ask posts no card and keeps waiting on the board and in the briefing.
func TestQuietPleaseVerifyPostsNoCard(t *testing.T) {
	s, f, todo, posts, _ := conclusionService(t, false)
	pushQuietFor(t, s, true)
	s.ownerVerifiesAll = true
	f.judge = `{"changed":"设置里多了开关","link":"https://hub.example.com/settings","how":"打开设置看开关"}`
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the ask", func() bool { a := askOf(s, todo.ID); return a != nil && !a.AskedAt.IsZero() })
	waitFor(t, "the cardless ask", func() bool { a := askOf(s, todo.ID); return a != nil && a.NoCard && a.MessageID == "" })
	if n := countAcceptCards(posts()); n != 0 {
		t.Fatalf("%d 请验收 cards", n)
	}
	if a := askOf(s, todo.ID); !a.NoCard || a.MessageID != "" {
		t.Fatalf("ask %+v", a)
	}
	ctx := context.Background()
	if _, total := s.snapshotUnaccepted(ctx, s.location(ctx)); total != 1 {
		t.Fatalf("%d waiting in the briefing, want 1", total)
	}
}

// With the switch off the same turn still posts its progress card (as before).
func TestQuietOffTaskReportStillPosts(t *testing.T) {
	s, f, posts := reportService(t, true)
	pushQuietFor(t, s, false)
	startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "修好了，https://github.com/o/r/pull/901 已合并", 0)
	waitFor(t, "the report", func() bool { return len(posts()) == 1 })
}

// With the switch on, a task's progress report posts nothing.
func TestQuietTaskReportPostsNothing(t *testing.T) {
	s, f, posts := reportService(t, true)
	pushQuietFor(t, s, true)
	startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "修好了，https://github.com/o/r/pull/901 已合并", 0)
	waitReportQueue(t, s)
	if n := len(posts()); n != 0 {
		t.Fatalf("%d progress cards with the switch on", n)
	}
}

// A plan waiting for the owner's Start is P1 (23:12 ruling): its push names no level, so the hub holds it for the
// briefing with the switch on; a task that failed to start is P1 as well.
func TestPlanWaitingForStartIsNotP0(t *testing.T) {
	ctx := context.Background()
	s, f := newTaskService(t)
	f.hello.Assistant.MachineID = "box"
	setSettings(t, s, func(v *Settings) { v.Main, v.MachineID = "main1", "box" })
	if _, err := s.toolTaskPlan(ctx, "side1", planArgs(false)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the push", func() bool { return len(f.pushed()) == 1 })
	if p := f.pushed()[0]; p.Level == assistantwire.LevelP0 || p.Kind != assistantwire.NotifyPush {
		t.Fatalf("plan push %+v", p)
	}
}

// The switch is the hub's hello: QuietPush on is on, an older hub that sends nothing is off.
func TestQuietModeFollowsTheHubsHello(t *testing.T) {
	s, f := newTaskService(t)
	ctx := context.Background()
	if s.quietModeOn(ctx) {
		t.Fatal("on without the hub saying so")
	}
	f.mu.Lock()
	f.hello.QuietPush = true
	f.mu.Unlock()
	if !s.quietModeOn(ctx) {
		t.Fatal("off with the hub's switch on")
	}
}

// A reply reminder is keyed by the question it waits on: the same stop of the same agent is one key, a new question
// (a later stop) another, so the hub never takes an agent's second question for a repeat of its first.
func TestReplyReminderKeyedByQuestion(t *testing.T) {
	at := time.Date(2026, 11, 14, 23, 0, 0, 0, time.UTC)
	a := replyRelatedKey(proto.Session{SessionID: "s1", UpdatedAt: at})
	if a != replyRelatedKey(proto.Session{SessionID: "s1", UpdatedAt: at}) {
		t.Fatal("one question, two keys")
	}
	if a == replyRelatedKey(proto.Session{SessionID: "s1", UpdatedAt: at.Add(time.Minute)}) {
		t.Fatal("two questions of one agent share a key")
	}
	if a == replyRelatedKey(proto.Session{SessionID: "s2", UpdatedAt: at}) {
		t.Fatal("two agents share a key")
	}
}
