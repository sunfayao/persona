package assistant

import (
	"context"
	"testing"
	"time"
)

func TestNativeFollowUpReadsSignalsOfAMessageWithoutFlag(t *testing.T) {
	done := superviseSignal{Todo: "t1", Kind: sigReview, Report: "completion", Key: "k"}
	routine := superviseSignal{Todo: "t1", Kind: sigStalled, Key: "k2"}
	for _, tc := range []struct {
		name string
		msg  relayMsg
		want bool
	}{
		{"old build, completion", relayMsg{Tool: superviseTool, Signals: encodeDigestSignals([]superviseSignal{routine, done})}, true},
		{"old build, routine", relayMsg{Tool: superviseTool, Signals: encodeDigestSignals([]superviseSignal{routine})}, false},
		{"old build, text only", relayMsg{Tool: superviseTool}, false},
		{"flagged", relayMsg{Tool: superviseTool, SentUp: true}, true},
		{"conversation_send", relayMsg{Tool: "conversation_send"}, true},
	} {
		if got := tc.msg.nativeFollowUp(); got != tc.want {
			t.Errorf("%s: nativeFollowUp = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRecheckQueuedDigestRecomputesSentUp(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	s.supervise.mu.Lock()
	s.supervise.init()
	s.supervise.mu.Unlock()
	kept := superviseSignal{Todo: todo.ID, Kind: sigStalled, Key: "stalled", Minutes: 50, At: time.Now().Add(-time.Minute)}
	gone := superviseSignal{Todo: "no-such-todo", Kind: sigReview, Report: "completion", Key: "review"}
	msg := relayMsg{From: superviseTool, Tool: superviseTool, Text: "synthetic", SentUp: true, At: time.Now(),
		Signals: encodeDigestSignals([]superviseSignal{kept, gone})}
	if n, err := s.queueRelayDurable("conv1", msg); err != nil || n != 1 {
		t.Fatalf("queue: n=%d err=%v", n, err)
	}
	s.relays.mu.Lock()
	queued := s.relays.st.Queue["conv1"][0]
	s.relays.mu.Unlock()
	next, stale, keep := s.recheckQueuedDigest(context.Background(), "conv1", queued)
	if stale || keep {
		t.Fatalf("stale=%v keep=%v: the routine signal should remain", stale, keep)
	}
	if list, _ := decodeDigestSignals(next.Signals); len(list) != 1 || list[0].Kind != sigStalled {
		t.Fatalf("signals kept: %+v", list)
	}
	if next.SentUp || next.nativeFollowUp() {
		t.Fatalf("the completion review was dropped, so the digest is routine: SentUp=%v", next.SentUp)
	}
}
