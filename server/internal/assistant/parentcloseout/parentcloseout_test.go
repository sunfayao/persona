package parentcloseout

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
)

var epoch = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

type memStore map[string]Marker

func (m memStore) Setting(_ context.Context, _ string, v any) (bool, error) {
	*v.(*map[string]Marker) = map[string]Marker(m)
	return len(m) > 0, nil
}

func (m memStore) PutSetting(_ context.Context, _ string, v any) error {
	clear(m)
	for k, x := range v.(map[string]Marker) {
		m[k] = x
	}
	return nil
}

func domainAt(now *time.Time, st Store) *Domain {
	return New(Dependencies{Clock: ClockFunc(func() time.Time { return *now }), Store: func() Store { return st }})
}

func piece(id string, mod func(*Piece)) Piece {
	p := Piece{ID: id, Title: "Piece " + id, Done: true, Dispatched: true, Changed: epoch}
	if mod != nil {
		mod(&p)
	}
	return p
}

var openParent = Parent{Open: true, Updated: epoch.Add(-time.Hour)}

func TestContainerIdentity(t *testing.T) {
	t.Parallel()
	pieces := []Piece{piece("a", nil), piece("b", nil)}
	for name, tc := range map[string]struct {
		parent Parent
		pieces []Piece
		want   bool
	}{
		"plain request":      {openParent, pieces, true},
		"own criteria":       {Parent{Open: true, HasLedger: true}, pieces, false},
		"own agent":          {Parent{Open: true, OwnAgent: true}, pieces, false},
		"concern or ask":     {Parent{Open: true, Excluded: true}, pieces, false},
		"undispatched piece": {openParent, []Piece{piece("a", nil), piece("b", func(p *Piece) { p.Dispatched = false })}, false},
		"no pieces":          {openParent, nil, false},
	} {
		if got := IsContainer(tc.parent, tc.pieces); got != tc.want {
			t.Errorf("%s: IsContainer = %v, want %v", name, got, tc.want)
		}
	}
}

func TestReady(t *testing.T) {
	t.Parallel()
	cancelled := func(p *Piece) { p.Cancelled = true }
	failed := func(p *Piece) { p.StartFailed = true }
	open := func(p *Piece) { p.Done = false }
	for name, tc := range map[string]struct {
		pieces []Piece
		want   bool
	}{
		"all accepted":              {[]Piece{piece("a", nil), piece("b", nil)}, true},
		"one cancelled":             {[]Piece{piece("a", nil), piece("b", cancelled), piece("c", nil)}, true},
		"only cancelled":            {[]Piece{piece("a", cancelled)}, false},
		"one open":                  {[]Piece{piece("a", nil), piece("b", open)}, false},
		"failed start blocks":       {[]Piece{piece("a", nil), piece("b", failed)}, false},
		"failed start replaced":     {[]Piece{piece("a", nil), piece("b", func(p *Piece) { failed(p); p.Title = "Piece a" })}, true},
		"no pieces":                 {nil, false},
		"cancelled failed is ended": {[]Piece{piece("a", nil), piece("b", func(p *Piece) { failed(p); cancelled(p) })}, true},
	} {
		if got := Ready(tc.pieces); got != tc.want {
			t.Errorf("%s: Ready = %v, want %v", name, got, tc.want)
		}
	}
}

func TestEvaluateSettleWindowAndContainer(t *testing.T) {
	t.Parallel()
	now := epoch.Add(SettleWindow - time.Second)
	d := domainAt(&now, nil)
	pieces := []Piece{piece("a", nil), piece("b", nil)}

	if v := d.Evaluate(openParent, pieces, nil); v.Action != ActionWait || v.State != StateClosing || !v.Container {
		t.Fatalf("inside the window: %+v", v)
	}
	now = epoch.Add(SettleWindow)
	if v := d.Evaluate(openParent, pieces, nil); v.Action != ActionClose {
		t.Fatalf("after the window: %+v", v)
	}
	// a piece attached or changed restarts the window
	pieces = append(pieces, piece("c", func(p *Piece) { p.Changed = now.Add(-time.Second) }))
	if v := d.Evaluate(openParent, pieces, nil); v.Action != ActionWait {
		t.Fatalf("a piece changed a second ago: %+v", v)
	}
	// still open piece: nothing at all
	pieces[2].Done = false
	if v := d.Evaluate(openParent, pieces, nil); v.Action != ActionNone || v.State != "" {
		t.Fatalf("open piece: %+v", v)
	}
	pieces[2].Done = true
	now = now.Add(time.Hour)
	// held by a future reminder or due date; reopened by the owner after the pieces ended
	held := openParent
	held.HoldUntil = now.Add(time.Minute)
	if v := d.Evaluate(held, pieces, nil); v.Action != ActionNone || v.State != "" {
		t.Fatalf("future reminder: %+v", v)
	}
	reopened := openParent
	reopened.Reopened = now.Add(-time.Minute)
	if v := d.Evaluate(reopened, pieces, nil); v.Action != ActionNone {
		t.Fatalf("reopened by the owner: %+v", v)
	}
	// not open, active Agent, concern
	for _, p := range []Parent{{}, {Open: true, HasAgent: true}, {Open: true, Excluded: true}} {
		if v := d.Evaluate(p, pieces, nil); v.Action != ActionNone {
			t.Fatalf("%+v: %+v", p, v)
		}
	}
}

