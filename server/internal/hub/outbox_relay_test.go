package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// handedOver forgets the oldest past its bound, and anything after its time.
func TestHandedOverIsBounded(t *testing.T) {
	var s seenItems
	now := time.Now()
	if !s.first("a", now) || s.first("a", now) {
		t.Fatal("a twice")
	}
	if !s.first("a", now.Add(handedOverFor+time.Second)) {
		t.Fatal("a not forgotten after its time")
	}
	for i := range handedOverMost + 10 {
		s.first(string(rune('A'+i%26))+time.Duration(i).String(), now)
	}
	if len(s.order) > handedOverMost || len(s.at) > handedOverMost {
		t.Fatalf("kept %d / %d", len(s.order), len(s.at))
	}
}

// flakyHandOver is a hand-over store that does not answer while fail is set.
type flakyHandOver struct {
	Store
	fail bool
	seen map[string]bool
}

func (f *flakyHandOver) HandOverFirst(_ context.Context, manager, id string, _ time.Duration) (bool, error) {
	if f.fail {
		return false, errors.New("database hiccup")
	}
	k := manager + "/" + id
	first := !f.seen[k]
	f.seen[k] = true
	return first, nil
}

func (f *flakyHandOver) HandOverForget(_ context.Context, manager, id string) error {
	delete(f.seen, manager+"/"+id)
	return nil
}

// An item handled while the store did not answer is not delivered again once it does.
func TestHandedOverWhileTheStoreWasDown(t *testing.T) {
	h := newTestHub(t)
	st := &flakyHandOver{fail: true, seen: map[string]bool{}}
	h.store = st
	first, rel, paused := h.beginHandOver(context.Background(), "assistant", "ntf_x")
	if !first || paused {
		t.Fatalf("the first time: first=%v paused=%v", first, paused)
	}
	if rel != nil {
		rel()
	}
	st.fail = false
	first, rel, paused = h.beginHandOver(context.Background(), "assistant", "ntf_x")
	if first || paused || rel != nil {
		t.Fatalf("delivered again once the store answered: first=%v paused=%v", first, paused)
	}
	first, rel, paused = h.beginHandOver(context.Background(), "assistant", "ntf_y")
	if !first || paused {
		t.Fatalf("a new one: first=%v paused=%v", first, paused)
	}
	if rel != nil {
		rel()
	}
}

// gatedHandOver is a hand-over store whose answers wait for a gate (a slow database).
type gatedHandOver struct {
	Store
	gate chan struct{}
	mu   sync.Mutex
	seen map[string]bool
}

func (g *gatedHandOver) HandOverFirst(_ context.Context, manager, id string, _ time.Duration) (bool, error) {
	<-g.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	k := manager + "/" + id
	first := !g.seen[k]
	g.seen[k] = true
	return first, nil
}

func (g *gatedHandOver) HandOverForget(context.Context, string, string) error { return nil }

