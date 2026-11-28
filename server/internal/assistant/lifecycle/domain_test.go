package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/proto"
)

type testStore struct {
	Store
	page    proto.LifecyclePage
	cursor  proto.LifecycleBatchRequest
	fail    bool
	commits int
	last    proto.LifecycleBatch
}

func (s *testStore) LifecycleCursor(context.Context, string) (proto.LifecycleBatchRequest, error) {
	return s.cursor, nil
}

func (s *testStore) CommitLifecycle(_ context.Context, _ string, cursor proto.LifecycleBatchRequest, batch proto.LifecycleBatch, _ time.Time) error {
	if cursor != s.cursor {
		return errors.New("unexpected cursor")
	}
	if s.fail {
		return errors.New("fixture commit failure")
	}
	s.cursor = proto.LifecycleBatchRequest{Stream: batch.Stream, After: batch.First + int64(len(batch.Events)) - 1}
	s.commits++
	s.last = batch
	return nil
}

func (s *testStore) LifecyclePage(context.Context, string, string, string, int64, bool, int, time.Time) (proto.LifecyclePage, error) {
	return s.page, nil
}

func TestLifecycleRequestsReplayBeforeAcknowledgingAfterCommit(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	st := &testStore{fail: true}
	event := proto.LifecycleObservation{Session: "session", Kind: proto.LifecycleTurnFinished, At: at,
		Source: "codex", Ref: proto.LifecycleRef("turn"), Turn: proto.LifecycleRef("turn")}
	stream := proto.LifecycleRef("node stream")
	ready := 0
	d := New(Dependencies{
		Now:   func() time.Time { return at },
		Scope: func(context.Context, string, string) (string, bool, error) { return "", true, nil },
		Ready: func(_, _ string) { ready++ },
		Call: func(_ context.Context, machine, method string, req, out any) error {
			if machine != "machine" || method != nodeproto.LifecycleRead {
				t.Fatalf("unexpected request %q %q", machine, method)
			}
			cursor := req.(proto.LifecycleBatchRequest)
			r := out.(*proto.LifecycleBatch)
			*r = proto.LifecycleBatch{Stream: stream, First: 1, Events: []proto.LifecycleObservation{event}}
			if cursor.After == 1 {
				r.First, r.Events = 2, nil
			}
			return nil
		},
	})
	if err := d.pull(t.Context(), st, "machine"); err == nil || st.cursor.After != 0 || ready != 0 {
		t.Fatalf("failed commit acknowledged or invoked recovery: %v, %+v, %d", err, st.cursor, ready)
	}
	st.fail = false
	for range 2 {
		if err := d.pull(t.Context(), st, "machine"); err != nil {
			t.Fatal(err)
		}
	}
	if st.cursor.After != 1 || ready != 1 || st.commits != 1 {
		t.Fatalf("unexpected acknowledgement or recovery: %+v, %d, %d", st.cursor, ready, st.commits)
	}
}

func TestLifecycleRecoveryRejectsAStaleCloseWhileWaiting(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	st := &testStore{}
	d := New(Dependencies{Store: func() Store { return st }, Now: func() time.Time { return at }})
	push := func(kind, ref, confidence string, when time.Time) {
		st.page.Events = append([]proto.LifecycleEvent{{Confidence: confidence,
			LifecycleObservation: proto.LifecycleObservation{Session: "session", Kind: kind, At: when, Ref: ref}}}, st.page.Events...)
	}
	push(proto.LifecycleGap, "gap", "observed", at)
	if !d.MayUseClosingTranscript(t.Context(), "machine", "session", at) {
		t.Fatal("coverage gap displaced independently confirmed transcript")
	}
	push(proto.LifecycleWaitOpened, "ix", "confirmed", at.Add(time.Second))
	if d.MayUseClosingTranscript(t.Context(), "machine", "session", at) {
		t.Fatal("used a transcript that predates a pending wait")
	}
	push(proto.LifecycleWaitResolved, "ix", "confirmed", at.Add(2*time.Second))
	if !d.MayUseClosingTranscript(t.Context(), "machine", "session", at) {
		t.Fatal("resolved wait blocked recovery")
	}
	push(proto.LifecycleTurnStarted, "turn", "confirmed", at.Add(3*time.Second))
	if d.MayUseClosingTranscript(t.Context(), "machine", "session", at) {
		t.Fatal("used a transcript older than the confirmed turn start")
	}
}

