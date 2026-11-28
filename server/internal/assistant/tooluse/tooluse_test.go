package tooluse

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHost is a store in memory and a clock the test moves.
type fakeHost struct {
	mu   sync.Mutex
	vals map[string][]byte
	now  time.Time
	fail error // Save fails with it
}

func newFakeHost() *fakeHost {
	return &fakeHost{vals: map[string][]byte{}, now: time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)}
}

func (f *fakeHost) host() Host {
	return Host{
		Load: func(_ context.Context, key string, out any) (bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			b, ok := f.vals[key]
			if !ok {
				return false, nil
			}
			return true, json.Unmarshal(b, out)
		},
		Save: func(_ context.Context, key string, v any) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.fail != nil {
				return f.fail
			}
			b, err := json.Marshal(v)
			f.vals[key] = b
			return err
		},
		Now: func() time.Time {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.now
		},
	}
}

func (f *fakeHost) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// A skeleton keeps the shape of the arguments and no value: not a string, a number, a key that is not a schema's
// field name (an environment variable, a header, a path), nor anything past the depth or the size it keeps.
func TestSkeletonHoldsNoValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ args, want string }{
		{`{"steps":[{"launch_app":{"name":"Calculator"}}]}`, `{steps:[{launch_app:{name:str}}]}`},
		{`{"script":"await cua.launch(\"Calculator\")","machine":"box"}`, `{machine:str,script:str}`},
		{`{"steps":[{"click":{"x":1}},{"click":{"x":2}},{"type_text":{"text":"secret words"}}]}`,
			`{steps:[{click:{x:num}}|{type_text:{text:str}}]}`},
		{`{"env":{"OPENAI_API_KEY":"sk-x","X-Token":"t","/home/me/file":"p"}}`, `{env:{*:str}}`},
		{`{"a":{"b":{"c":{"d":{"e":1}}}}}`, `{a:{b:{c:{d:{...}}}}}`},
		{`{"done":true,"n":null,"list":[]}`, `{done:bool,list:[],n:null}`},
		{``, `{}`},
		{`not json`, `invalid`},
	} {
		got := Skeleton(json.RawMessage(tc.args))
		if got != tc.want {
			t.Errorf("Skeleton(%s) = %s, want %s", tc.args, got, tc.want)
		}
		for _, value := range []string{"Calculator", "secret", "sk-x", "home", "OPENAI", "box", "await"} {
			if strings.Contains(got, value) {
				t.Errorf("Skeleton(%s) = %s holds %q", tc.args, got, value)
			}
		}
	}
	long := `{"items":[` + strings.Repeat(`{"`+strings.Repeat("k", 30)+`":1},`, 40) + `{}]}`
	if got := Skeleton(json.RawMessage(long)); len(got) > skeletonMax {
		t.Errorf("a long skeleton is %d bytes, more than %d", len(got), skeletonMax)
	}
}

// Usage counts every call per day, runtime and tool; what is older than the window goes.
func TestUsageCountsPerRuntimeWithinTheWindow(t *testing.T) {
	t.Parallel()
	f := newFakeHost()
	b := New(f.host())
	b.Record(Call{Tool: "agents_list", Runtime: "agy", OK: true})
	b.Record(Call{Tool: "agents_list", Runtime: "agy", OK: false, Code: "x"})
	b.Record(Call{Tool: "agents_list", Runtime: "claude", OK: true})
	b.Record(Call{Tool: "todo_get", Runtime: "agy", OK: true})
	if got := b.Usage("agy", f.now.Add(-Window))["agents_list"]; got != (Counts{Calls: 2, Errors: 1}) {
		t.Fatalf("agy agents_list = %+v", got)
	}
	if got := b.Usage("", f.now.Add(-Window))["agents_list"]; got.Calls != 3 {
		t.Fatalf("all runtimes agents_list = %+v", got)
	}
	f.advance(Window + 48*time.Hour)
	b.Record(Call{Tool: "todo_get", Runtime: "agy", OK: true})
	if got := b.Usage("agy", f.now.Add(-Window)); got["agents_list"].Calls != 0 || got["todo_get"].Calls != 1 {
		t.Fatalf("after the window: %+v", got)
	}
}

