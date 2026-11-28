package assistant

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// One entry per completion: the done report rides on the review of its to-do, so its words and evidence and the
// criteria are told once; the folded report is not an entry of its own.
func TestDoneReportFoldsIntoItsReview(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	holdDigest(s)
	ctx := context.Background()
	presenceTimes(t, 0, 0)
	ownerTurnTimes(t, 0, 0)
	const said = "PR merged, screenshots attached."
	waitsForReview(t, s, f, todo.ID, said)
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	review := superviseSignal{Todo: todo.ID, Kind: sigReview, Key: "review@1", At: time.Now().Add(-time.Minute)}
	report := superviseSignal{Todo: todo.ID, Kind: sigReport, Report: upReportDone, Key: "done", Detail: said,
		Evidence: []string{"https://example.test/pr/7"}, At: time.Now().Add(-time.Minute)}
	list := s.foldDoneReports([]superviseSignal{report, review})
	if len(list) != 1 || list[0].Kind != sigReview || list[0].ReportSaid != said {
		t.Fatalf("not folded: %+v", list)
	}
	before := len(f.notices())
	if !s.deliverDigest(ctx, s.settings(ctx), "conv1", list, "", time.Now(), time.Time{}) {
		t.Fatal("not delivered")
	}
	waitFor(t, "the message", func() bool { return len(f.notices()) > before })
	text := f.notices()[before]
	for what, n := range map[string]int{"signal=review": 1, "signal=report": 0, "Acceptance criteria:": 1, "Evidence it gave": 1,
		"https://example.test/pr/7": 1, "It said:": 1, said: 1} {
		if got := strings.Count(text, what); got != n {
			t.Errorf("%q appears %d times, want %d:\n%s", what, got, n, text)
		}
	}
}

// A done report told earlier is not told again by the review that follows it.
func TestReviewDoesNotRepeatAToldDoneReport(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	holdDigest(s)
	ctx := context.Background()
	const said = "All criteria met; the PR is merged."
	waitsForReview(t, s, f, todo.ID, said)
	review := superviseSignal{Todo: todo.ID, Kind: sigReview, Key: "review@1"}
	if e, ok := s.digestEntry(ctx, review, ""); !ok || !strings.Contains(e.Last, "PR is merged") {
		t.Fatalf("without the report told: %+v, %v", e, ok)
	}
	s.doneReportsTold([]superviseSignal{{Todo: todo.ID, Kind: sigReport, Report: upReportDone, Detail: said}})
	if e, ok := s.digestEntry(ctx, review, ""); !ok || e.Last != "" {
		t.Fatalf("the told report is repeated: %+v, %v", e, ok)
	}
}

// A report no longer needs telling once its supervisor reviewed the task after it.
func TestReportExpiresWhenItsSupervisorReviewed(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	holdDigest(s)
	ctx := context.Background()
	waitsForReview(t, s, f, todo.ID, "done")
	reportedAt := time.Now().Add(-time.Minute)
	for _, kind := range []string{upReportQuestion, upReportBlocked, upReportDone} {
		g := superviseSignal{Todo: todo.ID, Kind: sigReport, Report: kind, Key: kind, Detail: "words", At: reportedAt}
		if _, ok := s.digestEntry(ctx, g, ""); !ok {
			t.Fatalf("%s report dropped before any review", kind)
		}
	}
	for _, at := range []time.Time{reportedAt.Add(-time.Hour), time.Now()} { // a review before the report does not answer it
		if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool {
			v.Reviews = append(v.Reviews, TaskReview{At: at, Verdict: reviewRework})
			return true
		}); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{upReportQuestion, upReportBlocked, upReportDone} {
			g := superviseSignal{Todo: todo.ID, Kind: sigReport, Report: kind, Key: kind, Detail: "words", At: reportedAt}
			_, ok := s.digestEntry(ctx, g, "")
			if want := at.Before(reportedAt); ok != want {
				t.Errorf("%s report with a review at %s: kept %v, want %v", kind, at.Format(time.TimeOnly), ok, want)
			}
		}
	}
}

// The card-less clock counts from the review's delivery: a review raised a second ago, unread, is not late, however
// old its round is; once it was told, ten minutes after that.
func TestCardlessWaitCountsFromTheReviewDelivery(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	holdDigest(s)
	ctx := context.Background()
	waitsForReview(t, s, f, todo.ID, "done")
	long := time.Now().Add(-time.Hour)
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool { v.PendingSince = &long; return true }); err != nil {
		t.Fatal(err)
	}
	s.superviseWatch(ctx, time.Now().Add(time.Second))
	if slices.Contains(pendingKinds(s, todo.ID), sigCardlessWait) {
		t.Fatalf("raised while its review was unread: %v", pendingKinds(s, todo.ID))
	}
	reviewToldAlready(t, s, f)
	s.superviseWatch(ctx, time.Now().Add(cardlessSettle-time.Minute))
	if slices.Contains(pendingKinds(s, todo.ID), sigCardlessWait) {
		t.Fatal("raised before ten minutes passed since the delivery")
	}
	s.superviseWatch(ctx, time.Now().Add(cardlessSettle+time.Minute))
	if !slices.Contains(pendingKinds(s, todo.ID), sigCardlessWait) {
		t.Fatalf("not raised ten minutes after the delivery: %v", pendingKinds(s, todo.ID))
	}
}
