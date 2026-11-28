package lifecycle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestBackgroundObservationCarriesTurnAndClassNeverText(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	q, err := New(filepath.Join(t.TempDir(), "journal.json"), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Hook("agent-1", "UserPromptSubmit", "prompt-attempt", "opaque-runtime-prompt-id", 23, "process-start"); err != nil {
		t.Fatal(err)
	}
	task := proto.BackgroundTask{ID: "task-1", Kind: "shell", StartedAt: at.Add(time.Second),
		Command: "go test ./... --token=example-secret-value", Description: "Run the example suite at example.com"}
	session := proto.Session{SessionID: "agent-1", Launcher: "claude", Status: proto.StatusWorking, Background: []proto.BackgroundTask{task}}
	if err := q.Inventory([]proto.Session{session}); err != nil {
		t.Fatal(err)
	}
	// The turn ends before the inventory sees the task end: the end still names the turn that started it.
	if err := q.Hook("agent-1", "Stop", "stop-attempt", "", 23, "process-start"); err != nil {
		t.Fatal(err)
	}
	ended := task
	end := at.Add(time.Minute)
	ended.EndedAt, ended.Status = &end, "failed"
	session.Background = nil
	session.SetBackgroundEnded([]proto.BackgroundTask{ended})
	if err := q.Inventory([]proto.Session{session}); err != nil {
		t.Fatal(err)
	}
	batch, err := q.Read(proto.LifecycleBatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var started, finished *proto.LifecycleObservation
	var turn string
	for i, e := range batch.Events {
		switch e.Kind {
		case proto.LifecycleTurnStarted:
			turn = e.Turn
		case proto.LifecycleBackgroundStarted:
			started = &batch.Events[i]
		case proto.LifecycleBackgroundEnded:
			finished = &batch.Events[i]
		}
	}
	if started == nil || finished == nil || turn == "" {
		t.Fatalf("missing events: %+v", batch.Events)
	}
	for _, e := range []*proto.LifecycleObservation{started, finished} {
		if e.Turn != turn || e.Activity != "test" || e.Ref != started.Ref {
			t.Fatalf("background observation lacks turn or class: %+v (turn %s)", e, turn)
		}
	}
	if finished.Outcome != "failed" {
		t.Fatalf("outcome lost: %+v", finished)
	}
	raw, _ := json.Marshal(batch)
	disk := string(raw)
	for _, secret := range []string{"example-secret", "go test", "example suite", "example.com"} {
		if strings.Contains(disk, secret) {
			t.Fatalf("observation contains %q: %s", secret, disk)
		}
	}
}

func TestBackgroundObservationOfOlderTaskHasNoTurnAndUnknownCommandNoClass(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	q, err := New(filepath.Join(t.TempDir(), "journal.json"), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Hook("agent-1", "UserPromptSubmit", "prompt-attempt", "turn-now", 23, "process-start"); err != nil {
		t.Fatal(err)
	}
	end := at.Add(-time.Minute)
	old := proto.BackgroundTask{ID: "task-old", Kind: "shell", StartedAt: at.Add(-time.Hour), EndedAt: &end, Status: "completed", Command: "./example-script.sh"}
	session := proto.Session{SessionID: "agent-1", Launcher: "claude", Status: proto.StatusWorking}
	session.SetBackgroundEnded([]proto.BackgroundTask{old})
	if err := q.Inventory([]proto.Session{session}); err != nil {
		t.Fatal(err)
	}
	batch, _ := q.Read(proto.LifecycleBatchRequest{})
	n := 0
	for _, e := range batch.Events {
		if e.Kind == proto.LifecycleBackgroundStarted || e.Kind == proto.LifecycleBackgroundEnded {
			n++
			if e.Turn != "" || e.Activity != "" {
				t.Fatalf("an already ended task must not claim the open turn or a class: %+v", e)
			}
		}
	}
	if n != 2 {
		t.Fatalf("expected a pair, got %d", n)
	}
}

func TestBackgroundEndedKeepsUnlistedClosedStatuses(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	for status, want := range map[string]string{"cancelled": "cancelled", "canceled": "cancelled", "interrupted": "interrupted",
		"timed_out": "timed_out", "completed": "completed", "ended": "", "": "", "surprise": ""} {
		q, err := New(filepath.Join(t.TempDir(), "journal.json"), func() time.Time { return at })
		if err != nil {
			t.Fatal(err)
		}
		end := at.Add(-time.Minute)
		task := proto.BackgroundTask{ID: "task-1", Kind: "shell", StartedAt: at.Add(-time.Hour), EndedAt: &end, Status: status}
		session := proto.Session{SessionID: "agent-1", Launcher: "claude", Status: proto.StatusWorking}
		session.SetBackgroundEnded([]proto.BackgroundTask{task})
		if err := q.Inventory([]proto.Session{session}); err != nil {
			t.Fatal(err)
		}
		batch, _ := q.Read(proto.LifecycleBatchRequest{})
		for _, e := range batch.Events {
			if e.Kind == proto.LifecycleBackgroundEnded && e.Outcome != want {
				t.Errorf("status %q: outcome %q, want %q", status, e.Outcome, want)
			}
		}
	}
}
