package node

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// A failed async start's record is swapped for a starting one under the same id in one step: messages wait for the
// new start again, and its outcome is theirs. Only the failed record itself can be swapped, once.
func TestFailedAsyncStartCanRestartUnderItsID(t *testing.T) {
	var l startLog
	t.Cleanup(l.close)
	failed := &startRec{done: make(chan struct{}), session: proto.Session{SessionID: startID, Starting: true}}
	if err := l.add(startID, failed); err != nil {
		t.Fatal(err)
	}
	if err := l.restart(startID, failed, &startRec{done: make(chan struct{})}); !errors.Is(err, errStartNotWaiting) {
		t.Errorf("restarted a start still running: %v", err)
	}
	l.finish(startID, refusal(proto.CodeShellWaiting, "waiting", map[string]any{"line": "q?"}))
	if err := l.wait(context.Background(), startID); rpcCode(err) != proto.CodeShellWaiting {
		t.Fatalf("wait: %v", err)
	}
	if err := l.add(startID, &startRec{done: make(chan struct{})}); err == nil || !strings.Contains(err.Error(), "already starting") {
		t.Errorf("add over a failed start: %v", err)
	}
	again := &startRec{done: make(chan struct{}), session: proto.Session{SessionID: startID, Starting: true}}
	if err := l.restart(startID, failed, again); err != nil {
		t.Fatal(err)
	}
	if err := l.restart(startID, failed, &startRec{done: make(chan struct{})}); !errors.Is(err, errStartNotWaiting) {
		t.Errorf("restarted twice: %v", err)
	}
	if r, _ := l.failed(startID); r != nil {
		t.Error("listed as failed while starting again")
	}
	if got := l.listed(nil); len(got) != 1 || !got[0].Starting || got[0].StartErrorCode != "" {
		t.Errorf("listed %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := l.wait(ctx, startID); err == nil || rpcCode(err) != "" {
		t.Errorf("a message did not wait for the new start: %v", err)
	}
	l.finish(startID, nil)
	if err := l.wait(context.Background(), startID); err != nil {
		t.Errorf("after the new start: %v", err)
	}
}