// Two sends of one id at once (a live push still being handled when it comes again from the outbox): exactly one is
// delivered, also with a slow store.
func TestTwoSendsOfOneIDAtOnce(t *testing.T) {
	h := newTestHub(t)
	st := &gatedHandOver{gate: make(chan struct{}), seen: map[string]bool{}}
	h.store = st
	results := make(chan bool, 2)
	for range 2 {
		go func() {
			first, rel, _ := h.beginHandOver(context.Background(), "assistant", "ntf_z")
			if rel != nil {
				rel()
			}
			results <- first
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(st.gate)
	a, b := <-results, <-results
	if a == b {
		t.Fatalf("both %v: want exactly one delivery", a)
	}
}

// hookHandOver records HandOverFirst / HandOverForget calls and supports hooks for quiesce interleavings.
type hookHandOver struct {
	Store
	mu       sync.Mutex
	seen     map[string]bool
	firsts   int
	forgets  int
	onFirst  func(manager, id string)
	onForget func(manager, id string)
}

func (s *hookHandOver) HandOverFirst(_ context.Context, manager, id string, _ time.Duration) (bool, error) {
	if s.onFirst != nil {
		s.onFirst(manager, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firsts++
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	k := manager + "/" + id
	first := !s.seen[k]
	s.seen[k] = true
	return first, nil
}

func (s *hookHandOver) HandOverForget(_ context.Context, manager, id string) error {
	if s.onForget != nil {
		s.onForget(manager, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgets++
	delete(s.seen, manager+"/"+id)
	return nil
}

// A pause starting after HandOverFirst commits and before delivery fails waits for the handover turn to finish
// HandOverForget, so a retry after resume is not stranded against a stale durable claim.
func TestPauseBetweenHandOverClaimAndFailedDeliveryDoesNotStrandItem(t *testing.T) {
	c := &client{device: "test-device", out: make(chan []byte, 16)}
	h := newTestHub(t)
	h.clients, h.push = map[*client]struct{}{c: {}}, newPusher(nil, t.Logf)
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	<-c.out
	if err := h.push.setPrefs(c.device, NotifyPrefs{Input: true, Done: DoneOff}); err != nil {
		t.Fatal(err)
	}

	pauseDone := make(chan error, 1)
	var pauseOnce sync.Once
	st := &hookHandOver{
		seen: map[string]bool{},
		onFirst: func(_, _ string) {
			pauseOnce.Do(func() {
				go func() { pauseDone <- h.quiesce.pause(context.Background()) }()
				waitFor(t, "quiesce paused during handover claim", func() bool {
					h.quiesce.mu.Lock()
					defer h.quiesce.mu.Unlock()
					return h.quiesce.paused
				})
				// pause must still be waiting on the held handover turn.
				select {
				case err := <-pauseDone:
					t.Fatalf("pause completed before delivery and HandOverForget settled: %v", err)
				case <-time.After(20 * time.Millisecond):
				}
			})
		},
	}
	h.store = st

	ix := proto.Interaction{ID: "ix-pause", MachineID: "m", SessionID: "s", Kind: proto.IxQuestion, Summary: "choose"}
	raw, _ := json.Marshal(assistantwire.Outbox{Items: []assistantwire.OutboxItem{{
		ID:           "ntf_ix_pause",
		At:           time.Now(),
		Notification: &assistantwire.Notification{ID: "ntf_ix_pause", Kind: assistantwire.NotifyInput, Interaction: &ix},
	}}})
	// Delivery fails because the interaction card has not been reported yet.
	if _, err := busMethods[assistantwire.MethodOutbox](h, context.Background(), nil, raw); err != nil {
		t.Fatalf("outbox failed delivery: %v", err)
	}
	select {
	case err := <-pauseDone:
		if err != nil {
			t.Fatalf("pause returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pause did not finish after handover settled")
	}
	st.mu.Lock()
	forgets := st.forgets
	stillClaimed := st.seen["assistant/ntf_ix_pause"]
	st.mu.Unlock()
	if forgets != 1 || stillClaimed {
		t.Fatalf("durable forget did not run under the held turn: forgets=%d stillClaimed=%v", forgets, stillClaimed)
	}

	h.quiesce.resume()
	reportInteraction(t, h, ix)
	for drained := false; !drained; {
		select {
		case <-c.out:
		case <-time.After(150 * time.Millisecond):
			drained = true
		}
	}

	// Retry after resume must succeed and deliver the push.
	if _, err := busMethods[assistantwire.MethodOutbox](h, context.Background(), nil, raw); err != nil {
		t.Fatalf("outbox retry after resume: %v", err)
	}
	select {
	case got := <-c.out:
		var m map[string]any
		if json.Unmarshal(got, &m) != nil || m["interaction_id"] != "ix-pause" {
			t.Fatalf("unexpected retry push: %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retried push was stranded after pause between claim and failed delivery")
	}
}