// A failure is friction unless it says nothing of how the tool is used (declined, a rate limit, away). A successful
// call of the same tool in the same conversation soon after, with other arguments, is the retry that fixed it; one
// with the same arguments, another conversation's, or one after the retry window is not.
func TestFrictionPairsTheRetryThatWorked(t *testing.T) {
	t.Parallel()
	f := newFakeHost()
	b := New(f.host())
	fail := Call{Tool: "computer_run", Runtime: "agy", Machine: "m", Session: "s", Skeleton: "{steps:[{launch_app:{name:str}}]}",
		Code: "invalid_batch"}
	b.Record(fail)
	b.Record(Call{Tool: "computer_run", Machine: "m", Session: "other", Skeleton: "{script:str}", OK: true})
	b.Record(Call{Tool: "computer_run", Machine: "m", Session: "s", Skeleton: "{script:str}", OK: true})
	b.Record(Call{Tool: "agent_send", Machine: "m", Session: "s", Code: "declined", Kind: "declined"})
	b.Record(Call{Tool: "agent_send", Machine: "m", Session: "s", Code: "x", Kind: "unavailable"})
	b.Record(fail) // fails again; retried with the same arguments, which tells nothing
	b.Record(Call{Tool: "computer_run", Machine: "m", Session: "s", Skeleton: fail.Skeleton, OK: true})
	b.Record(fail) // and once more, retried too late
	f.advance(RetryWindow + time.Second)
	b.Record(Call{Tool: "computer_run", Machine: "m", Session: "s", Skeleton: "{script:str}", OK: true})

	events := b.Friction(time.Time{})
	if len(events) != 3 {
		t.Fatalf("friction = %+v, want the three failures of computer_run", events)
	}
	if events[0].RetrySkeleton != "{script:str}" || events[0].Code != "invalid_batch" || events[0].Runtime != "agy" {
		t.Errorf("the retried failure = %+v", events[0])
	}
	if events[1].RetrySkeleton != "" || events[2].RetrySkeleton != "" {
		t.Errorf("a same-arguments or late retry was paired: %+v", events[1:])
	}
}

// Flush writes only when something changed, and a new book Loads what was written: the counts and the friction add
// to what it recorded before loading.
func TestFlushAndLoadKeepUsageAndFriction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeHost()
	b := New(f.host())
	b.Record(Call{Tool: "task_plan", Runtime: "agy", Code: "readonly_unsupported", Skeleton: "{tasks:[{readonly:bool}]}"})
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	written := f.vals[UsageKey]
	delete(f.vals, UsageKey) // a second Flush with nothing new must not write it again
	f.mu.Unlock()
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	_, rewritten := f.vals[UsageKey]
	f.vals[UsageKey] = written
	f.mu.Unlock()
	if rewritten {
		t.Fatal("Flush wrote again with nothing new")
	}

	next := New(f.host())
	next.Record(Call{Tool: "task_plan", Runtime: "agy", OK: true})
	next.Load(ctx)
	next.Load(ctx) // a second Load adds nothing
	if got := next.Usage("agy", f.now.Add(-time.Hour))["task_plan"]; got != (Counts{Calls: 2, Errors: 1}) {
		t.Fatalf("loaded usage = %+v", got)
	}
	if events := next.Friction(time.Time{}); len(events) != 1 || events[0].Code != "readonly_unsupported" {
		t.Fatalf("loaded friction = %+v", events)
	}
}

// The newest MaxEvents failures are kept.
func TestFrictionKeepsTheNewest(t *testing.T) {
	t.Parallel()
	f := newFakeHost()
	b := New(f.host())
	for i := range MaxEvents + 10 {
		b.Record(Call{Tool: "t", Code: "c", Skeleton: Skeleton(json.RawMessage(`{"i":` + strings.Repeat("1", i%3+1) + `}`))})
		f.advance(time.Second)
	}
	events := b.Friction(time.Time{})
	if len(events) != MaxEvents || events[len(events)-1].At != f.now.Add(-time.Second) {
		t.Fatalf("%d events kept, the last at %v", len(events), events[len(events)-1].At)
	}
}

// A draft is changed through UpdateDraft and stored; an unknown id or a failing change leaves the drafts as they were.
func TestDraftsUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeHost()
	b := New(f.host())
	if err := b.PutDrafts(ctx, []Draft{{ID: "fd_1", Kind: DraftNote, Tool: "computer_run", Status: DraftPending}}); err != nil {
		t.Fatal(err)
	}
	if err := b.UpdateDraft(ctx, "fd_1", func(d *Draft) error { d.Status, d.NoteID = DraftProposed, "tn_1"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := b.UpdateDraft(ctx, "fd_2", func(*Draft) error { return nil }); !errors.Is(err, ErrNoDraft) {
		t.Fatalf("unknown draft: %v", err)
	}
	boom := errors.New("no")
	if err := b.UpdateDraft(ctx, "fd_1", func(d *Draft) error { d.Status = DraftDismissed; return boom }); !errors.Is(err, boom) {
		t.Fatalf("failing change: %v", err)
	}
	drafts, err := b.Drafts(ctx)
	if err != nil || len(drafts) != 1 || drafts[0].Status != DraftProposed || drafts[0].NoteID != "tn_1" || drafts[0].UpdatedAt.IsZero() {
		t.Fatalf("drafts = %+v, %v", drafts, err)
	}
}

func TestNormalizeCode(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"invalid_batch": "invalid_batch", "Readonly Unsupported": "readonly_unsupported",
		"assistant-tasks.readonly_agy_unsupported": "assistant-tasks.readonly_agy_unsupported", "": "error", "!!": "error"} {
		if got := NormalizeCode(in); got != want {
			t.Errorf("NormalizeCode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := NormalizeCode(strings.Repeat("a", 200)); len(got) != 80 {
		t.Errorf("a long code is %d bytes", len(got))
	}
}
