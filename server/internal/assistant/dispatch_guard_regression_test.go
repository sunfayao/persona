package assistant

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func discoveryTools(t *testing.T, out map[string]any) ([]proto.Tool, string) {
	t.Helper()
	if out["unchanged"] == true {
		t.Fatalf("watch stayed unchanged: %+v", out)
	}
	tools, ok := out["tools"].([]proto.Tool)
	if !ok {
		t.Fatalf("watch returned no full tool list: %+v", out)
	}
	revision, _ := out["revision"].(string)
	if revision == "" {
		t.Fatalf("watch returned no revision: %+v", out)
	}
	return tools, revision
}

func guardHeartbeatEvents(t *testing.T) chan string {
	t.Helper()
	events := make(chan string, 4)
	previousEvent := mcpToolsWatchEvent
	mcpToolsWatchEvent = func(event string) { events <- event }
	heartbeat := mcpToolsHeartbeat
	mcpToolsHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() {
		mcpToolsWatchEvent = previousEvent
		mcpToolsHeartbeat = heartbeat
	})
	return events
}

func requireWatchEvent(t *testing.T, events chan string, want string) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("watch event %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("watch did not emit %q", want)
	}
}

func TestDispatchGuardNaturalExpiryUpdatesRevisionAwareList(t *testing.T) {
	f := newGuardFakeHub()
	s := newTestService(t, f)
	full := s.sessionMCPTools(context.Background(), guardMachine, guardSession, true, false)
	now := time.Now().UTC()
	advance := make(chan time.Time, 1)
	var current atomic.Value
	current.Store(now)
	originalNow := dispatchGuardNow
	dispatchGuardNow = func() time.Time {
		select {
		case next := <-advance:
			current.Store(next)
			return next
		default:
			return current.Load().(time.Time)
		}
	}
	t.Cleanup(func() { dispatchGuardNow = originalNow })
	expires := now.Add(time.Minute)
	g := guardPolicy(t, "task_plan")
	g.ExpiresAt = &expires
	putGuard(t, s, g)

	before, revision := discoveryTools(t, s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, ""))
	if slices.Contains(guardNamesOf(before), "task_plan") {
		t.Fatal("the enabled guard did not narrow the initial list")
	}
	events := guardHeartbeatEvents(t)
	watch := make(chan map[string]any, 1)
	go func() {
		watch <- s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, revision)
	}()
	requireWatchEvent(t, events, "wait")
	advance <- now.Add(2 * time.Minute)
	requireWatchEvent(t, events, "heartbeat")
	after, nextRevision := discoveryTools(t, <-watch)
	if nextRevision == revision {
		t.Fatal("natural expiry did not change the discovery revision")
	}
	if !reflect.DeepEqual(full, after) {
		t.Fatalf("expiry did not restore the complete original set: got %d tools, want %d", len(after), len(full))
	}
}

func TestDispatchGuardReadRecoveryUpdatesRevisionAwareList(t *testing.T) {
	f := newGuardFakeHub()
	st := &guardRecoveryStore{memStore: &memStore{vals: map[string][]byte{}}}
	s := newServiceOn(t, f, st)
	full := s.sessionMCPTools(context.Background(), guardMachine, guardSession, true, false)
	st.down.Store(true)
	_, revision := discoveryTools(t, s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, ""))
	events := guardHeartbeatEvents(t)
	watch := make(chan map[string]any, 1)
	go func() {
		watch <- s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, revision)
	}()
	requireWatchEvent(t, events, "wait")
	st.down.Store(false)
	requireWatchEvent(t, events, "heartbeat")
	after, nextRevision := discoveryTools(t, <-watch)
	if nextRevision == revision {
		t.Fatal("read recovery did not change the discovery revision")
	}
	if !reflect.DeepEqual(full, after) {
		t.Fatalf("read recovery did not restore the complete original set: got %d tools, want %d", len(after), len(full))
	}
}

type guardRecoveryStore struct {
	*memStore
	down atomic.Bool
}

func (s *guardRecoveryStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == dispatchGuardSetting && s.down.Load() {
		return false, errors.New("temporary guard read failure")
	}
	return s.memStore.Setting(ctx, key, out)
}

type guardSecondReadStore struct {
	*memStore
	reads atomic.Int32
}

func (s *guardSecondReadStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == dispatchGuardSetting && s.reads.Add(1) == 2 {
		return false, errors.New("one-read outage after a healthy admission read")
	}
	return s.memStore.Setting(ctx, key, out)
}

func TestDispatchGuardSecondReadFailureRecoversWholeSet(t *testing.T) {
	st := &guardSecondReadStore{memStore: &memStore{vals: map[string][]byte{}}}
	s := newServiceOn(t, newGuardFakeHub(), st)
	full := s.sessionMCPTools(context.Background(), guardMachine, guardSession, true, false)
	st.reads.Store(0)
	before, revision := discoveryTools(t, s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, ""))
	if slices.Contains(guardNamesOf(before), "task_plan") {
		t.Fatal("the one-read outage did not narrow the initial list")
	}
	if got := st.reads.Load(); got != 2 {
		t.Fatalf("initial response sampled guard %d times, want 2", got)
	}
	after, nextRevision := discoveryTools(t, s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, revision))
	if nextRevision == revision {
		t.Fatal("the failed decision remained bound to the recovered list")
	}
	if !reflect.DeepEqual(full, after) {
		t.Fatalf("recovery did not restore the complete original set: got %d tools, want %d", len(after), len(full))
	}
}
