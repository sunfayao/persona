package hub

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// sentPusher is a pusher on store (nil: memory only) with its clock at now, started as newPusher starts one: it reads
// the delivered pushes back (loadSent) on that clock.
func sentPusher(t *testing.T, store *gradingStore, now time.Time) *Pusher {
	t.Helper()
	p := newPusher(nil, t.Logf)
	p.now = func() time.Time { return now }
	if store != nil {
		p.store = store
		p.loadSent()
	}
	return p
}

// statsViews are what the readers of the record answer: the daily counts for every manager and for one, over the
// default day, two days and the whole 14 days.
func statsViews(p *Pusher) map[string][]pushDayStats {
	now := p.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	out := map[string][]pushDayStats{}
	for _, since := range []time.Time{today, today.AddDate(0, 0, -1), now.Add(-heldPushMaxAge)} {
		for _, manager := range []string{"", "manager-a", "manager-b"} {
			out[since.Format(time.RFC3339)+"/"+manager] = p.pushDailyStats(since, manager)
		}
	}
	return out
}

// Upgrade: the entries of the old whole-list setting are added to the table once, at the first start; the expired
// ones go, the counts equal what the old build read from the setting, and a second start adds nothing again. The
// setting stays as the old build left it, for a switch back to that build.
func TestPushSentAdoptsTheOldSetting(t *testing.T) {
	now := time.Now().UTC()
	var legacy []pushSent
	for i := range 10 { // past the 14 days: the old build dropped them at load too
		legacy = append(legacy, pushSent{At: now.Add(-heldPushMaxAge - time.Duration(10-i)*time.Hour), Level: "p1", Manager: "manager-a"})
	}
	for i := range pushSentMax - 10 {
		manager := "manager-a"
		if i%3 == 0 {
			manager = "manager-b"
		}
		legacy = append(legacy, pushSent{At: now.Add(-time.Duration(pushSentMax-10-i) * 9 * time.Minute), Level: "p1",
			RelatedKey: fmt.Sprintf("k-%d", i), Manager: manager})
	}
	store := newGradingStore()
	if err := store.PutSetting(t.Context(), pushSentSettingKey, legacy); err != nil {
		t.Fatal(err)
	}
	// What the old build answered: loadPushGradingState kept the setting's entries inside the 14 days.
	old := sentPusher(t, nil, now)
	for _, e := range legacy {
		if e.At.After(now.Add(-heldPushMaxAge)) {
			old.sent = append(old.sent, e)
		}
	}
	want := statsViews(old)

	before := slices.Clone(store.vals[pushSentSettingKey])
	p := sentPusher(t, store, now)
	if after := store.vals[pushSentSettingKey]; !bytes.Equal(after, before) || store.puts != 1 {
		t.Fatalf("the upgrade changed the old setting (%d puts, %d bytes, was %d)", store.puts, len(after), len(before))
	}
	if rows := store.sentTable(); len(rows) != pushSentMax-10 || rows[0].RelatedKey != "k-0" || rows[len(rows)-1].RelatedKey != fmt.Sprintf("k-%d", pushSentMax-11) {
		t.Fatalf("table after the move: %d rows, want the %d unexpired entries in order", len(rows), pushSentMax-10)
	}
	if got := statsViews(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("counts after the upgrade differ from the old build's:\n got %+v\nwant %+v", got, want)
	}
	again := sentPusher(t, store, now)
	if rows := store.sentTable(); len(rows) != pushSentMax-10 {
		t.Fatalf("a second start changed the table: %d rows", len(rows))
	}
	if got := statsViews(again); !reflect.DeepEqual(got, want) {
		t.Fatalf("counts after a second start differ:\n got %+v\nwant %+v", got, want)
	}
}

// landedThenFailed is the real database with its first AddPushSent calls committing and then reporting an error.
type landedThenFailed struct {
	*db.DB
	fail atomic.Int32
}
