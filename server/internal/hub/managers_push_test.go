package hub

// Per-manager push switches (docs/assistant/managers.md "Clients"): a device that turned a manager's pushes off gets none of
// what that manager asks for, whichever way it asks, and everything else as before.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
)

// The prefs API keeps the muted managers per device, trimmed, without repeats and only the roster's (a removed
// manager's id would take one of the 64 places for good), and always answers a list (its presence tells an app the
// hub has the switch); a PUT of other fields keeps them.
func TestPrefsMutedManagersAPI(t *testing.T) {
	h, srv, _ := newBusHub(t)
	busToken(t, h, "m-0b0b0b", "B")
	do := func(method, body string) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/api/v1/devices/d1/prefs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %v", method, resp.StatusCode, out)
		}
		return out
	}
	if got := do("GET", ""); !reflect.DeepEqual(got["muted_managers"], []any{}) {
		t.Fatalf("default: %v", got["muted_managers"])
	}
	got := do("PUT", `{"muted_managers":[" m-0b0b0b ","m-0b0b0b","","assistant","m-dead00"]}`)
	if !reflect.DeepEqual(got["muted_managers"], []any{"m-0b0b0b", "assistant"}) || got["done"] != DoneLong {
		t.Fatalf("stored (m-dead00 is no manager of the roster): %v", got)
	}
	if got := do("PUT", `{"done":"always"}`); !reflect.DeepEqual(got["muted_managers"], []any{"m-0b0b0b", "assistant"}) {
		t.Fatalf("after another change: %v", got["muted_managers"])
	}
	if got := do("PUT", `{"muted_managers":null}`); !reflect.DeepEqual(got["muted_managers"], []any{}) {
		t.Fatalf("null: %v", got["muted_managers"])
	}
	// Removed managers' ids take no place: 70 stale ones before a real one still keep the real one.
	stale := make([]string, 0, 71)
	for i := range 70 {
		stale = append(stale, fmt.Sprintf("m-%06x", 0xa00000+i))
	}
	raw, _ := json.Marshal(map[string]any{"muted_managers": append(stale, "m-0b0b0b")})
	if got := do("PUT", string(raw)); !reflect.DeepEqual(got["muted_managers"], []any{"m-0b0b0b"}) {
		t.Fatalf("stale ids: %v", got["muted_managers"])
	}
	// More managers in the roster than places: the first 64 stay.
	many := make([]string, 0, 80)
	for i := range 80 {
		id := fmt.Sprintf("m-%06x", 0xb00000+i)
		busToken(t, h, id, id)
		many = append(many, id)
	}
	raw, _ = json.Marshal(map[string]any{"muted_managers": many})
	if got, _ := do("PUT", string(raw))["muted_managers"].([]any); len(got) != maxMutedManagers || got[0] != many[0] {
		t.Fatalf("bounded: %d", len(got))
	}
	if (NotifyPrefs{MutedManagers: []string{""}}).mutes("") {
		t.Fatal("the hub's own pushes are nobody's to mute")
	}
}

// rosterReads is the database, counting the roster reads; away: they fail, as a database that does not answer.
type rosterReads struct {
	*db.DB
	away           atomic.Bool
	lists, lookups atomic.Int32
}

func (r *rosterReads) BusIdentities(ctx context.Context) ([]db.BusIdentity, error) {
	r.lists.Add(1)
	if r.away.Load() {
		return nil, errors.New("database away")
	}
	return r.DB.BusIdentities(ctx)
}

func (r *rosterReads) BusIdentityByID(ctx context.Context, id string) (*db.BusIdentity, error) {
	r.lookups.Add(1)
	if r.away.Load() {
		return nil, errors.New("database away")
	}
	return r.DB.BusIdentityByID(ctx, id)
}

// The muted ids are checked against one read of the roster, not one lookup each; when the roster cannot answer, every
// id is kept (it may be a manager the database would have named).
func TestPrefsMutedManagersWhenTheRosterCannotAnswer(t *testing.T) {
	h, srv, store := newBusHub(t)
	busToken(t, h, "m-0b0b0b", "B")
	reads := &rosterReads{DB: store}
	h.bus.idents = reads
	put := func(body string) []any {
		t.Helper()
		req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/devices/d1/prefs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT: %d %v", resp.StatusCode, out)
		}
		got, _ := out["muted_managers"].([]any)
		return got
	}
	body := `{"muted_managers":["m-0b0b0b","m-0c0c0c","m-0d0d0d","m-dead00"]}`
	busToken(t, h, "m-0c0c0c", "C")
	busToken(t, h, "m-0d0d0d", "D")
	reads.lists.Store(0)
	reads.lookups.Store(0)
	if got := put(body); !reflect.DeepEqual(got, []any{"m-0b0b0b", "m-0c0c0c", "m-0d0d0d"}) {
		t.Fatalf("the roster answering: %v", got)
	}
	if l, k := reads.lists.Load(), reads.lookups.Load(); l != 1 || k != 0 {
		t.Fatalf("roster reads for four ids: %d lists, %d lookups; want one list", l, k)
	}
	reads.away.Store(true)
	h.expireOwners() // the roster read a moment ago is not kept any more
	if got := put(body); !reflect.DeepEqual(got, []any{"m-0b0b0b", "m-0c0c0c", "m-0d0d0d", "m-dead00"}) {
		t.Fatalf("the roster not answering keeps every id: %v", got)
	}
}
