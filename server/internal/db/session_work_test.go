package db

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Work-event evidence has no todo foreign key, so truncating todos cannot
// remove it. Every fixture must start without a previous test's decisions.
func TestTruncateForTestClearsDetachedWorkEvents(t *testing.T) {
	d, ctx := open(t), context.Background()
	at := time.Now().UTC()
	if err := d.AddWorkEvent(ctx, WorkEvent{At: at, Kind: "owner_decided", Detail: map[string]any{"decision_id": "synthetic-detached"}}); err != nil {
		t.Fatal(err)
	}
	if events, err := d.WorkEvents(ctx, at.Add(-time.Second)); err != nil || len(events) != 1 {
		t.Fatalf("fixture event missing: %+v %v", events, err)
	}
	if err := d.TruncateForTest(ctx); err != nil {
		t.Fatal(err)
	}
	if events, err := d.WorkEvents(ctx, time.Time{}); err != nil || len(events) != 0 {
		t.Fatalf("detached events survived fixture cleanup: %+v %v", events, err)
	}
}

func TestSessionWorkRestoresOldModelRetry(t *testing.T) {
	d, ctx := open(t), context.Background()
	_, _ = d.pool.Exec(ctx, `TRUNCATE session_work, session_work_events`)
	now := time.Now().UTC().Truncate(time.Millisecond)
	wake := now.Add(time.Minute)
	w := SessionWork{MachineID: "box", SessionID: "retry", TurnAt: now.Add(-40 * 24 * time.Hour), WakeAt: &wake,
		Work: proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitTransient}, UpdatedAt: now}
	if err := d.PutSessionWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := d.PruneSessionWork(ctx, now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := d.SessionWorks(ctx, now.Add(-48*time.Hour))
	if err != nil || len(got) != 1 || got[0].WakeAt == nil {
		t.Fatalf("old pending model retry lost: %+v, %v", got, err)
	}
}

func TestSessionWork(t *testing.T) {
	d, ctx := open(t), context.Background()
	_, _ = d.pool.Exec(ctx, `TRUNCATE session_work, session_work_events`)
	turn := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	wake := turn.Add(2 * time.Hour)
	w := SessionWork{MachineID: "server", SessionID: "s", TurnAt: turn, WakeAt: &wake, Retries: 1, InteractionID: "work_1",
		Work: proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Line: "等部署", WakeAt: &wake}, UpdatedAt: turn}
	if err := d.PutSessionWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	w.Wakes, w.WakeAt = 2, nil // an update replaces the row
	if err := d.PutSessionWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	old := w
	old.SessionID, old.TurnAt = "old", turn.Add(-40*24*time.Hour)
	_ = d.PutSessionWork(ctx, old)
	got, err := d.SessionWorks(ctx, turn.Add(-time.Minute))
	if err != nil || len(got) != 1 || got[0].Wakes != 2 || got[0].WakeAt != nil || got[0].Work.Line != "等部署" ||
		got[0].InteractionID != "work_1" || !got[0].TurnAt.Equal(turn) {
		t.Fatalf("works = %+v, %v", got, err)
	}

	_ = d.AddWorkEvent(ctx, WorkEvent{At: turn, MachineID: "server", SessionID: "s", Kind: "triaged", Detail: map[string]any{"phase": "done"}})
	_ = d.AddWorkEvent(ctx, WorkEvent{At: turn.Add(-40 * 24 * time.Hour), Kind: "sample"})
	if err := d.PruneSessionWork(ctx, turn.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ev, err := d.WorkEvents(ctx, turn.Add(-60*24*time.Hour))
	if err != nil || len(ev) != 1 || ev[0].Kind != "triaged" || ev[0].Detail["phase"] != "done" {
		t.Fatalf("events = %+v, %v", ev, err)
	}
	if all, _ := d.SessionWorks(ctx, time.Time{}); len(all) != 1 {
		t.Fatalf("pruned works = %+v", all)
	}
}

