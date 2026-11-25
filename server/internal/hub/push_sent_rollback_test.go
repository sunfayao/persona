package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/db/dbtest"
)

// sentRecordStore is what the delivered pushes use of the hub's store: its settings and the hub_push_sent table.
type sentRecordStore interface {
	Store
	pushSentStore
}

// oldBuild is the hub from before hub_push_sent as far as the delivered pushes go (push_grading.go on main before #8257):
// at start it reads only the hub.push_sent setting and keeps the entries inside the 14 days (loadPushGradingState), and every
// delivered push writes the whole list back (addSentLocked, saveIntercepts: PutSetting). It knows nothing of the
// table. Its pusher has no store, so the list is in memory and the daily counts are the real reader's.
type oldBuild struct {
	p     *Pusher
	store sentRecordStore
}

func startOldBuild(t *testing.T, store sentRecordStore, now time.Time) *oldBuild {
	t.Helper()
	p := newPusher(nil, t.Logf)
	p.now = func() time.Time { return now }
	var delivered []pushSent
	if _, err := store.Setting(t.Context(), pushSentSettingKey, &delivered); err != nil {
		t.Fatalf("the old build reads %s: %v", pushSentSettingKey, err)
	}
	cutoff := now.Add(-heldPushMaxAge)
	for _, e := range delivered {
		if e.At.After(cutoff) {
			p.sent = append(p.sent, e)
		}
	}
	return &oldBuild{p: p, store: store}
}

func (o *oldBuild) deliver(t *testing.T, manager, key string, at time.Time) {
	t.Helper()
	o.p.recordSent(pushPolicy{manager: manager, relatedKey: key}, "p1", at)
	o.p.gradingMu.Lock()
	list := slices.Clone(o.p.sent)
	o.p.gradingMu.Unlock()
	if err := o.store.PutSetting(t.Context(), pushSentSettingKey, list); err != nil {
		t.Fatal(err)
	}
}

// startNewBuild is this build starting on store: newPusher's loadSent, on the clock now.
func startNewBuild(t *testing.T, store sentRecordStore, now time.Time) *Pusher {
	t.Helper()
	p := newPusher(nil, t.Logf)
	p.now = func() time.Time { return now }
	p.store = store
	p.loadSent()
	return p
}

// history is a pusher's delivered pushes as what they hold, in order: the time to the microsecond the table keeps.
func history(p *Pusher) []string {
	p.gradingMu.Lock()
	defer p.gradingMu.Unlock()
	out := make([]string, len(p.sent))
	for i, e := range p.sent {
		out[i] = fmt.Sprintf("%s %s %s %q", e.At.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano), e.Level, e.Manager, e.RelatedKey)
	}
	return out
}

func tableRows(t *testing.T, store sentRecordStore) int {
	t.Helper()
	rows, err := store.PushSentAfter(t.Context(), time.Time{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

// sentStores runs a test on the in-memory store and, with a test database, on Postgres (a database of its own: the
// table and the setting are the whole hub's).
func sentStores(t *testing.T, run func(t *testing.T, store sentRecordStore)) {
	t.Run("memory", func(t *testing.T) { run(t, newGradingStore()) })
	t.Run("postgres", func(t *testing.T) {
		base := os.Getenv(dbtest.Env)
		if base == "" {
			t.Skip(dbtest.Env + " not set")
		}
		url, done, err := dbtest.Own(context.Background(), base)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(done)
		d, err := db.Open(context.Background(), url, db.HubSet)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(d.Close)
		run(t, d)
	})
}

// failingAdopt is a store whose start could not read the old setting into the table.
type failingAdopt struct{ *gradingStore }

func (failingAdopt) AdoptPushSentSetting(context.Context, string, time.Time, int) (int, bool, error) {
	return 0, false, errors.New("database away")
}
