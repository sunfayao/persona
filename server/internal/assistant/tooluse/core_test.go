package tooluse

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

var rankNow = time.Date(2026, 12, 30, 9, 0, 0, 0, time.UTC)

func (f *fakeHost) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.vals[key]
	return ok
}

func (f *fakeHost) drop(key string) {
	f.mu.Lock()
	delete(f.vals, key)
	f.mu.Unlock()
}

// settled is a CoreInput whose static tools have been static, and whose runtime has been recorded, for longer than
// Window: every count can be compared.
func settled(static []string, usage map[string]int) CoreInput {
	in := CoreInput{Static: static, Seen: map[string]time.Time{}, From: rankNow.Add(-40 * 24 * time.Hour),
		Usage: map[string]Counts{}, Now: rankNow}
	for _, name := range static {
		in.Seen[name] = rankNow.Add(-60 * 24 * time.Hour)
	}
	for name, calls := range usage {
		in.Usage[name] = Counts{Calls: calls}
	}
	return in
}

// Under CoreStartCalls calls a runtime keeps the static list (no ranking); once ranked, it goes back only under
// CoreStopCalls.
func TestRankStartsWithEnoughUseAndStopsOnlyWellBelowIt(t *testing.T) {
	t.Parallel()
	static := []string{"a", "b"}
	if got := Rank(Ranking{}, settled(static, map[string]int{"a": 200, "b": CoreStartCalls - 201})); got.Tools != nil ||
		got.Calls != CoreStartCalls-1 || !got.At.Equal(rankNow) {
		t.Fatalf("one call short of CoreStartCalls: %+v", got)
	}
	ranked := Rank(Ranking{}, settled(static, map[string]int{"a": 200, "b": CoreStartCalls - 200}))
	if !slices.Equal(ranked.Tools, []string{"a", "b"}) {
		t.Fatalf("at CoreStartCalls: %+v", ranked)
	}
	if got := Rank(ranked, settled(static, map[string]int{"a": 100, "b": CoreStopCalls - 100})); !slices.Equal(got.Tools, ranked.Tools) {
		t.Fatalf("ranked, at CoreStopCalls: %+v", got)
	}
	if got := Rank(ranked, settled(static, map[string]int{"a": 100, "b": CoreStopCalls - 101})); got.Tools != nil {
		t.Fatalf("ranked, under CoreStopCalls: %+v", got)
	}
}

// With enough use the core tools are ranked by calls, ties by the static order (static tools first, then by name): a
// tool outside the static list called PromoteCalls times joins them, and a static one not called in a whole Window
// leaves.
func TestRankPromotesByCallsAndDemotesTheUncalled(t *testing.T) {
	t.Parallel()
	static := []string{"a", "b", "c", "d"}
	got := Rank(Ranking{}, settled(static, map[string]int{"b": 150, "a": 100, "d": 50, "x": 50, "w": 50, "y": PromoteCalls,
		"z": PromoteCalls - 1}))
	if want := []string{"b", "a", "d", "w", "x", "y"}; !slices.Equal(got.Tools, want) {
		t.Fatalf("ranked %q, want %q", got.Tools, want)
	}
}

// A tool is not demoted for calls it could not have had. A static tool that joined the static list within Window
// stays whatever its calls, and keeps at least its static place, which its calls can only improve; so does every
// static tool while the runtime's record is younger than Window. A whole Window later, an uncalled one leaves.
func TestRankDoesNotDemoteAToolForCallsItCouldNotHaveHad(t *testing.T) {
	t.Parallel()
	static := []string{"a", "new", "b", "c", "hot"}
	in := settled(static, map[string]int{"a": 100, "b": 90, "c": 80, "x": 200, "hot": 150})
	in.Seen["new"], in.Seen["hot"] = rankNow.Add(-2*24*time.Hour), rankNow.Add(-2*24*time.Hour)
	got := Rank(Ranking{}, in)
	// new: no call, at its static place (1); hot: its calls place it above its static place (4).
	if want := []string{"x", "new", "hot", "a", "b", "c"}; !slices.Equal(got.Tools, want) {
		t.Fatalf("joined within Window: %q, want %q", got.Tools, want)
	}
	delete(in.Seen, "new") // first seen now
	if got := Rank(Ranking{}, in); !slices.Contains(got.Tools, "new") {
		t.Fatalf("joined now: %q", got.Tools)
	}

	young := settled(static, map[string]int{"a": 300})
	young.From = rankNow.Add(-10 * 24 * time.Hour)
	if got := Rank(Ranking{}, young); !slices.Equal(got.Tools, static) {
		t.Fatalf("a record of 10 days: %q, want every static tool", got.Tools)
	}
	young.From = time.Time{} // no record at all is no whole record either
	if got := Rank(Ranking{Tools: static}, young); len(got.Tools) != len(static) {
		t.Fatalf("no record: %q", got.Tools)
	}

	old := settled(static, map[string]int{"a": 300})
	if got := Rank(Ranking{Tools: static}, old); !slices.Equal(got.Tools, []string{"a"}) {
		t.Fatalf("a whole Window without calls: %q, want only a", got.Tools)
	}

	moved := settled([]string{"new", "a", "b"}, map[string]int{"a": 300, "b": 100})
	moved.Seen["new"] = rankNow.Add(-2 * 24 * time.Hour)
	if got := Rank(Ranking{Tools: []string{"a", "b", "new"}}, moved); !slices.Equal(got.Tools, []string{"new", "a", "b"}) {
		t.Fatalf("a joined tool moved up the static list: %q, want it at its new place", got.Tools)
	}
}

