package lifecycle

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

func TestLifecycleShortTurnSurvivesRestartUntilCommitted(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "journal.json")
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return at }
	q, err := New(path, now)
	if err != nil {
		t.Fatal(err)
	}
	hook := func(event, attempt, prompt string) {
		t.Helper()
		if err := q.Hook("agent-1", event, attempt, prompt, 23, "process-start"); err != nil {
			t.Fatal(err)
		}
	}
	hook("SessionStart", "start-attempt", "")
	hook("UserPromptSubmit", "prompt-attempt", "opaque-runtime-prompt-id")
	at = at.Add(time.Millisecond)
	hook("Stop", "stop-attempt", "")
	hook("Stop", "stop-attempt", "")
	first, err := q.Read(proto.LifecycleBatchRequest{})
	if err != nil || len(first.Events) != 3 {
		t.Fatalf("short turn batch: %+v: %v", first, err)
	}
	if first.Events[1].Turn != first.Events[2].Turn || first.Events[2].DurationMS != 1 {
		t.Fatalf("short turn association missing: %+v", first.Events)
	}
	q, err = New(path, now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := q.Read(proto.LifecycleBatchRequest{})
	if err != nil || first.Stream != replay.Stream || !slices.Equal(first.Events, replay.Events) {
		t.Fatalf("unacknowledged observations were lost: %+v: %v", replay, err)
	}
	ack := proto.LifecycleBatchRequest{Stream: replay.Stream, After: 3}
	rest, err := q.Read(ack)
	if err != nil || rest.First != 4 || len(rest.Events) != 0 {
		t.Fatalf("acknowledgement was not cumulative: %+v: %v", rest, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), "opaque-runtime-prompt-id") {
		t.Fatalf("journal contains raw runtime prompt identifier: %v", err)
	}
}

func TestLifecycleOverflowExposesDroppedRangeAndHistoryReplacement(t *testing.T) {
	t.Parallel()
	now := time.Now
	q, err := New(filepath.Join(t.TempDir(), "journal.json"), now)
	if err != nil {
		t.Fatal(err)
	}
	events := make([]proto.LifecycleObservation, capacity+3)
	for i := range events {
		events[i] = proto.LifecycleObservation{Session: "agent", Source: "hook", Kind: proto.LifecycleTurnStarted,
			Ref: proto.LifecycleRef(strconv.Itoa(i)), At: now()}
	}
	if err := q.Append(events...); err != nil {
		t.Fatal(err)
	}
	b, err := q.Read(proto.LifecycleBatchRequest{})
	if err != nil || b.First != 4 || len(b.Events) != proto.LifecycleBatchSize || b.Events[0].Ref != events[3].Ref {
		t.Fatalf("dropped range is hidden: first=%d size=%d err=%v", b.First, len(b.Events), err)
	}
	if err := q.Transcript("agent", nil, transcript.Span{History: "first"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := q.Transcript("agent", nil, transcript.Span{History: "second"}, false, false); err != nil {
		t.Fatal(err)
	}
	e := q.state.Events[len(q.state.Events)-1]
	if e.Kind != proto.LifecycleGap || e.Outcome != "history_changed" {
		t.Fatalf("history replacement went unmarked: %+v", e)
	}
}
