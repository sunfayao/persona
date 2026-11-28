package tooluse

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// The adaptive core tools. A conversation's core tools, the ones its runtime loads up front (the root's
// withToolLoading), follow what the main chat's runtime calls: once it has made CoreStartCalls calls in Window, they
// are its tools ranked by use (Rank), else the root's static list, as measured. A ranking is computed at most once
// every CoreEvery, stored under CoreKey so that a restart keeps it, and moves only past thresholds: the list does not
// flap.

const (
	// CoreKey stores each runtime's ranking, and when each static core tool was first seen.
	CoreKey = "assistant.core_tools_adaptive"
	// CoreEvery is how often a runtime's ranking may be computed.
	CoreEvery = 24 * time.Hour
	// CoreStartCalls is the use a runtime needs in Window before its own ranking replaces the static list. The main
	// conversation on Antigravity made about 1,000 Persona calls in 30 days (2026-11-27). At 300, a tool called
	// PromoteCalls times is 1.7% of the use, about where the measured static list stopped; with fewer calls a tool's
	// place is mostly chance.
	CoreStartCalls = 300
	// CoreStopCalls is the use below which a ranked runtime goes back to the static list: half of CoreStartCalls, so
	// a quiet month does not switch the list back and forth.
	CoreStopCalls = 150
	// PromoteCalls is how often a tool outside the core tools is called in Window to join them: the bar of the
	// measured static list (tools called at least 5 times in 30 days). A core tool stays while it is called at all.
	PromoteCalls = 5
	// overtakeMargin is the least lead in calls by which a tool reorders a kept ranking (overtaken).
	overtakeMargin = 3
)

// Ranking is one runtime's adaptive core tools.
type Ranking struct {
	// Tools are the core tools, the most called first; none while the runtime's use is too little to rank, when the
	// static list applies.
	Tools []string `json:"tools,omitempty"`
	// Calls are the runtime's calls in Window when it was computed.
	Calls int `json:"calls"`
	// At is when it was computed.
	At time.Time `json:"at"`
}

type coreDoc struct {
	Runtimes map[string]Ranking `json:"runtimes"`
	// Static is when each tool of the static list was first seen in it (stamps): zero for the ones listed at the first
	// RefreshCore, which are as old as the record, so their counts compare with any other's.
	Static map[string]time.Time `json:"static,omitempty"`
}

// coreState is the Book's copy of what CoreKey stores; Load reads it, RefreshCore changes it.
type coreState struct {
	mu  sync.Mutex
	doc coreDoc
}

// CoreInput is what a runtime's ranking is computed from.
type CoreInput struct {
	// Static is the measured default list, the most called first (the root's coreTools); its order breaks ties.
	Static []string
	// Seen is when each static tool was first seen in Static; one missing has just joined it.
	Seen map[string]time.Time
	// From is the first day of the runtime's record in Window (zero: none); Usage its calls in Window.
	From  time.Time
	Usage map[string]Counts
	Now   time.Time
}