// The list does not flap: counts that drift keep the last order, and only a clear overtaking reorders it; a core tool
// stays while it is called at all, and a demoted one comes back only with PromoteCalls calls.
func TestRankHysteresis(t *testing.T) {
	t.Parallel()
	static := []string{"a", "b", "c"}
	last := Ranking{Tools: []string{"a", "b", "c", "x"}}
	drift := settled(static, map[string]int{"a": 100, "b": 140, "c": 90, "x": 1})
	if got := Rank(last, drift); !slices.Equal(got.Tools, last.Tools) {
		t.Fatalf("b drifted above a: %q", got.Tools)
	}
	overtake := settled(static, map[string]int{"a": 100, "b": 160, "c": 90, "x": 1})
	if got := Rank(last, overtake); !slices.Equal(got.Tools, []string{"b", "a", "c", "x"}) {
		t.Fatalf("b overtook a: %q", got.Tools)
	}
	few := Ranking{Tools: []string{"a", "b", "c"}}
	if got := Rank(few, settled(static, map[string]int{"a": 300, "b": 2, "c": 4})); !slices.Equal(got.Tools, few.Tools) {
		t.Fatalf("4 calls over 2 is no overtaking (overtakeMargin): %q", got.Tools)
	}
	if got := Rank(few, settled(static, map[string]int{"a": 300, "b": 1, "c": 4})); !slices.Equal(got.Tools, []string{"a", "c", "b"}) {
		t.Fatalf("4 calls over 1 is: %q", got.Tools)
	}

	demoted := Ranking{Tools: []string{"a", "b"}}
	back := settled(static, map[string]int{"a": 300, "b": 50, "c": PromoteCalls - 1})
	if got := Rank(demoted, back); !slices.Equal(got.Tools, demoted.Tools) {
		t.Fatalf("a demoted tool with %d calls: %q", PromoteCalls-1, got.Tools)
	}
	back.Usage["c"] = Counts{Calls: PromoteCalls}
	if got := Rank(demoted, back); !slices.Equal(got.Tools, []string{"a", "b", "c"}) {
		t.Fatalf("a demoted tool with %d calls: %q", PromoteCalls, got.Tools)
	}
}

// Core tools are taken in rank order while they fit the budget; one that does not fit is passed over for smaller ones
// after it, and a tool the conversation does not list is not core. No budget takes every listed one.
func TestCoreRanksFitTheBudget(t *testing.T) {
	t.Parallel()
	order := []string{"a", "b", "gone", "c", "d", "e"}
	listed := map[string]int{"a": 400, "b": 300, "c": 500, "d": 100, "e": 50, "other": 10}
	if got, want := CoreRanks(order, listed, 800), map[string]int{"a": 1, "b": 2, "d": 5}; !equalRanks(got, want) {
		t.Fatalf("budget 800: %v, want %v", got, want)
	}
	if got, want := CoreRanks(order, listed, 0), map[string]int{"a": 1, "b": 2, "c": 4, "d": 5, "e": 6}; !equalRanks(got, want) {
		t.Fatalf("no budget: %v, want %v", got, want)
	}
}

