package quietturn

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu   sync.Mutex
	vals map[string][]byte
}

func (m *memStore) Setting(_ context.Context, key string, out any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (m *memStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vals == nil {
		m.vals = map[string][]byte{}
	}
	m.vals[key] = raw
	return nil
}

type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func newBook(st *memStore, clock *fakeClock) *Book {
	return New(Dependencies{Store: func() Store { return st }, Clock: clock})
}

func start() *fakeClock { return &fakeClock{at: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)} }

func TestFinishOldestOpenBatchWithoutAnId(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	b.Open(ctx, "c1", "a")
	b.Open(ctx, "c1", "b")
	r, err := b.Finish(ctx, "c1", "", "silent")
	if err != nil || r.Level != "triage" || r.Note != "" {
		t.Fatalf("finish: %+v, %v", r, err)
	}
	if _, err := b.Finish(ctx, "c1", "a", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("a closed batch finished again: %v", err)
	}
	if _, err := b.Finish(ctx, "c1", "b", "silent"); err != nil {
		t.Fatalf("the later batch is still open: %v", err)
	}
	if _, err := b.Finish(ctx, "c1", "", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("nothing is open: %v", err)
	}
}

func TestFinishClosesOlderBatchesToo(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	for _, id := range []string{"a", "b", "c"} {
		b.Open(ctx, "c1", id)
	}
	if _, err := b.Finish(ctx, "c1", "b", "silent"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(ctx, "c1", "a", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("an older batch stayed open: %v", err)
	}
	if _, err := b.Finish(ctx, "c1", "c", "silent"); err != nil {
		t.Fatalf("a newer batch was closed: %v", err)
	}
}

func TestSilentFinishNamingLaterBatchKeepsVisibleOlderBatch(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	b.Open(ctx, "c1", "a")
	b.Open(ctx, "c1", "b") // delivered while the turn of a runs
	b.Promote(ctx, "c1")   // a's turn accepted a task
	if r, err := b.Finish(ctx, "c1", "b", "silent"); err != nil || r.Level != "user" || r.Note == "" {
		t.Fatalf("b's silent finish closes a that was already shown, so it stays visible: %+v, %v", r, err)
	}
}

func TestSilentFinishAfterPromotionIsOkAndVisible(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	b.Open(ctx, "c1", "a")
	b.Promote(ctx, "c1")
	r, err := b.Finish(ctx, "c1", "", "silent")
	if err != nil || r.Level != "user" || r.Note == "" {
		t.Fatalf("silent after promotion: %+v, %v", r, err)
	}
}

func TestOwnerSpeakingKeepsBatchesOpen(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	b.Hold("c1")
	b.Open(ctx, "c1", "a")
	if !b.Release("c1") || b.Held("c1") {
		t.Fatal("the owner speaking releases the pushes")
	}
	if r, err := b.Finish(ctx, "c1", "a", "silent"); err != nil || r.Level != "triage" {
		t.Fatalf("finish after the owner spoke: %+v, %v", r, err)
	}
}

func TestUnknownBatchAndOtherConversation(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	b.Open(ctx, "c1", "a")
	if _, err := b.Finish(ctx, "c1", "nope", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("unknown batch: %v", err)
	}
	if _, err := b.Finish(ctx, "c2", "a", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("another conversation's batch: %v", err)
	}
	if _, err := b.Finish(ctx, "c1", "a", "silent"); err != nil {
		t.Fatalf("a failed finish closed the batch: %v", err)
	}
}

func TestRestartKeepsOpenAndVisibleBatches(t *testing.T) {
	t.Parallel()
	ctx, st, clock := context.Background(), &memStore{}, start()
	b := newBook(st, clock)
	b.Open(ctx, "c1", "a")
	b.Open(ctx, "c1", "b")
	b.Promote(ctx, "c1")
	b.Open(ctx, "c2", "x")
	if _, err := b.Finish(ctx, "c2", "x", "silent"); err != nil {
		t.Fatal(err)
	}
	restarted := newBook(st, clock)
	if r, err := restarted.Finish(ctx, "c1", "a", "silent"); err != nil || r.Level != "user" {
		t.Fatalf("a was promoted before the restart: %+v, %v", r, err)
	}
	if _, err := restarted.Finish(ctx, "c1", "b", "silent"); err != nil {
		t.Fatalf("b is open after the restart: %v", err)
	}
	if _, err := restarted.Finish(ctx, "c2", "x", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("a finished batch came back: %v", err)
	}
}

func TestExpireDropsOldBatchesAndHolds(t *testing.T) {
	t.Parallel()
	ctx, st, clock := context.Background(), &memStore{}, start()
	b := newBook(st, clock)
	b.Hold("c1")
	b.Open(ctx, "c1", "old")
	clock.add(90 * time.Minute)
	b.Open(ctx, "c1", "new")
	if b.Expire(ctx) {
		t.Fatal("nothing is past its time yet")
	}
	clock.add(40 * time.Minute) // old is 130 minutes old, new 40
	if !b.Expire(ctx) || b.Held("c1") {
		t.Fatal("the hold outlived the maximum")
	}
	if _, err := b.Finish(ctx, "c1", "old", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("an old batch survived: %v", err)
	}
	if _, err := newBook(st, clock).Finish(ctx, "c1", "new", "silent"); err != nil {
		t.Fatalf("the pruned book was stored without the young batch: %v", err)
	}
}

func TestDropForgetsAnUndeliveredBatch(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	b.Open(ctx, "c1", "a")
	b.Drop(ctx, "c1", "a")
	if _, err := b.Finish(ctx, "c1", "", "silent"); !errors.Is(err, ErrNoBatch) {
		t.Fatalf("a dropped batch finished: %v", err)
	}
}

func TestUserFinishMarksTheHeldConversationShown(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), newBook(&memStore{}, start())
	b.Hold("c1")
	b.Open(ctx, "c1", "a")
	if b.Shown("c1") {
		t.Fatal("shown before anything was shown")
	}
	if r, err := b.Finish(ctx, "c1", "a", "user"); err != nil || r.Level != "user" || !b.Shown("c1") {
		t.Fatalf("user finish: %+v, %v, shown %v", r, err, b.Shown("c1"))
	}
}