// Older active CI outbox entries survive a manager restart; ordinary old turns
// and cancelled watches are not restored by the recent-work query.
func TestSessionWorkRestoresOldCI(t *testing.T) {
	d, ctx := open(t), context.Background()
	_, _ = d.pool.Exec(ctx, `TRUNCATE session_work, session_work_events`)
	now := time.Now().UTC().Truncate(time.Millisecond)
	wake := now.Add(time.Minute)
	old := SessionWork{MachineID: "box", SessionID: "active", TurnAt: now.Add(-72 * time.Hour), WakeAt: &wake, UpdatedAt: now,
		Work: proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, CI: &proto.WorkCI{Number: 12, Message: "CI finished"}}}
	if err := d.PutSessionWork(ctx, old); err != nil {
		t.Fatal(err)
	}
	cancelled := old
	cancelled.SessionID = "cancelled"
	cancelled.WakeAt = nil
	if err := d.PutSessionWork(ctx, cancelled); err != nil {
		t.Fatal(err)
	}
	ordinary := old
	ordinary.SessionID = "ordinary"
	ordinary.Work.CI = nil
	if err := d.PutSessionWork(ctx, ordinary); err != nil {
		t.Fatal(err)
	}
	got, err := d.SessionWorks(ctx, now.Add(-48*time.Hour))
	if err != nil || len(got) != 1 || got[0].SessionID != "active" || got[0].Work.CI == nil || got[0].Work.CI.Message != "CI finished" {
		t.Fatalf("restored %+v: %v", got, err)
	}
	ancient := old
	ancient.SessionID, ancient.TurnAt = "ancient", now.Add(-40*24*time.Hour)
	if err := d.PutSessionWork(ctx, ancient); err != nil {
		t.Fatal(err)
	}
	cancelled.TurnAt = ancient.TurnAt
	if err := d.PutSessionWork(ctx, cancelled); err != nil {
		t.Fatal(err)
	}
	retired := cancelled
	retired.SessionID, retired.UpdatedAt = "retired", now
	retired.Work = proto.WorkState{RetiredAt: &now}
	if err := d.PutSessionWork(ctx, retired); err != nil {
		t.Fatal(err)
	}
	if err := d.PruneSessionWork(ctx, now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err = d.SessionWorks(ctx, now.Add(-48*time.Hour))
	if err != nil || len(got) != 3 {
		t.Fatalf("active CI watches and retirement not retained: %+v, %v", got, err)
	}
}

