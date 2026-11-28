package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/taskleader"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A routine digest waits in the hub's relay queue while its conversation works, and what it says is decided when it
// reaches the head of the queue (queuedDigestFixture, drainQueuedDigest).
func TestRoutineDigestQueuesWhileTheConversationWorks(t *testing.T) {
	s, f, _ := queuedDigestFixture(t, "Routine example")
	if q := queuedRelays(s, "conv1"); len(q) != 1 || q[0].SentUp || q[0].nativeFollowUp() || f.sentTo("conv1") != 0 {
		t.Fatalf("queued %+v, %d sent", q, f.sentTo("conv1"))
	}
	drainQueuedDigest(t, s, f)
	if len(f.notices()) != 1 || s.queuedFor("conv1") != 0 {
		t.Fatalf("after the turn: sends=%q queue=%d", f.notices(), s.queuedFor("conv1"))
	}
}

// A routine digest also waits out the owner's presence in an idle conversation.
func TestRoutineDigestWaitsForTheOwnerPresence(t *testing.T) {
	presenceTimes(t, time.Hour, time.Hour)
	s, f := newQuietService(t)
	stopHolds(t, s)
	ctx := context.Background()
	todo, err := s.createTodo(ctx, "Present example", "Check the example", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	s.superviseTodo(ctx, todo.ID, "conv1", []string{"Verified"}, 0)
	idle := proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle}
	f.setSessions("server", idle)
	s.UserSpoke(ctx, SessionRef{MachineID: "server", SessionID: "conv1"})
	g := superviseSignal{Todo: todo.ID, Kind: sigInterrupted, Key: "interrupted", Detail: "Capacity unavailable"}
	if !s.deliverDigest(ctx, s.settings(ctx), "conv1", []superviseSignal{g}, "", time.Now(), time.Time{}) {
		t.Fatal("digest not queued")
	}
	s.drainRelays("server", []proto.Session{idle})
	if s.queuedFor("conv1") != 1 || f.sentTo("conv1") != 0 {
		t.Fatalf("held for the owner: %d queued, %d sent", s.queuedFor("conv1"), f.sentTo("conv1"))
	}
}

// Two routine digests waiting for the same conversation become one message with both tasks.
func TestWaitingRoutineDigestsMerge(t *testing.T) {
	s, _, _ := queuedDigestFixture(t, "First example")
	ctx := context.Background()
	other, err := s.createTodo(ctx, "Second example", "Check the example", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	s.superviseTodo(ctx, other.ID, "conv1", []string{"Verified"}, 0)
	g := superviseSignal{Todo: other.ID, Kind: sigInterrupted, Key: "interrupted", Detail: "Capacity unavailable"}
	if !s.deliverDigest(ctx, s.settings(ctx), "conv1", []superviseSignal{g}, "", time.Now(), time.Time{}) {
		t.Fatal("second digest not accepted")
	}
	q := queuedRelays(s, "conv1")
	if len(q) != 1 || !strings.Contains(q[0].Text, "First example") || !strings.Contains(q[0].Text, "Second example") {
		t.Fatalf("not merged: %d queued, %+v", len(q), q)
	}
}

// A supervisor Agent's completion and its decisions go into the running conversation's input queue at once, and a
// digest that carries one goes with the routine entries beside it.
func TestSupervisorAgentCompletionGoesInWhileTheConversationWorks(t *testing.T) {
	s, f := newQuietService(t)
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusWorking})
	ctx := context.Background()
	todo, err := s.createTodo(ctx, "Led example", "Check the example", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	s.superviseTodo(ctx, todo.ID, "conv1", []string{"Verified"}, 0)
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool { v.State = reviewPending; return true }); err != nil {
		t.Fatal(err)
	}
	g := superviseSignal{Todo: todo.ID, Kind: sigReview, Report: taskleader.ReportCompletion, Key: "completion"}
	if !s.deliverDigest(ctx, s.settings(ctx), "conv1", []superviseSignal{g}, "", time.Now(), time.Time{}) {
		t.Fatal("completion not delivered")
	}
	if f.sentTo("conv1") != 1 || s.queuedFor("conv1") != 0 {
		t.Fatalf("completion: %d sent, %d queued", f.sentTo("conv1"), s.queuedFor("conv1"))
	}
}

// The kinds a supervisor Agent sends up are the ones the digest recognises; the others are routine.
func TestSentUpSignals(t *testing.T) {
	for _, tc := range []struct {
		g    superviseSignal
		want bool
	}{
		{superviseSignal{Kind: sigReview, Report: taskleader.ReportCompletion}, true},
		{superviseSignal{Kind: sigDecide}, true},
		{superviseSignal{Kind: sigEscalated}, true},
		{superviseSignal{Kind: sigReview}, false},
		{superviseSignal{Kind: sigReport, Report: upReportDone}, false},
		{superviseSignal{Kind: sigStalled}, false},
	} {
		if got := sentUp([]superviseSignal{tc.g}); got != tc.want {
			t.Errorf("%s/%s: sent up %v, want %v", tc.g.Kind, tc.g.Report, got, tc.want)
		}
	}
}
