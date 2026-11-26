package hub

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
	"time"
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

// Upgrade: the entries of the old whole-list setting are added to the table once, at the first start; the expired
// ones go, the entries equal what the old build read from the setting, and a second start adds nothing again. The
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
	want := old.sent

	before := slices.Clone(store.vals[pushSentSettingKey])
	p := sentPusher(t, store, now)
	if after := store.vals[pushSentSettingKey]; !bytes.Equal(after, before) || store.puts != 1 {
		t.Fatalf("the upgrade changed the old setting (%d puts, %d bytes, was %d)", store.puts, len(after), len(before))
	}
	if rows := store.sentTable(); len(rows) != pushSentMax-10 || rows[0].RelatedKey != "k-0" || rows[len(rows)-1].RelatedKey != fmt.Sprintf("k-%d", pushSentMax-11) {
		t.Fatalf("table after the move: %d rows, want the %d unexpired entries in order", len(rows), pushSentMax-10)
	}
	if !slices.EqualFunc(p.sent, want, func(got, old pushSent) bool {
		if got.ID == "" {
			return false
		}
		got.ID = old.ID
		return got == old
	}) {
		t.Fatal("migrated entries changed or lack generated IDs")
	}
	again := sentPusher(t, store, now)
	if rows := store.sentTable(); len(rows) != pushSentMax-10 {
		t.Fatalf("a second start changed the table: %d rows", len(rows))
	}
	if !slices.Equal(again.sent, p.sent) {
		t.Fatal("a second start changed the migrated entries")
	}
}
