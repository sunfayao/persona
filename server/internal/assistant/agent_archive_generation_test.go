package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// archiveGenerationCloseFailureStore fails only the close metadata write after the archive itself is durable.
type archiveGenerationCloseFailureStore struct {
	*fleetMem
	archiveWrites int
}

func (s *archiveGenerationCloseFailureStore) PutSetting(ctx context.Context, key string, value any) error {
	if key == agentArchivesKey {
		s.archiveWrites++
		if s.archiveWrites == 2 {
			return errors.New("close metadata write failed")
		}
	}
	return s.fleetMem.PutSetting(ctx, key, value)
}

func archiveGenerationDrainNative(s *Service, sid string) {
	s.nativeArchiveAwaited(context.Background(), "mac", sid)
}

func TestAgentArchiveGenerationOrdinaryCloseRetriesNativeAfterMetadataFailure(t *testing.T) {
	ft, row := closeTestService(t, func(s *proto.Session) { s.Agent = "codex" })
	store := &archiveGenerationCloseFailureStore{fleetMem: ft.st}
	ft.s.store = func() Store { return store }
	t.Cleanup(func() { archiveGenerationDrainNative(ft.s, row.SessionID) })
	native := playNativeNode(ft, "mac")
	terminalClosed := make(chan struct{})
	native.set(func(string) (any, error) {
		select {
		case <-terminalClosed:
			return map[string]any{"native": nativeArchiveDone}, nil
		default:
			return nil, errors.New("terminal still open")
		}
	})
	initialPending := false
	ft.f.answerNode(deviceSessionRetire, func(_ string, _ json.RawMessage) (any, error) {
		intent := ft.s.nativeArchiveAwaited(context.Background(), "mac", row.SessionID)
		initialPending = intent != nil && intent.State == nativeArchivePending
		close(terminalClosed)
		return map[string]any{"closed": true}, nil
	})
	res, err := ft.s.archiveAndCloseAgentSession(t.Context(), "mac", row.SessionID)
	if err != nil || !res.Closed || !initialPending {
		t.Fatalf("close=%+v err=%v initial native pending=%v", res, err, initialPending)
	}
	intent := ft.s.nativeArchiveAwaited(t.Context(), "mac", row.SessionID)
	if calls := native.calls(); len(calls) != 2 {
		t.Fatalf("native calls after close metadata failure = %v, want the initial try and immediate retry", calls)
	}
	if intent == nil || intent.State != nativeArchiveDone || !intent.Archived {
		t.Fatalf("native archive was not retried after close metadata failure: %+v", intent)
	}
}

func TestAgentArchiveGenerationOrdinaryCloseReadErrorDoesNotReplaceRestore(t *testing.T) {
	ft, row := closeTestService(t, func(s *proto.Session) { s.Agent = "codex" })
	native := playNativeNode(ft, "mac")
	t.Cleanup(func() { archiveGenerationDrainNative(ft.s, row.SessionID) })
	var restoreErr error
	ft.f.answerNode(deviceSessionRetire, func(_ string, _ json.RawMessage) (any, error) {
		restoreErr = ft.s.noteAgentArchive(context.Background(), row, false)
		ft.s.nativeArchiveAwaited(context.Background(), "mac", row.SessionID)
		store := &archiveFailStore{fleetMem: ft.st, failRead: true}
		ft.s.store = func() Store { return store }
		ft.s.agentArchives.loaded = false // force close completion to fail before it can inspect the newer restore
		return map[string]any{"closed": true}, nil
	})
	res, err := ft.s.archiveAndCloseAgentSession(t.Context(), "mac", row.SessionID)
	if err != nil || !res.Closed || restoreErr != nil {
		t.Fatalf("close=%+v err=%v restore=%v", res, err, restoreErr)
	}
	calls := native.calls()
	if len(calls) == 0 || calls[len(calls)-1] != deviceSessionUnarchive+" "+row.SessionID {
		t.Fatalf("restore was not the last native request before close completion: %v", calls)
	}
	intent := ft.s.nativeArchiveAwaited(t.Context(), "mac", row.SessionID)
	if after := native.calls(); len(after) != len(calls) {
		t.Fatalf("close completion retried without reading the newer generation: before=%v after=%v", calls, after)
	}
	if intent == nil || intent.State != nativeArchiveDone || intent.Archived {
		t.Fatalf("close metadata read error replaced the newer restore: %+v", intent)
	}
}

func TestAgentArchiveGenerationCancelledTaskPublishesAfterMetadataFailure(t *testing.T) {
	ft, row := closeTestService(t, func(s *proto.Session) { s.Status, s.State = proto.StatusOffline, proto.StateOffline })
	store := &archiveGenerationCloseFailureStore{fleetMem: ft.st}
	ft.s.store = func() Store { return store }
	t.Cleanup(func() { archiveGenerationDrainNative(ft.s, row.SessionID) })
	before := len(ft.toldOf(assistantwire.EventFleetSessions))
	ft.s.stopCancelledAgent(context.Background(), "todo-close-metadata", "mac", row.SessionID)
	after := len(ft.toldOf(assistantwire.EventFleetSessions))
	if after != before+1 {
		t.Fatalf("fleet publishes after committed archive = %d, want %d (metadata error must not suppress publish)", after-before, 1)
	}
	if !archivedIDs(t, ft.s)[row.SessionID] {
		t.Fatal("cancelled task's archive was not committed")
	}
}