func TestLifecycleRoutesRequireTheOwnerAndTheReferencedTodo(t *testing.T) {
	t.Parallel()
	st := &testStore{page: proto.LifecyclePage{Events: []proto.LifecycleEvent{}}}
	d := New(Dependencies{
		Store: func() Store { return st }, Now: time.Now,
		UserOnly: func(w http.ResponseWriter, r *http.Request) bool {
			if r.Header.Get("Role") == "owner" {
				return true
			}
			w.WriteHeader(http.StatusForbidden)
			return false
		},
		AllowTodo:    func(_ context.Context, id string) bool { return id == "allowed" },
		AllowSession: func(_ context.Context, machine, id string) bool { return machine == "machine" && id == "session" },
	})
	mux := http.NewServeMux()
	d.Register(func(pattern string, handler http.HandlerFunc) { mux.Handle(pattern, handler) })
	for _, c := range []struct {
		path, role string
		want       int
	}{
		{"/api/v1/todos/allowed/lifecycle", "extension", http.StatusForbidden},
		{"/api/v1/todos/absent/lifecycle", "owner", http.StatusNotFound},
		{"/api/v1/todos/allowed/lifecycle?after=1&before=2", "owner", http.StatusBadRequest},
		{"/api/v1/todos/allowed/lifecycle?after=5", "owner", http.StatusOK},
		{"/api/v1/sessions/machine/session/lifecycle", "owner", http.StatusOK},
	} {
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		r.Header.Set("Role", c.role)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.path, w.Code, c.want)
		}
	}
}

func TestLifecycleKeepsKnownActivityClassesAndDropsUnknownOnes(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	turn := proto.LifecycleRef("turn")
	event := func(ref, activity string) proto.LifecycleObservation {
		return proto.LifecycleObservation{Session: "session", Kind: proto.LifecycleBackgroundStarted, At: at, Source: "inventory",
			Ref: proto.LifecycleRef(ref), Turn: turn, Activity: activity}
	}
	st := &testStore{}
	d := New(Dependencies{
		Now:   func() time.Time { return at },
		Scope: func(context.Context, string, string) (string, bool, error) { return "", true, nil },
		Call: func(_ context.Context, _, _ string, _, out any) error {
			*out.(*proto.LifecycleBatch) = proto.LifecycleBatch{Stream: proto.LifecycleRef("stream"), First: 1,
				Events: []proto.LifecycleObservation{event("a", "test"), event("b", "future_class"), event("c", "")}}
			return nil
		},
	})
	if err := d.pull(t.Context(), st, "machine"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range st.last.Events {
		got = append(got, e.Activity)
		if e.Turn != turn {
			t.Fatalf("turn lost: %+v", e)
		}
	}
	if len(got) != 3 || got[0] != "test" || got[1] != "" || got[2] != "" {
		t.Fatalf("activity classes after validation: %q", got)
	}
}

func TestLifecycleAcceptsEveryClosedBackgroundOutcome(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	for _, outcome := range append([]string{""}, proto.LifecycleBackgroundOutcomes...) {
		b := proto.LifecycleBatch{Stream: proto.LifecycleRef("stream"), First: 1, Events: []proto.LifecycleObservation{{Session: "session",
			Kind: proto.LifecycleBackgroundEnded, At: at, Source: "inventory", Ref: proto.LifecycleRef("a"), Outcome: outcome}}}
		if !validBatch(b) {
			t.Errorf("outcome %q refused", outcome)
		}
	}
	b := proto.LifecycleBatch{Stream: proto.LifecycleRef("stream"), First: 1, Events: []proto.LifecycleObservation{{Session: "session",
		Kind: proto.LifecycleBackgroundEnded, At: at, Source: "inventory", Ref: proto.LifecycleRef("a"), Outcome: "surprise"}}}
	if validBatch(b) {
		t.Error("an outcome outside the closed set was accepted")
	}
}