func equalRanks(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// A tool weighs what Antigravity's proxy weighs: its name, description and schema as compact JSON, with no HTML escape.
func TestDeclaredSizeIsTheCompactJSONTheProxyWeighs(t *testing.T) {
	t.Parallel()
	want := `{"name":"t","description":"a <b> & “c”","inputSchema":{"type":"object","required":["x"]}}`
	if got := DeclaredSize("t", "a <b> & “c”", json.RawMessage("{\n  \"type\": \"object\",\n  \"required\": [\"x\"]\n}")); got != len(want) {
		t.Fatalf("size %d, want %d", got, len(want))
	}
	empty := `{"name":"t","description":"","inputSchema":{"type":"object","properties":{}}}`
	if got := DeclaredSize("t", "", nil); got != len(empty) {
		t.Fatalf("no schema: size %d, want %d", got, len(empty))
	}
}

// A runtime's ranking is computed at most once every CoreEvery, stored, and loaded again by the next book: a restart
// keeps it. An unranked runtime is stored only when the static list changed.
func TestRefreshCoreIsDailyAndSurvivesARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeHost()
	b := New(f.host())
	static := []string{"a", "b", "c"}
	if changed, err := b.RefreshCore(ctx, "", static); changed || err != nil {
		t.Fatalf("no runtime: %v, %v", changed, err)
	}
	for range 100 {
		b.Record(Call{Tool: "a", Runtime: "agy", OK: true})
	}
	if changed, err := b.RefreshCore(ctx, "agy", static); changed || err != nil || b.CoreOrder("agy") != nil {
		t.Fatalf("too little use: %v, %v, %q", changed, err, b.CoreOrder("agy"))
	}
	if !f.has(CoreKey) {
		t.Fatal("the first static list seen was not stored")
	}
	f.drop(CoreKey)
	for range 200 {
		b.Record(Call{Tool: "b", Runtime: "agy", OK: true})
	}
	if changed, _ := b.RefreshCore(ctx, "agy", static); changed || f.has(CoreKey) {
		t.Fatalf("computed again within CoreEvery (changed %v), or an unchanged unranked runtime stored", changed)
	}
	f.advance(CoreEvery)
	changed, err := b.RefreshCore(ctx, "agy", static)
	if want := []string{"b", "a", "c"}; !changed || err != nil || !slices.Equal(b.CoreOrder("agy"), want) {
		t.Fatalf("ranked: %v, %v, %q, want %q", changed, err, b.CoreOrder("agy"), want)
	}
	next := New(f.host())
	next.Load(ctx)
	if !slices.Equal(next.CoreOrder("agy"), b.CoreOrder("agy")) || next.CoreOrder("claude") != nil {
		t.Fatalf("after a restart: agy %q, claude %q", next.CoreOrder("agy"), next.CoreOrder("claude"))
	}
	f.advance(CoreEvery)
	if changed, err := b.RefreshCore(ctx, "agy", static); changed || err != nil {
		t.Fatalf("the same use a day later: %v, %v", changed, err)
	}
}

// A tool added to the static list while a runtime is ranked joins its core tools at once, uncalled, at its static
// place; it is judged by its calls only once it has been listed for a whole Window, and leaves if it had none.
func TestRefreshCoreGivesAToolAddedToTheStaticListAWholeWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeHost()
	b := New(f.host())
	record := func(calls map[string]int) {
		for tool, n := range calls {
			for range n {
				b.Record(Call{Tool: tool, Runtime: "agy", OK: true})
			}
		}
	}
	record(map[string]int{"a": 150, "b": 100, "c": 60})
	if _, err := b.RefreshCore(ctx, "agy", []string{"a", "b", "c"}); err != nil || !slices.Equal(b.CoreOrder("agy"), []string{"a", "b", "c"}) {
		t.Fatalf("ranked: %q, %v", b.CoreOrder("agy"), err)
	}
	f.advance(30 * 24 * time.Hour)
	static := []string{"a", "added", "b", "c"}
	if changed, err := b.RefreshCore(ctx, "agy", static); !changed || err != nil || !slices.Equal(b.CoreOrder("agy"), static) {
		t.Fatalf("added: %v, %v, %q", changed, err, b.CoreOrder("agy"))
	}
	f.advance(24 * time.Hour)
	record(map[string]int{"a": 100, "b": 60, "c": 20})
	f.advance(30 * 24 * time.Hour)
	if changed, err := b.RefreshCore(ctx, "agy", static); !changed || err != nil || !slices.Equal(b.CoreOrder("agy"), []string{"a", "b", "c"}) {
		t.Fatalf("a whole Window listed without a call: %v, %v, %q", changed, err, b.CoreOrder("agy"))
	}
}
