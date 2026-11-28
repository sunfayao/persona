// Package lifecycle owns assistant-side capture, replay and retention of Agent lifecycle observations.
package lifecycle

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/proto"
)

const retention = 30 * 24 * time.Hour

type Store interface {
	LifecycleCursor(context.Context, string) (proto.LifecycleBatchRequest, error)
	CommitLifecycle(context.Context, string, proto.LifecycleBatchRequest, proto.LifecycleBatch, time.Time) error
	AppendLifecycle(context.Context, string, []proto.LifecycleObservation) error
	LifecyclePage(context.Context, string, string, string, int64, bool, int, time.Time) (proto.LifecyclePage, error)
	PruneLifecycle(context.Context, time.Time) error
}

type Dependencies struct {
	Store        func() Store
	Nodes        func() []string
	Call         func(context.Context, string, string, any, any) error
	Scope        func(context.Context, string, string) (spaceRef string, allowed bool, err error)
	Ready        func(string, string)
	AllowSession func(context.Context, string, string) bool
	AllowTodo    func(context.Context, string) bool
	UserOnly     func(http.ResponseWriter, *http.Request) bool
	Now          func() time.Time
	Ticker       func(time.Duration) (<-chan time.Time, func())
	Logf         func(string, ...any)
}

type Domain struct {
	deps   Dependencies
	legacy chan legacyBatch
}

type legacyBatch struct {
	machine string
	events  []proto.LifecycleObservation
}

func New(deps Dependencies) *Domain {
	return &Domain{deps: deps, legacy: make(chan legacyBatch, 128)}
}

func (d *Domain) Run(ctx context.Context) {
	tick, stop := d.deps.Ticker(3 * time.Second)
	defer stop()
	var prune time.Time
	var pending legacyBatch
	poll := func() {
		st := d.deps.Store()
		if st == nil {
			return
		}
		now := d.deps.Now()
		if !now.Before(prune) {
			if err := st.PruneLifecycle(ctx, now.Add(-retention)); err != nil {
				d.deps.Logf("lifecycle: prune: %v", err)
			} else {
				prune = now.Add(time.Hour)
			}
		}
		for _, machine := range d.deps.Nodes() {
			if err := d.pull(ctx, st, machine); err != nil && ctx.Err() == nil {
				d.deps.Logf("lifecycle: replay batch: %v", err)
			}
		}
	}
	poll()
	for {
		var legacy <-chan legacyBatch
		if pending.machine == "" {
			legacy = d.legacy
		}
		select {
		case <-ctx.Done():
			return
		case <-tick:
			poll()
		case pending = <-legacy:
		}
		if pending.machine != "" {
			if st := d.deps.Store(); st != nil {
				if err := st.AppendLifecycle(ctx, pending.machine, pending.events); err != nil {
					d.deps.Logf("lifecycle: legacy batch: %v", err)
				} else {
					pending = legacyBatch{}
				}
			}
		}
	}
}

func (d *Domain) pull(ctx context.Context, st Store, machine string) error {
	for range 4 {
		if err := ctx.Err(); err != nil {
			return err
		}
		cursor, err := st.LifecycleCursor(ctx, machine)
		if err != nil {
			return err
		}
		var batch proto.LifecycleBatch
		request, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = d.deps.Call(request, machine, nodeproto.LifecycleRead, cursor, &batch)
		cancel()
		if err != nil {
			return err
		}
		if !validBatch(batch) {
			return errors.New("invalid lifecycle batch")
		}
		if len(batch.Events) == 0 && cursor.Stream == batch.Stream && cursor.After+1 == batch.First {
			return nil
		}
		now := d.deps.Now()
		ready := make(map[string]bool)
		type sessionScope struct {
			space   string
			allowed bool
		}
		scopes := make(map[string]sessionScope)
		for i, e := range batch.Events {
			scope, known := scopes[e.Session]
			if !known {
				scope.space, scope.allowed, err = d.deps.Scope(ctx, machine, e.Session)
				if err != nil {
					return err
				}
				scopes[e.Session] = scope
			}
			if !proto.ValidLifecycleActivity(e.Activity) {
				// A newer node may know classes this manager does not: drop the class, keep the event.
				batch.Events[i].Activity = ""
			}
			if scope.allowed {
				// The sending node never decides the event's scope.
				batch.Events[i].SpaceRef = scope.space
				if (batch.Stream != cursor.Stream || batch.First+int64(i) > cursor.After) &&
					e.Kind == proto.LifecycleTurnFinished && e.Source != "inventory" &&
					e.At.After(now.Add(-15*time.Minute)) {
					ready[e.Session] = true
				}
			} else {
				// Keep the position, so suppressing a deleted session never creates a false gap.
				batch.Events[i] = proto.LifecycleObservation{}
			}
		}
		if err := st.CommitLifecycle(ctx, machine, cursor, batch, now); err != nil {
			return err
		}
		for session := range ready {
			d.deps.Ready(machine, session)
		}
		if len(batch.Events) < proto.LifecycleBatchSize {
			return nil
		}
	}
	return nil
}

