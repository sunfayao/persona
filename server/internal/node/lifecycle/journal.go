// Package lifecycle journals compact runtime observations until the manager commits them.
package lifecycle

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

const capacity = 2048

type activeTurn struct {
	ID          string    `json:"id"`
	Incarnation string    `json:"incarnation"`
	At          time.Time `json:"at"`
}

type journal struct {
	Stream string                       `json:"stream"`
	First  int64                        `json:"first"`
	Events []proto.LifecycleObservation `json:"events"`
	Open   map[string]activeTurn        `json:"open,omitempty"`
}

type Queue struct {
	mu        sync.Mutex
	path      string
	now       func() time.Time
	state     journal
	dirty     bool
	seen      map[string]bool
	keys      []string
	nextKey   int
	inventory map[string]inventory
	history   map[string]string
}

func New(path string, now func() time.Time) (*Queue, error) {
	q := &Queue{path: path, now: now, seen: make(map[string]bool, capacity*2)}
	if err := q.load(); err != nil {
		return nil, err
	}
	for _, e := range q.state.Events {
		q.remember(key(e))
	}
	return q, nil
}

func (q *Queue) load() error {
	f, err := os.Open(q.path)
	if err == nil {
		defer f.Close()
		err = json.NewDecoder(io.LimitReader(f, 3<<20)).Decode(&q.state)
		if err == nil && len(q.state.Stream) == 32 && q.state.First > 0 && len(q.state.Events) <= capacity {
			if q.state.Open == nil {
				q.state.Open = make(map[string]activeTurn)
			}
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A different stream lets the Manager mark any loss even if this file could not be recovered.
	var id [16]byte
	rand.Read(id[:])
	q.state = journal{Stream: hex.EncodeToString(id[:]), First: 1, Open: make(map[string]activeTurn)}
	q.dirty = true
	return q.save()
}

func (q *Queue) save() error {
	if !q.dirty {
		return nil
	}
	data, err := json.Marshal(q.state)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(q.path+".next", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err == nil {
		err = os.Rename(q.path+".next", q.path)
	}
	if err == nil {
		q.dirty = false
	}
	return err
}

func key(e proto.LifecycleObservation) string {
	return e.Session + "\x00" + e.Incarnation + "\x00" + e.Kind + "\x00" + e.Source + "\x00" + e.Ref + strconv.FormatBool(e.Source == "codex" && e.DurationMS > 0)
}

func (q *Queue) remember(k string) {
	if len(q.keys) < capacity*2 {
		q.keys = append(q.keys, k)
	} else {
		delete(q.seen, q.keys[q.nextKey])
		q.keys[q.nextKey] = k
		q.nextKey = (q.nextKey + 1) % len(q.keys)
	}
	q.seen[k] = true
}

func (q *Queue) add(e proto.LifecycleObservation) {
	k := key(e)
	if q.seen[k] {
		return
	}
	q.remember(k)
	q.state.Events = append(q.state.Events, e)
	q.dirty = true
	if excess := len(q.state.Events) - capacity; excess > 0 {
		q.state.Events = slices.Delete(q.state.Events, 0, excess)
		q.state.First += int64(excess)
	}
}

func (q *Queue) Append(events ...proto.LifecycleObservation) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range events {
		q.add(e)
	}
	return q.save()
}

// Hook records a Claude process boundary using the hook attempt and the runtime prompt identifier.
func (q *Queue) Hook(session, event, attempt, prompt string, pid int, processStart string) error {
	if !proto.ValidLifecycleSession(session) || attempt == "" {
		return nil
	}
	e := proto.LifecycleObservation{Session: session, At: q.now(), Source: "hook", Ref: proto.LifecycleRef(attempt)}
	if processStart != "" {
		e.Incarnation = proto.LifecycleRef(strconv.Itoa(pid), processStart)
	}
	switch event {
	case "SessionStart":
		e.Kind = proto.LifecycleSessionStarted
		if e.Incarnation != "" {
			e.Ref = e.Incarnation
		}
	case "SessionEnd":
		e.Kind = proto.LifecycleSessionExited
		if e.Incarnation != "" {
			e.Ref = e.Incarnation
		}
	case "UserPromptSubmit":
		e.Kind, e.Turn = proto.LifecycleTurnStarted, e.Ref
		if prompt != "" {
			e.Turn = proto.LifecycleRef(prompt)
		}
		e.Ref = e.Turn
	case "Stop":
		e.Kind = proto.LifecycleTurnFinished
	default:
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.seen[key(e)] {
		return q.save()
	}
	turn := q.state.Open[session]
	switch e.Kind {
	case proto.LifecycleTurnStarted:
		if len(q.state.Open) < capacity || turn.ID != "" {
			q.state.Open[session] = activeTurn{ID: e.Turn, Incarnation: e.Incarnation, At: e.At}
		}
	case proto.LifecycleTurnFinished:
		if turn.ID != "" && turn.Incarnation == e.Incarnation {
			e.Turn = turn.ID
			e.DurationMS = max(0, e.At.Sub(turn.At).Milliseconds())
			delete(q.state.Open, session)
		} else {
			// The Stop is evidence of an end even when its opening hook was missed.
			e.Outcome = "missing_start"
		}
	case proto.LifecycleSessionExited:
		delete(q.state.Open, session)
	}
	q.add(e)
	return q.save()
}

func (q *Queue) Read(req proto.LifecycleBatchRequest) (proto.LifecycleBatch, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	last := q.state.First + int64(len(q.state.Events)) - 1
	if req.After < 0 || (req.Stream == q.state.Stream && req.After > last) {
		return proto.LifecycleBatch{}, fmt.Errorf("invalid lifecycle acknowledgement")
	}
	if req.Stream == q.state.Stream && req.After >= q.state.First {
		n := int(req.After - q.state.First + 1)
		q.state.Events = slices.Delete(q.state.Events, 0, n)
		q.state.First += int64(n)
		q.dirty = true
	}
	if err := q.save(); err != nil {
		return proto.LifecycleBatch{}, err
	}
	return proto.LifecycleBatch{Stream: q.state.Stream, First: q.state.First,
		Events: slices.Clone(q.state.Events[:min(proto.LifecycleBatchSize, len(q.state.Events))])}, nil
}