func TestLifecycleCommitReplaysWithOneRevisionAndRecordsMissingRanges(t *testing.T) {
	ctx, d := context.Background(), open(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	machine, session, todo := "lifecycle-machine", "lifecycle-session", "lifecycle-todo"
	if err := d.CreateTodo(ctx, TodoRow{ID: todo, Title: "Trace lifecycle", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LinkTodoAgentWithSource(ctx, todo, machine, session, "", "", "", "", "spawned", "another/session", at); err != nil {
		t.Fatal(err)
	}
	event := proto.LifecycleObservation{Session: session, Kind: proto.LifecycleTurnStarted, At: at,
		Source: "hook", Ref: proto.LifecycleRef("attempt-1")}
	reset := proto.LifecycleBatchRequest{}
	batch := proto.LifecycleBatch{Stream: proto.LifecycleRef("stream-one"), First: 1, Events: []proto.LifecycleObservation{event}}
	invalid := event
	invalid.Session = "invalid\x00value"
	bad := batch
	bad.Events = []proto.LifecycleObservation{event, invalid}
	if err := d.CommitLifecycle(ctx, machine, reset, bad, at); err == nil {
		t.Fatal("invalid record committed")
	}
	if cursor, err := d.LifecycleCursor(ctx, machine); err != nil || cursor != reset {
		t.Fatalf("failed batch changed cursor to %+v: %v", cursor, err)
	}
	page := func(after int64, limit int, forward bool) proto.LifecyclePage {
		t.Helper()
		got, err := d.LifecyclePage(ctx, "", "", todo, after, forward, limit, at.Add(-time.Second))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if p := page(0, 10, true); len(p.Events) != 1 || p.Events[0].Kind != proto.LifecycleChildLinked {
		t.Fatalf("business transition and lifecycle rows were changed by the failed batch: %+v", p)
	}
	if err := d.CommitLifecycle(ctx, machine, reset, batch, at); err != nil {
		t.Fatal(err)
	}
	cursor := proto.LifecycleBatchRequest{Stream: batch.Stream, After: 1}
	if err := d.CommitLifecycle(ctx, machine, cursor, batch, at); err != nil {
		t.Fatal(err)
	}
	batch.First = 2 // the same source observation in a different delivery position
	if err := d.CommitLifecycle(ctx, machine, cursor, batch, at); err != nil {
		t.Fatal(err)
	}
	first := page(0, 2, true)
	if len(first.Events) != 2 || first.Events[0].Kind != proto.LifecycleChildLinked || first.Events[1].Kind != proto.LifecycleTurnStarted {
		t.Fatalf("expected one source observation after replay: %+v", first)
	}
	ended := event
	ended.Kind, ended.Ref = proto.LifecycleTurnFinished, proto.LifecycleRef("stop-1")
	batch.First, batch.Events = 5, []proto.LifecycleObservation{ended}
	cursor.After = 2
	if err := d.CommitLifecycle(ctx, machine, cursor, batch, at); err != nil {
		t.Fatal(err)
	}
	got := page(first.Next, 2, true)
	if len(got.Events) != 2 || got.Events[0].Kind != proto.LifecycleGap ||
		got.Events[0].Outcome != "queue_overflow" || got.Events[1].Kind != proto.LifecycleTurnFinished ||
		got.Events[1].Revision <= got.Events[0].Revision {
		t.Fatalf("missing range or unstable order: %+v", got)
	}
	cursor.After = 5
	batch.Stream, batch.First, batch.Events = proto.LifecycleRef("stream-two"), 1, nil
	if err := d.CommitLifecycle(ctx, machine, cursor, batch, at); err != nil {
		t.Fatal(err)
	}
	last := page(0, 2, false)
	if len(last.Events) != 2 || last.Events[0].Outcome != "journal_reset" || last.Next != last.Events[1].Revision {
		t.Fatalf("reset or backwards cursor missing: %+v", last)
	}
	sessionPage, err := d.LifecyclePage(ctx, machine, session, "", 0, true, 10, at.Add(-time.Second))
	if err != nil || len(sessionPage.Events) != 5 {
		t.Fatalf("session events: %+v: %v", sessionPage, err)
	}
}

func TestLifecyclePersistsOnlyInteractionReferencesAndPrunesByOccurrence(t *testing.T) {
	ctx, d := context.Background(), open(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	machine, session := "wait-machine", "wait-session"
	ix := proto.Interaction{ID: "ix_wait_example", MachineID: machine, SessionID: session, Kind: "question", CreatedAt: at}
	for range 2 {
		if err := d.InsertInteraction(ctx, ix); err != nil {
			t.Fatal(err)
		}
		if err := d.FinishInteraction(ctx, ix.ID, "answered", &proto.Resolution{Answers: map[string]string{"answer": "fixture-private-body"}}); err != nil {
			t.Fatal(err)
		}
	}
	old := proto.LifecycleObservation{Session: session, Kind: proto.LifecycleTurnStarted, At: at.Add(-31 * 24 * time.Hour),
		Source: "hook", Ref: proto.LifecycleRef("old-attempt")}
	if err := d.AppendLifecycle(ctx, machine, []proto.LifecycleObservation{old}); err != nil {
		t.Fatal(err)
	}
	if err := d.PruneLifecycle(ctx, at.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	page, err := d.LifecyclePage(ctx, machine, session, "", 0, true, 10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(page.Events); !slices.EqualFunc(page.Events, []string{proto.LifecycleWaitOpened, proto.LifecycleWaitResolved},
		func(e proto.LifecycleEvent, kind string) bool { return e.Kind == kind && e.Ref == ix.ID }) ||
		page.Events[1].Outcome != "answered" || strings.Contains(got, "fixture-private-body") {
		t.Fatalf("unexpected wait metadata: %+v", page.Events)
	}
}

func BenchmarkLifecycleBatch(b *testing.B) {
	ctx, d := context.Background(), open(b)
	at := time.Now().UTC()
	events := make([]proto.LifecycleObservation, proto.LifecycleBatchSize)
	b.ResetTimer()
	for i := range b.N {
		for j := range events {
			events[j] = proto.LifecycleObservation{Session: "benchmark-session", Kind: proto.LifecycleTurnStarted, At: at,
				Source: "hook", Ref: proto.LifecycleRef(strconv.Itoa(i), strconv.Itoa(j))}
		}
		if err := d.AppendLifecycle(ctx, "benchmark-machine", events); err != nil {
			b.Fatal(err)
		}
	}
}

func TestLifecycleActivityRoundTripAndRowsWithoutIt(t *testing.T) {
	ctx, d := context.Background(), open(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	machine, session, todo := "lifecycle-activity-machine", "lifecycle-activity-session", "lifecycle-activity-todo"
	if err := d.CreateTodo(ctx, TodoRow{ID: todo, Title: "Trace activity", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LinkTodoAgentWithSource(ctx, todo, machine, session, "", "", "", "", "spawned", "another/session", at.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	turn := proto.LifecycleRef("turn-1")
	classified := proto.LifecycleObservation{Session: session, Kind: proto.LifecycleBackgroundStarted, At: at, Source: "inventory",
		Ref: proto.LifecycleRef("task-1"), Turn: turn, Activity: "test"}
	older := proto.LifecycleObservation{Session: session, Kind: proto.LifecycleBackgroundStarted, At: at, Source: "inventory",
		Ref: proto.LifecycleRef("task-2")} // as an older node wrote it: neither field
	if err := d.AppendLifecycle(ctx, machine, []proto.LifecycleObservation{classified, older}); err != nil {
		t.Fatal(err)
	}
	var activity *string
	if err := d.pool.QueryRow(ctx, "SELECT activity FROM agent_lifecycle_events WHERE machine_id = $1 AND source_ref = $2",
		machine, older.Ref).Scan(&activity); err != nil || activity != nil {
		t.Fatalf("a row without a class must keep NULL: %v, %v", activity, err)
	}
	p, err := d.LifecyclePage(ctx, "", "", todo, 0, true, 10, at.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]proto.LifecycleEvent{}
	for _, e := range p.Events {
		byRef[e.Ref] = e
	}
	if e := byRef[classified.Ref]; e.Activity != "test" || e.Turn != turn {
		t.Fatalf("class and turn did not round trip: %+v", e)
	}
	if e, ok := byRef[older.Ref]; !ok || e.Activity != "" || e.Turn != "" {
		t.Fatalf("a row without the fields must still read: %+v", e)
	}
}