func digest(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func validBatch(b proto.LifecycleBatch) bool {
	if !digest(b.Stream) || b.First < 1 || b.First > 1<<62 || len(b.Events) > proto.LifecycleBatchSize {
		return false
	}
	for _, e := range b.Events {
		if !proto.ValidLifecycleSession(e.Session) || e.At.IsZero() || !digest(e.Ref) || e.DurationMS < 0 ||
			(e.Incarnation != "" && !digest(e.Incarnation)) || (e.Turn != "" && !digest(e.Turn)) {
			return false
		}
		switch e.Source {
		case "hook", "inventory", "transcript", "coverage", "codex":
		default:
			return false
		}
		switch e.Kind {
		case proto.LifecycleSessionStarted, proto.LifecycleSessionExited, proto.LifecycleTurnStarted,
			proto.LifecycleTurnFinished, proto.LifecycleTurnFailed, proto.LifecycleChildLinked,
			proto.LifecycleBackgroundStarted, proto.LifecycleBackgroundEnded, proto.LifecycleGap:
		default:
			return false
		}
		switch e.Outcome {
		case "", "missing_start", "runtime_error", "history_changed":
		default:
			if !slices.Contains(proto.LifecycleBackgroundOutcomes, e.Outcome) {
				return false
			}
		}
	}
	return true
}

// ObserveLegacy records what inventory can prove and separately marks missing runtime coverage.
func (d *Domain) ObserveLegacy(machine string, connected time.Time, reconnected bool, list []proto.Session, known func(string) bool) {
	if connected.IsZero() || d.deps.Store() == nil {
		return
	}
	b := legacyBatch{machine: machine}
	if reconnected {
		b.events = append(b.events, proto.LifecycleObservation{Kind: proto.LifecycleGap, Source: "coverage", At: connected,
			Ref: proto.LifecycleRef(strconv.FormatInt(connected.UnixNano(), 10)), Outcome: "legacy_node"})
	}
	now := d.deps.Now()
	for _, s := range list {
		start := s.StartedAt()
		if known(s.SessionID) || !proto.ValidLifecycleSession(s.SessionID) || start.IsZero() || start.Before(connected) || start.Before(now.Add(-retention)) ||
			s.Starting || s.AwaitingFirstMessage || s.IsDeleted() {
			continue
		}
		ref := proto.LifecycleRef(start.UTC().Format(time.RFC3339Nano))
		b.events = append(b.events, proto.LifecycleObservation{Session: s.SessionID, Kind: proto.LifecycleSessionStarted,
			At: start, Source: "inventory", Ref: ref, Incarnation: ref})
	}
	if len(b.events) == 0 {
		return
	}
	select {
	case d.legacy <- b:
	default:
		d.deps.Logf("lifecycle: legacy inventory queue full")
	}
}

// MayUseClosingTranscript checks whether a later open turn would make a closing record stale.
// A gap or a runtime boundary alone says nothing about the to-do's outcome.
func (d *Domain) MayUseClosingTranscript(ctx context.Context, machine, session string, closed time.Time) bool {
	p, err := d.page(ctx, machine, session, "", 0, false, 64)
	if err != nil {
		return true // transcript recovery remains available when the independent event history is unavailable
	}
	resolved := make(map[string]bool)
	for _, e := range p.Events {
		if e.Session != session {
			continue
		}
		switch e.Kind {
		case proto.LifecycleTurnStarted:
			if e.Confidence == "confirmed" && e.At.After(closed) {
				return false
			}
		case proto.LifecycleWaitResolved:
			resolved[e.Ref] = true
		case proto.LifecycleWaitOpened:
			if !resolved[e.Ref] && !e.At.Before(closed) {
				return false
			}
		}
	}
	return true
}
