package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// startJudgeRecovery takes one closed agent turn through the supervision sweep without a separate turn-ended event.
func startJudgeRecovery(t *testing.T, s *Service, f *fakeHub, todo Todo, reply string) {
	t.Helper()
	f.pageTurns = true
	f.setItems(turnPage("turn-1", reply, true)...)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, reply, 0)
	waitFor(t, "the initial judgment to start", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.judged > 0
	})
	turnsQuiet(t, s, todo.ID)
}

// A request failure that recovers before the deferred retry produces no unjudged signal. The retry reads the reply as
// it is then, rather than judging the snapshot that failed earlier.
func TestSupervisedJudgeRetryUsesLatestReply(t *testing.T) {
	fastJudgeRetry(t)
	shortTurnRetries(t, 0)
	s, f, todo := newSupervisedService(t)
	f.judgeFails = 2 // both calls of the initial pass fail; the deferred pass can answer
	startJudgeRecovery(t, s, f, todo, "the earlier reply")
	if got := stopSignals(s, todo.ID); got != "" {
		t.Fatalf("the failed initial pass told %q before its deferred retry", got)
	}
	row := turnRow(proto.StatusDone, 4, "the latest reply")
	f.setItems(turnPage("turn-1", "the latest reply", true)...)
	f.setSessions("box", row)
	s.tasks.mu.Lock()
	s.tasks.working[todo.ID] = time.Now() // a start heard while the retry was pending: judge that stretch, not e.Work
	s.tasks.mu.Unlock()
	s.watchTurn(context.Background(), todo, []proto.Session{row})
	turnsQuiet(t, s, todo.ID)
	waitFor(t, "the recovered review", func() bool { return len(f.notices()) == 1 })
	if got := f.judged; got != 3 {
		t.Fatalf("model calls = %d, want two failed calls and one deferred success", got)
	}
	f.mu.Lock()
	last := f.judgeAsked[len(f.judgeAsked)-1]
	f.mu.Unlock()
	if !strings.Contains(last, "the latest reply") || strings.Contains(last, "the earlier reply") {
		t.Fatalf("the deferred judgment did not read the latest reply:\n%s", last)
	}
	if got := entryLine(f.notices()[0]); !hasSignal(got, "review") || strings.Contains(got, "could not tell whether it finished") {
		t.Fatalf("the recovered judgment produced the wrong signal:\n%s", got)
	}
}

// A valid unsure verdict is a judgment, not a service failure: it keeps the existing immediate unjudged behavior.
func TestSupervisedJudgeUnsureStillReportsImmediately(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	f.judge = `{"verdict":"unsure","reason":"not enough evidence"}`
	startJudgeRecovery(t, s, f, todo, "the work is finished")
	if got := stopSignals(s, todo.ID); got != sigUnjudged {
		t.Fatalf("a valid unsure judgment was deferred or changed: %q", got)
	}
	if got := f.judged; got != 1 {
		t.Fatalf("a valid unsure judgment made %d model calls, want 1", got)
	}
	waitFor(t, "the immediate unjudged signal", func() bool { return len(f.notices()) == 1 })
	if got := entryLine(f.notices()[0]); !strings.Contains(got, "cannot tell whether it is finished") || strings.Contains(got, "the judge service failed") {
		t.Fatalf("a valid unsure judgment was mislabeled:\n%s", got)
	}
}
