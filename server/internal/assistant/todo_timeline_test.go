package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
)

// GET /api/v1/todos/{id} carries the rounds the paper shows, how many there are, its results and its recap,
// all read from the manager's feed items, decisions, runs and sessions.

// todoHistory is a to-do a run works on (server/s1, working) with a history: three instructions of the owner's, a turn
// of the run after the first (a receipt from before #10465, naming its computer only, with a PR), a rework long ago
// (a Background round, on its day in the owner's zone, Asia/Tokyo) and, with a database, a decision of the owner's.
// It answers the to-do's id and its rounds' ids, oldest first.
func todoHistory(t *testing.T, e *todoEnv, database *db.DB) (string, []string) {
	t.Helper()
	ctx := context.Background()
	setSettings(t, e.s, func(v *Settings) { v.Timezone = "Asia/Tokyo" })
	began := time.Now().UTC().Add(-time.Minute)
	e.sessions("server", proto.Session{SessionID: "s1", Title: "Hedge trimming", Status: proto.StatusWorking,
		State: proto.StateWorking, StateSince: began, UpdatedAt: began, Activity: "measuring the hedge"})
	id := e.create("Prune the hedge").ID
	e.must("PUT", "/api/v1/todos/"+id+"/agents/server/s1", "")
	rounds := []string{"start"}
	if database != nil {
		var d struct{ Decision db.TodoDecisionRow }
		_ = json.Unmarshal(e.must("POST", "/api/v1/todos/"+id+"/decisions", `{"question":"Which side first?","choice":"The street side"}`).Body.Bytes(), &d)
		rounds = append(rounds, "r:dec:"+d.Decision.ID)
	}
	t0 := time.Now().UTC().Add(time.Minute)
	add := func(kind string, body any, at time.Time) string {
		t.Helper()
		it, err := e.s.appendFeedAt(ctx, "conv1", kind, id, body, at)
		if err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	add(feedReview, reviewFeedBody{Title: "Prune the hedge", Verdict: reviewRework, Round: 1, Note: "the corner is uneven"},
		time.Date(2026, 1, 15, 20, 0, 0, 0, time.UTC)) // 2026-01-16 in Tokyo
	ask := map[string]string{"machine_id": "server", "session_id": "s1"}
	for i, text := range []string{"trim the street side first", "then the corner", "and sweep up after"} {
		body := map[string]string{"text": text}
		for k, v := range ask {
			body[k] = v
		}
		rounds = append(rounds, "r:feed:"+add(feedRouted, body, t0.Add(time.Duration(2*i)*time.Minute)))
		if i == 0 {
			add(feedReceipt, receiptBody{Title: "Prune the hedge", State: "done", Machine: "server", Summary: "Trimmed the street side.",
				PR: "https://github.com/o/garden/pull/7"}, t0.Add(time.Minute))
		}
	}
	rounds = slices.Insert(rounds, 1, "bg:2026-01-16") // after start, before every round that began later
	return id, rounds
}

func roundIDs(rounds []assistantwire.TodoRound) []string {
	out := make([]string, len(rounds))
	for i, r := range rounds {
		out[i] = r.ID
	}
	return out
}

// The item route: the row as the list has it, with the rounds the paper shows (the latest two that are not Background
// ones), how many there are, the results and what changed since the owner looked, with a stable response across reads.
func TestTodoTimelineOnTheItem(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, database *db.DB) {
		id, want := todoHistory(t, e, database)
		e.must("PUT", "/api/v1/todos/"+id+"/seen", "") // before the instructions: the run's turn is news
		body := e.must("GET", "/api/v1/todos/"+id, "").Body.Bytes()
		var item struct {
			Todo json.RawMessage `json:"todo"`
			assistantwire.TodoItemDetail
		}
		if err := json.Unmarshal(body, &item); err != nil || item.RoundsTotal == nil {
			t.Fatalf("%v %s", err, body)
		}
		var row listedTodo
		if err := json.Unmarshal(item.Todo, &row); err != nil || row.ID != id || row.View == nil {
			t.Fatalf("the row: %v %s", err, item.Todo)
		}
		if got := roundIDs(item.Rounds); !slices.Equal(got, want[len(want)-2:]) || *item.RoundsTotal != len(want) {
			t.Fatalf("rounds %v of %d, want %v of %d", got, *item.RoundsTotal, want[len(want)-2:], len(want))
		}
		if len(item.Results) != 1 || item.Results[0].Kind != assistantwire.TodoResultKindChange ||
			item.Results[0].URL != "https://github.com/o/garden/pull/7" {
			t.Errorf("results: %+v", item.Results)
		}
		if r := item.Recap; r == nil || r.Round != want[len(want)-3] || r.Counts[assistantwire.TodoEntryTypeRun] != 1 {
			t.Errorf("recap: %+v", item.Recap)
		}
		if again := e.must("GET", "/api/v1/todos/"+id, "").Body.Bytes(); !bytes.Equal(again, body) {
			t.Fatalf("unchanged state read twice:\n%s\n%s", body, again)
		}
	})
}

// A to-do with no history yet answers rounds [] and rounds_total 0, which a client tells apart from an older manager
// that sends neither; no recap and no results.
func TestTodoTimelineOnAnItemWithNoHistory(t *testing.T) {
	e := newTodoEnv(t, nil)
	id := e.create("Water the roses").ID
	var item map[string]json.RawMessage
	body := e.must("GET", "/api/v1/todos/"+id, "").Body.Bytes()
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatal(err)
	}
	if string(item["rounds"]) != "[]" || string(item["rounds_total"]) != "0" || item["recap"] != nil || item["results"] != nil {
		t.Fatalf("%s", body)
	}

}

// When the to-do's history cannot be read (here a store without the feed), the item route still answers the row,
// without the history fields.
func TestTodoTimelineWithoutItsStores(t *testing.T) {
	e := &todoEnv{t: t, f: newFakeHub().withMachine()}
	e.s = newTestService(t, e.f) // memStore: no assistant_feed
	id := e.create("Order compost").ID
	var item map[string]json.RawMessage
	body := e.must("GET", "/api/v1/todos/"+id, "").Body.Bytes()
	if err := json.Unmarshal(body, &item); err != nil || item["todo"] == nil {
		t.Fatalf("%v %s", err, body)
	}
	for _, k := range []string{"rounds", "rounds_total", "recap", "results"} {
		if item[k] != nil {
			t.Errorf("%s with no readable history: %s", k, body)
		}
	}

}

// rowsAs is every row of GET /api/v1/todos as role reads it ("" a device), by id, as JSON values, and the answer's
// bytes.
func rowsAs(t *testing.T, e *todoEnv, role string) (map[string]map[string]any, []byte) {
	t.Helper()
	w := e.doAs(role, "GET", "/api/v1/todos", "")
	var list struct {
		Todos []map[string]any `json:"todos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != http.StatusOK {
		t.Fatalf("the list as %q: %d %s", role, w.Code, w.Body)
	}
	out := map[string]map[string]any{}
	for _, r := range list.Todos {
		out[r["id"].(string)] = r
	}
	return out, w.Body.Bytes()
}
