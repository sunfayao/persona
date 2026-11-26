package hub

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/bus"
	"github.com/sunfayao/persona/server/internal/db"
)

// heldPrefs is deviceRows whose first prefs write waits for release (a slow database row), closing entered as it starts.
type heldPrefs struct {
	*deviceRows
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (s *heldPrefs) SetDevicePrefs(ctx context.Context, id, credential string, prefs json.RawMessage) error {
	first := false
	s.once.Do(func() { first = true })
	if first {
		close(s.entered)
		<-s.release
	}
	return s.deviceRows.SetDevicePrefs(ctx, id, credential, prefs)
}

// rosterOf is a roster of the given identities with no default manager picked (the rest of managerStore is unused).
type rosterOf struct {
	managerStore
	ids []db.BusIdentity
}

func (r rosterOf) BusIdentities(context.Context) ([]db.BusIdentity, error) { return r.ids, nil }

func (rosterOf) Setting(context.Context, string, any) (bool, error) { return false, nil }

// Two partial changes of one device's settings at once (the apps send one switch per PUT) both stay: the second
// waits for the first instead of writing its change over the settings as they were before the first was stored.
func TestPrefsConcurrentPartialPutsBothApply(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		muted        []string
	}{
		{"one switch", `{"mute_while_desktop":true}`, nil},
		{"muted managers", `{"mute_while_desktop":true,"muted_managers":["m-0b0b0b","m-dead00"]}`, []string{"m-0b0b0b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			h.bus.idents = rosterOf{ids: []db.BusIdentity{{ID: "m-0b0b0b", Role: bus.RoleManager}}}
			store := &heldPrefs{deviceRows: &deviceRows{rows: map[string]db.Device{}},
				entered: make(chan struct{}), release: make(chan struct{})}
			h.push.store = store
			release := sync.OnceFunc(func() { close(store.release) })
			defer release() // a failed check does not leave the first PUT held
			mux := h.routes()
			put := func(body string, code chan<- int) {
				c, _ := call(t, mux, "PUT", "/api/v1/devices/phone/prefs", h.cfg.Token, body)
				code <- c
			}
			first, second := make(chan int, 1), make(chan int, 1)
			go put(`{"input":false}`, first)
			select {
			case <-store.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the first PUT never reached the database")
			}
			go put(tc.second, second)
			// Time for the second PUT to read the settings before the first is stored (without the edit lock it does).
			select {
			case c := <-second:
				t.Fatalf("the second PUT answered %d while the first was still being stored", c)
			case <-time.After(100 * time.Millisecond):
			}
			release()
			for i, ch := range []chan int{first, second} {
				select {
				case c := <-ch:
					if c != 200 {
						t.Fatalf("PUT %d answered %d", i+1, c)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("PUT %d never answered", i+1)
				}
			}
			got := h.push.prefs("phone")
			if got.Input || !got.MuteWhileDesktop || !slices.Equal(got.MutedManagers, tc.muted) {
				t.Fatalf("settings after both PUTs = input %v, mute_while_desktop %v, muted_managers %v; want input off, mute_while_desktop on, %v",
					got.Input, got.MuteWhileDesktop, got.MutedManagers, tc.muted)
			}
			row, _, _ := store.row("phone")
			if stored := devicePrefs(row.Prefs); stored.Input || !stored.MuteWhileDesktop {
				t.Fatalf("stored row = %s; want both changes", row.Prefs)
			}
		})
	}
}