// Rank is a runtime's next ranking after prev, its last one.
//
// Cold start: under CoreStartCalls calls the ranking is empty and the static list applies; a ranked runtime goes
// back to it only under CoreStopCalls.
//
// Members: every tool called PromoteCalls times; every tool of the last ranking (of the static list, coming from a cold
// start) that is still called; and the static tools that could not have been called for a whole Window: every one that
// joined the static list within Window, and every one of the last ranking while the runtime's record is younger than
// Window. A tool is not demoted for calls it could not have had: a static tool leaves only after a whole Window without
// a call, and comes back as any other tool does, with PromoteCalls calls.
//
// Order: by calls, ties by the static order, then by name; a static tool that joined the static list within Window
// keeps at least its place in that list, which is the judgment of it there is until its calls can be compared. The last
// ranking's order stays while its members are the same, each such tool still holds its place, and no tool has clearly
// overtaken one above it (more than half again as many calls, and at least overtakeMargin more), so counts that drift
// from day to day reorder nothing.
func Rank(prev Ranking, in CoreInput) Ranking {
	next := Ranking{At: in.Now.UTC()}
	for _, c := range in.Usage {
		next.Calls += c.Calls
	}
	ranked := len(prev.Tools) > 0
	if next.Calls < CoreStopCalls || !ranked && next.Calls < CoreStartCalls {
		return next
	}
	calls := func(name string) int { return in.Usage[name].Calls }
	place := make(map[string]int, len(in.Static))
	for i, name := range in.Static {
		if _, ok := place[name]; !ok {
			place[name] = i
		}
	}
	horizon := in.Now.Add(-Window)
	joined := func(name string) bool { // a static tool that joined the static list within Window
		_, static := place[name]
		seen, ok := in.Seen[name]
		return static && (!ok || seen.After(horizon))
	}
	whole := !in.From.IsZero() && !in.From.After(horizon) // the runtime's record covers Window
	last := prev.Tools
	if !ranked {
		last = in.Static
	}
	member := map[string]bool{}
	for _, name := range last {
		_, static := place[name]
		if calls(name) > 0 || static && (!whole || joined(name)) {
			member[name] = true
		}
	}
	for name, c := range in.Usage {
		if c.Calls >= PromoteCalls {
			member[name] = true
		}
	}
	for name := range place {
		if joined(name) {
			member[name] = true
		}
	}
	before := func(a, b string) int {
		if c := cmp.Compare(calls(b), calls(a)); c != 0 {
			return c
		}
		pa, staticA := place[a]
		pb, staticB := place[b]
		switch {
		case staticA && staticB:
			return cmp.Compare(pa, pb)
		case staticA != staticB:
			if staticA {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	}
	order := make([]string, 0, len(member))
	for name := range member {
		order = append(order, name)
	}
	slices.SortFunc(order, before)
	// In the static order, so that a tool moved up later never pushes one moved before it below its place.
	for _, name := range in.Static {
		if i, at := slices.Index(order, name), place[name]; i > at && joined(name) {
			order = slices.Insert(slices.Delete(order, i, i+1), at, name)
		}
	}
	if ranked && sameMembers(prev.Tools, order) && !stale(prev.Tools, calls, place, joined) {
		order = prev.Tools
	}
	next.Tools = order
	return next
}

// stale: order no longer stands. A tool has clearly overtaken one ranked above it, or a static tool that joined the
// static list within Window sits below its place there (the static list changed).
func stale(order []string, calls func(string) int, place map[string]int, joined func(string) bool) bool {
	least := -1 // the fewest calls of a tool above
	for i, name := range order {
		if joined(name) {
			if i > place[name] {
				return true
			}
			continue
		}
		c := calls(name)
		if least >= 0 && 2*c > 3*least && c-least >= overtakeMargin {
			return true
		}
		if least < 0 || c < least {
			least = c
		}
	}
	return false
}

func sameMembers(a, b []string) bool {
	return len(a) == len(b) && slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// CoreRanks are the core tools of a conversation's list, with their ranks: the tools of order that listed has (each
// listed tool's declared size, by name), each ranked by its place in order from 1, taken in that order while they fit
// budget bytes. A tool that does not fit is passed over for the smaller ones after it, as Antigravity's proxy does with
// what it declares. budget 0 is no limit.
func CoreRanks(order []string, listed map[string]int, budget int) map[string]int {
	ranks := make(map[string]int, len(order))
	room := budget
	for i, name := range order {
		size, ok := listed[name]
		if !ok || ranks[name] > 0 {
			continue
		}
		if budget > 0 {
			if size > room {
				continue
			}
			room -= size
		}
		ranks[name] = i + 1
	}
	return ranks
}

// DeclaredSize is what a runtime declares of a tool, in bytes: the JSON of its name, description and input schema,
// compact and with no HTML escaping, as Antigravity's MCP proxy weighs a tool (integrations/antigravity/src/
// mcp-proxy.mjs, size). No schema, or one that is not JSON, weighs as the empty object schema personad mcp declares.
func DeclaredSize(name, description string, schema json.RawMessage) int {
	var compact bytes.Buffer
	if len(schema) == 0 || json.Compact(&compact, schema) != nil {
		compact.Reset()
		compact.WriteString(`{"type":"object","properties":{}}`)
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}{name, description, compact.Bytes()})
	return out.Len() - 1 // Encode's newline
}

// CoreOrder is runtime's ranked core tools, the most called first, as last computed or stored; nil while it has none
// and the static list applies. Callers do not change the slice.
func (b *Book) CoreOrder(runtime string) []string {
	b.core.mu.Lock()
	defer b.core.mu.Unlock()
	return b.core.doc.Runtimes[runtime].Tools
}

// RefreshCore computes runtime's ranking again (Rank) when it has none or its last is CoreEvery old, from the static
// list static (the root's coreTools) and the record, stores it, and says whether its core tools changed. It is called
// after Load: a record not yet loaded has too few calls. A runtime that stays unranked is stored only when the static
// list changed.
func (b *Book) RefreshCore(ctx context.Context, runtime string, static []string) (bool, error) {
	if runtime == "" {
		return false, nil
	}
	now := b.now()
	b.core.mu.Lock()
	prev, ok := b.core.doc.Runtimes[runtime]
	seen := b.core.doc.Static
	b.core.mu.Unlock()
	if ok && now.Sub(prev.At) < CoreEvery {
		return false, nil
	}
	seen, stamped := stamps(seen, static, now)
	since := now.Add(-Window)
	next := Rank(prev, CoreInput{Static: static, Seen: seen, From: b.usageFrom(runtime, since),
		Usage: b.Usage(runtime, since), Now: now})
	b.core.mu.Lock()
	if b.core.doc.Runtimes == nil {
		b.core.doc.Runtimes = map[string]Ranking{}
	}
	b.core.doc.Runtimes[runtime] = next
	b.core.doc.Static = seen
	doc := coreDoc{Runtimes: maps.Clone(b.core.doc.Runtimes), Static: seen}
	b.core.mu.Unlock()
	changed := !slices.Equal(prev.Tools, next.Tools)
	if b.h.Save == nil || !changed && !stamped && len(next.Tools) == 0 {
		return changed, nil
	}
	return changed, b.h.Save(ctx, CoreKey, doc)
}

// stamps is when each tool of static was first seen in it, and whether that differs from old: as old says, else now;
// with no old stamps at all, the time before stamps (zero).
func stamps(old map[string]time.Time, static []string, now time.Time) (map[string]time.Time, bool) {
	out := make(map[string]time.Time, len(static))
	for _, name := range static {
		at, ok := old[name]
		if !ok && len(old) > 0 {
			at = now.UTC()
		}
		out[name] = at
	}
	return out, len(old) == 0 || !maps.EqualFunc(out, old, time.Time.Equal)
}

// usageFrom is the first day since since on which runtime has calls recorded; zero when there is none.
func (b *Book) usageFrom(runtime string, since time.Time) time.Time {
	first := since.UTC().Format(time.DateOnly)
	b.mu.Lock()
	defer b.mu.Unlock()
	from := ""
	for day, runtimes := range b.usage.Days {
		if day >= first && len(runtimes[runtime]) > 0 && (from == "" || day < from) {
			from = day
		}
	}
	t, _ := time.Parse(time.DateOnly, from)
	return t
}

// loadCore reads what CoreKey stores; Load calls it.
func (b *Book) loadCore(ctx context.Context) {
	var doc coreDoc
	if _, err := b.h.Load(ctx, CoreKey, &doc); err != nil {
		b.logf("tool use: read core tools: %v", err)
		return
	}
	b.core.mu.Lock()
	b.core.doc = doc
	b.core.mu.Unlock()
}