func TestEvaluateWakeAndReraise(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	d := domainAt(&now, nil)
	pieces := []Piece{piece("a", nil)}
	own := Parent{Open: true, HasLedger: true, Updated: epoch}
	first := d.Evaluate(own, pieces, nil)
	if first.Action != ActionWake || first.State != StateAwaiting || first.Container || first.Key != "children:a" {
		t.Fatalf("first: %+v", first)
	}
	m := &Marker{Key: first.Key, At: now, Count: 1}
	at := func(after time.Duration) Verdict {
		now = m.At.Add(after)
		return d.Evaluate(own, pieces, m)
	}
	if v := at(ReraiseAfter - time.Second); v.Action != ActionNone || v.State != StateAwaiting {
		t.Fatalf("inside the back-off: %+v", v)
	}
	if v := at(ReraiseAfter); v.Action != ActionWake || v.Key != "children:a#2" {
		t.Fatalf("after the back-off: %+v", v)
	}
	// a note or a park recorded since the wake, or a new piece, ends it
	noted := own
	noted.Updated = m.At.Add(time.Minute)
	now = m.At.Add(time.Hour)
	if v := d.Evaluate(noted, pieces, m); v.Action != ActionNone {
		t.Fatalf("noted since the wake: %+v", v)
	}
	parked := own
	parked.Parked = true
	if v := d.Evaluate(parked, pieces, m); v.Action != ActionNone {
		t.Fatalf("parked: %+v", v)
	}
	if v := d.Evaluate(own, append(pieces, piece("b", nil)), m); v.Action != ActionWake || v.Key != "children:a,b" {
		t.Fatalf("a new finished set wakes afresh: %+v", v)
	}
	// at most three wakes; a marker from before re-raising (no count) counts as used up
	m.Count = MaxWakes
	if v := d.Evaluate(own, pieces, m); v.Action != ActionNone {
		t.Fatalf("fourth wake: %+v", v)
	}
	if v := d.Evaluate(own, pieces, m); v.State != "" {
		t.Fatalf("wakes used up: the parent is unattended again, not awaiting: %+v", v)
	}
	m.Count = 0
	if v := d.Evaluate(own, pieces, m); v.Action != ActionNone || v.State != "" {
		t.Fatalf("a marker written before the count is used up: %+v", v)
	}
	noted.Updated = m.At.Add(time.Minute)
	m.Count = 1
	if v := d.Evaluate(noted, pieces, m); v.State != "" {
		t.Fatalf("no wake can be raised any more after a note: %+v", v)
	}
}

func TestMarkersPersistAndCount(t *testing.T) {
	t.Parallel()
	now := epoch
	st := memStore{}
	d := domainAt(&now, st)
	ctx := context.Background()
	d.Told(ctx, "p1", "children:a", "conv")
	now = now.Add(time.Hour)
	d.Told(ctx, "p1", "children:a", "conv") // the same wake again: nothing changes
	d.Told(ctx, "p1", "children:a#2", "conv")
	d.Told(ctx, "p2", "children:x", "")
	if m, ok, _ := d.Marker(ctx, "p1"); !ok || m.Count != 2 || m.Key != "children:a#2" {
		t.Fatalf("p1 marker %+v", m)
	}
	if st["p1"].Count != 2 || len(st) != 2 {
		t.Fatalf("store %+v", st)
	}
	// another Domain on the same store reads what was kept
	if m, ok, _ := domainAt(&now, st).Marker(ctx, "p2"); !ok || m.Key != "children:x" {
		t.Fatalf("reloaded %+v", m)
	}
	d.Forget(ctx, "p1", nil)
	d.Forget(ctx, "", func(id string) bool { return id == "p2" })
	if len(st) != 0 {
		t.Fatalf("not forgotten: %+v", st)
	}
}

func TestRollUp(t *testing.T) {
	t.Parallel()
	text := RollUp(i18n.En, []Piece{
		piece("a", func(p *Piece) { p.Title, p.Criteria, p.Met, p.PRs = "API", 3, 3, []string{"https://x/pull/1"} }),
		piece("b", func(p *Piece) { p.Title = "UI" }),
		piece("c", func(p *Piece) { p.Title, p.Cancelled, p.CancelReason = "Docs", true, "no longer\nneeded" }),
	})
	for _, want := range []string{"2 accepted, 1 cancelled", "- API: accepted, 3/3 criteria met (delivery: https://x/pull/1)", "- UI: accepted\n", "- Docs: cancelled (no longer needed)"} {
		if !strings.Contains(text+"\n", want) {
			t.Errorf("roll-up lacks %q:\n%s", want, text)
		}
	}
}
