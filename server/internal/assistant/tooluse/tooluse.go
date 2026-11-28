// Package tooluse records how the assistant's conversations use their tools, with no argument value in it: how often
// each tool is called on each runtime (the usage that orders the core tools), and the friction, the calls that failed
// and the retry with other arguments that worked. Repeated friction becomes drafts, a field note or a fix suggestion,
// that wait for the main conversation or the user to review them. The assistant Service lends it what it needs through
// Host.
package tooluse

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// The stored values, in the assistant's settings.
const (
	UsageKey    = "assistant.tool_usage"           // calls per day, runtime and tool
	FrictionKey = "assistant.tool_friction"        // the failed calls of the last Window
	DraftsKey   = "assistant.tool_friction_drafts" // what the friction loop proposes from them
)

const (
	// Window is how long usage and friction are kept.
	Window = 30 * 24 * time.Hour
	// MaxEvents bounds the friction kept, the newest first kept.
	MaxEvents = 500
	// RetryWindow is how soon after a failure a successful call of the same tool in the same conversation counts as
	// its retry.
	RetryWindow = 10 * time.Minute
)

// Host is what the package takes from the assistant Service.
type Host struct {
	// Load reads the stored value of key into out; false when there is none.
	Load func(ctx context.Context, key string, out any) (bool, error)
	// Save stores v under key.
	Save func(ctx context.Context, key string, v any) error
	// Now is the clock.
	Now func() time.Time
	// Logf logs what could not be read or saved.
	Logf func(format string, args ...any)
	// Canonical is the name a tool's recorded calls count under (Usage), the tool that stands for an older one merged
	// into it; nil counts each as recorded.
	Canonical func(name string) string
}

// Call is one call of a tool by an assistant conversation, as the book keeps it.
type Call struct {
	Tool string
	// Runtime is the calling conversation's: claude, codex, agy; "" when it is not known.
	Runtime string
	// Machine and Session are the calling conversation's: evidence of where friction happened, never its content.
	Machine, Session string
	// Skeleton is Skeleton of the call's arguments.
	Skeleton string
	OK       bool
	// Code is the failure's code or category ("" for a success); Kind its proto.ToolResult.ErrorKind.
	Code, Kind string
}

// Counts are one tool's calls, and how many of them failed.
type Counts struct {
	Calls  int `json:"calls"`
	Errors int `json:"errors,omitempty"`
}

// Event is one failed call kept as friction.
type Event struct {
	At       time.Time `json:"at"`
	Tool     string    `json:"tool"`
	Runtime  string    `json:"runtime,omitempty"`
	Code     string    `json:"code"`
	Skeleton string    `json:"skeleton"`
	Machine  string    `json:"machine,omitempty"`
	Session  string    `json:"session,omitempty"`
	// RetrySkeleton: the same conversation called the tool again within RetryWindow with other arguments, and that
	// call worked; this is its skeleton.
	RetrySkeleton string `json:"retry_skeleton,omitempty"`
	// Serial tells the events apart while the book runs (pairing a retry with its failure).
	Serial int64 `json:"-"`
}

// WorkerRuntime is the runtime the calls of Agents (not the main conversation's) are counted under: their use is
// counted, and nothing else of them is kept (no friction, no retry pairing).
const WorkerRuntime = "agent"

// notFriction are the failures that say nothing of how a tool is used: the user declined, a rate limit, the computer
// or the connector away.
var notFriction = []string{"declined", "rate_limited", "unavailable"}

type usageDoc struct {
	// Days holds the counts by day (UTC, "2006-01-02"), runtime ("" unknown) and tool.
	Days map[string]map[string]map[string]Counts `json:"days"`
}

type frictionDoc struct {
	Events []Event `json:"events"`
}

type pairKey struct{ machine, session, tool string }

type lastFailure struct {
	at       time.Time
	skeleton string
	serial   int64
}

// Book is the record of tool use. Record is cheap and never blocks on the store; Flush writes what changed.
type Book struct {
	h Host

	mu      sync.Mutex
	loaded  bool
	usage   usageDoc
	events  []Event
	serial  int64
	last    map[pairKey]lastFailure
	changes int64 // increases with every change; Flush compares it with what it wrote
	written int64

	// draftsMu orders the read-modify-writes of the stored drafts (drafts.go), the friction dream's too (dream.go).
	draftsMu sync.Mutex
	dream    dreamState // guarded by draftsMu
	core     coreState  // the adaptive core tools (core.go)
}

// New makes a book; Load reads what was stored before.
func New(h Host) *Book {
	return &Book{h: h, usage: usageDoc{Days: map[string]map[string]map[string]Counts{}}, last: map[pairKey]lastFailure{}}
}

func (b *Book) now() time.Time {
	if b.h.Now != nil {
		return b.h.Now()
	}
	return time.Now()
}

func (b *Book) logf(format string, args ...any) {
	if b.h.Logf != nil {
		b.h.Logf(format, args...)
	}
}

// Load reads the stored usage, friction and core tools once, adding what was recorded before it; later calls do
// nothing. A value that cannot be read is logged and left out.
func (b *Book) Load(ctx context.Context) {
	b.mu.Lock()
	done := b.loaded
	b.loaded = true
	b.mu.Unlock()
	if done || b.h.Load == nil {
		return
	}
	var usage usageDoc
	if _, err := b.h.Load(ctx, UsageKey, &usage); err != nil {
		b.logf("tool use: read usage: %v", err)
	}
	var friction frictionDoc
	if _, err := b.h.Load(ctx, FrictionKey, &friction); err != nil {
		b.logf("tool use: read friction: %v", err)
	}
	b.loadCore(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	for day, runtimes := range usage.Days {
		for runtime, tools := range runtimes {
			for tool, c := range tools {
				b.add(day, runtime, tool, c)
			}
		}
	}
	stored := friction.Events
	for i := range stored {
		b.serial++
		stored[i].Serial = b.serial
	}
	b.events = append(stored, b.events...)
	b.prune(b.now())
}

// Record keeps one call: its count, and when it failed in a way that tells of how the tool is used, an event. A
// successful call that follows a failure of the same tool in the same conversation within RetryWindow, with another
// skeleton, marks that failure as fixed by it.
func (b *Book) Record(c Call) {
	if c.Tool == "" {
		return
	}
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	counts := Counts{Calls: 1}
	if !c.OK {
		counts.Errors = 1
	}
	b.add(now.UTC().Format(time.DateOnly), c.Runtime, c.Tool, counts)
	b.changes++
	if c.Runtime == WorkerRuntime {
		return
	}
	key := pairKey{c.Machine, c.Session, c.Tool}
	if c.OK {
		if f, ok := b.last[key]; ok {
			delete(b.last, key)
			if now.Sub(f.at) <= RetryWindow && f.skeleton != c.Skeleton {
				for i := len(b.events) - 1; i >= 0; i-- {
					if b.events[i].Serial == f.serial {
						b.events[i].RetrySkeleton = c.Skeleton
						break
					}
				}
			}
		}
		return
	}
	if slices.Contains(notFriction, c.Kind) {
		return
	}
	b.serial++
	b.events = append(b.events, Event{At: now.UTC(), Tool: c.Tool, Runtime: c.Runtime, Code: NormalizeCode(c.Code),
		Skeleton: c.Skeleton, Machine: c.Machine, Session: c.Session, Serial: b.serial})
	if c.Session != "" {
		b.last[key] = lastFailure{at: now, skeleton: c.Skeleton, serial: b.serial}
	}
	b.prune(now)
}

func (b *Book) add(day, runtime, tool string, c Counts) {
	runtimes := b.usage.Days[day]
	if runtimes == nil {
		runtimes = map[string]map[string]Counts{}
		b.usage.Days[day] = runtimes
	}
	tools := runtimes[runtime]
	if tools == nil {
		tools = map[string]Counts{}
		runtimes[runtime] = tools
	}
	old := tools[tool]
	tools[tool] = Counts{Calls: old.Calls + c.Calls, Errors: old.Errors + c.Errors}
}

// prune drops what is older than Window, the oldest events past MaxEvents, and retries no longer waited for.
func (b *Book) prune(now time.Time) {
	oldest := now.Add(-Window)
	for day := range b.usage.Days {
		if t, err := time.Parse(time.DateOnly, day); err != nil || t.Before(oldest.UTC().Truncate(24*time.Hour)) {
			delete(b.usage.Days, day)
		}
	}
	b.events = slices.DeleteFunc(b.events, func(e Event) bool { return e.At.Before(oldest) })
	if extra := len(b.events) - MaxEvents; extra > 0 {
		b.events = slices.Delete(b.events, 0, extra)
	}
	for k, f := range b.last {
		if now.Sub(f.at) > RetryWindow {
			delete(b.last, k)
		}
	}
}

// Flush stores the usage and the friction when something changed since the last Flush, pruned to Window. The store is
// written outside the book's lock: Record does not wait for it.
func (b *Book) Flush(ctx context.Context) error {
	if b.h.Save == nil {
		return nil
	}
	b.mu.Lock()
	if b.changes == b.written {
		b.mu.Unlock()
		return nil
	}
	b.prune(b.now())
	gen := b.changes
	usage := usageDoc{Days: make(map[string]map[string]map[string]Counts, len(b.usage.Days))}
	for day, runtimes := range b.usage.Days {
		r := make(map[string]map[string]Counts, len(runtimes))
		for runtime, tools := range runtimes {
			r[runtime] = cloneMap(tools)
		}
		usage.Days[day] = r
	}
	friction := frictionDoc{Events: slices.Clone(b.events)}
	b.mu.Unlock()
	if err := b.h.Save(ctx, UsageKey, usage); err != nil {
		return err
	}
	if err := b.h.Save(ctx, FrictionKey, friction); err != nil {
		return err
	}
	b.mu.Lock()
	b.written = max(b.written, gen)
	b.mu.Unlock()
	return nil
}

func cloneMap(m map[string]Counts) map[string]Counts {
	out := make(map[string]Counts, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Usage is each tool's calls since since (whole days, UTC) on runtime; "" counts every runtime.
func (b *Book) Usage(runtime string, since time.Time) map[string]Counts {
	first := since.UTC().Format(time.DateOnly)
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]Counts{}
	for day, runtimes := range b.usage.Days {
		if day < first {
			continue
		}
		for r, tools := range runtimes {
			if runtime != "" && r != runtime {
				continue
			}
			for tool, c := range tools {
				if b.h.Canonical != nil {
					tool = b.h.Canonical(tool)
				}
				old := out[tool]
				out[tool] = Counts{Calls: old.Calls + c.Calls, Errors: old.Errors + c.Errors}
			}
		}
	}
	return out
}

// Friction is a copy of the failed calls since since, oldest first.
func (b *Book) Friction(since time.Time) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, 0, len(b.events))
	for _, e := range b.events {
		if !e.At.Before(since) {
			out = append(out, e)
		}
	}
	return out
}

// NormalizeCode is a failure's code as it is kept: lower case, letters, digits, '_', '-' and '.', at most 80 bytes;
// "error" when nothing of it is left.
func NormalizeCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	var b strings.Builder
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('_')
		}
		if b.Len() >= 80 {
			break
		}
	}
	if b.Len() == 0 {
		return "error"
	}
	return b.String()
}
