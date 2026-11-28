package db

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// ---- agent work states ----

// SessionWork is where one session stands in its work, about its latest turn (session_work).
type SessionWork struct {
	MachineID, SessionID string
	TurnAt               time.Time
	Work                 proto.WorkState
	WakeAt               *time.Time
	Retries, Wakes       int
	InteractionID        string
	UpdatedAt            time.Time
}

// PutSessionWork stores a session's work state, replacing what was there.
func (d *DB) PutSessionWork(ctx context.Context, w SessionWork) error {
	b, err := json.Marshal(w.Work)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, `INSERT INTO session_work (machine_id, session_id, turn_at, work, wake_at, retries, wakes,
			interaction_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (machine_id, session_id) DO UPDATE SET turn_at = $3, work = $4, wake_at = $5, retries = $6, wakes = $7,
			interaction_id = $8, updated_at = $9`,
		w.MachineID, w.SessionID, w.TurnAt, b, w.WakeAt, w.Retries, w.Wakes, w.InteractionID, w.UpdatedAt)
	return err
}

// SessionWorks are recent states, pending CI watches, and recently retired turns.
// A recent retirement of an old turn must survive the manager restarting.
func (d *DB) SessionWorks(ctx context.Context, since time.Time) ([]SessionWork, error) {
	rows, err := d.pool.Query(ctx, `SELECT machine_id, session_id, turn_at, work, wake_at, retries, wakes, interaction_id,
			updated_at FROM session_work WHERE turn_at > $1 OR (wake_at IS NOT NULL AND (work ? 'ci' OR work->>'on' = 'transient'))
			OR (updated_at > $1 AND work ? 'retired_at')`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionWork
	for rows.Next() {
		var w SessionWork
		var b []byte
		if err := rows.Scan(&w.MachineID, &w.SessionID, &w.TurnAt, &b, &w.WakeAt, &w.Retries, &w.Wakes, &w.InteractionID,
			&w.UpdatedAt); err != nil {
			return nil, err
		}
		if json.Unmarshal(b, &w.Work) != nil {
			continue
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// PruneSessionWork keeps active CI watches and recent retirement markers.
func (d *DB) PruneSessionWork(ctx context.Context, before time.Time) error {
	if _, err := d.pool.Exec(ctx, `DELETE FROM session_work WHERE turn_at < $1 AND NOT (wake_at IS NOT NULL AND (work ? 'ci' OR work->>'on' = 'transient'))
		AND NOT (updated_at >= $1 AND work ? 'retired_at')`, before); err != nil {
		return err
	}
	_, err := d.pool.Exec(ctx, `DELETE FROM session_work_events WHERE at < $1`, before)
	return err
}

// WorkEvent is one thing that happened about a session's work (session_work_events).
type WorkEvent struct {
	At                   time.Time
	MachineID, SessionID string
	Kind                 string
	Detail               map[string]any
}

// AddWorkEvent records an event.
func (d *DB) AddWorkEvent(ctx context.Context, e WorkEvent) error {
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	b, err := json.Marshal(e.Detail)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, `INSERT INTO session_work_events (at, machine_id, session_id, kind, detail)
		VALUES ($1, $2, $3, $4, $5)`, e.At, e.MachineID, e.SessionID, e.Kind, b)
	return err
}

// WorkEvents are the events since since, oldest first.
func (d *DB) WorkEvents(ctx context.Context, since time.Time) ([]WorkEvent, error) {
	rows, err := d.pool.Query(ctx, `SELECT at, machine_id, session_id, kind, detail FROM session_work_events
		WHERE at > $1 ORDER BY at, id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkEvent
	for rows.Next() {
		var e WorkEvent
		var b []byte
		if err := rows.Scan(&e.At, &e.MachineID, &e.SessionID, &e.Kind, &b); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(b, &e.Detail)
		out = append(out, e)
	}
	return out, rows.Err()
}

// LifecycleCursor is the node's last committed sequence. The node can release entries only after it
// sees this cursor in its next request.
func (d *DB) LifecycleCursor(ctx context.Context, machine string) (proto.LifecycleBatchRequest, error) {
	return lifecycleCursor(ctx, d.pool, machine)
}

func lifecycleCursor(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, machine string) (proto.LifecycleBatchRequest, error) {
	var p proto.LifecycleBatchRequest
	err := q.QueryRow(ctx, "SELECT stream, ack FROM agent_lifecycle_sources WHERE machine_id = $1", machine).Scan(&p.Stream, &p.After)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return p, err
}

const lifecycleInsert = `INSERT INTO agent_lifecycle_events
 (machine_id, session_id, incarnation, kind, occurred_at, source, source_ref, turn_id, outcome, duration_ms, confidence, space_ref, activity, todo_id)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,nullif($13,''),
   coalesce((SELECT todo_id FROM todo_agents WHERE machine_id = $1 AND session_id = $2
     AND linked_at <= $5 AND (released_at IS NULL OR released_at > $5)
     AND (dismissed_at IS NULL OR dismissed_at > $5)
     ORDER BY linked_at DESC LIMIT 1), ''))
 ON CONFLICT (machine_id, session_id, incarnation, kind, source, source_ref) DO UPDATE
   SET duration_ms = excluded.duration_ms, revision = excluded.revision
   WHERE agent_lifecycle_events.duration_ms = 0 AND excluded.duration_ms > 0`

func insertLifecycle(ctx context.Context, tx pgx.Tx, machine string, events []proto.LifecycleObservation) error {
	if len(events) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, e := range events {
		confidence := "confirmed"
		if e.Source == "inventory" || e.Source == "coverage" {
			confidence = "observed"
		}
		b.Queue(lifecycleInsert, machine, e.Session, e.Incarnation, e.Kind, e.At, e.Source, e.Ref, e.Turn, e.Outcome, e.DurationMS, confidence, e.SpaceRef, e.Activity)
	}
	return tx.SendBatch(ctx, b).Close()
}

// CommitLifecycle commits a contiguous replay and its acknowledged offset in the same transaction.
func (d *DB) CommitLifecycle(ctx context.Context, machine string, expected proto.LifecycleBatchRequest, batch proto.LifecycleBatch, at time.Time) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT agent_lifecycle_lock()"); err != nil {
			return err
		}
		current, err := lifecycleCursor(ctx, tx, machine)
		if err != nil {
			return err
		}
		if current != expected {
			return errors.New("lifecycle cursor changed during replay")
		}
		reset := current.Stream != batch.Stream
		if reset {
			current.After = 0
		}
		first := max(batch.First, current.After+1)
		last := batch.First + int64(len(batch.Events)) - 1
		lost := reset && expected.Stream != "" || batch.First > current.After+1
		events := make([]proto.LifecycleObservation, 0, len(batch.Events)+1)
		if lost {
			reason := "queue_overflow"
			if reset && expected.Stream != "" {
				reason = "journal_reset"
			}
			events = append(events, proto.LifecycleObservation{Kind: proto.LifecycleGap, Source: "coverage", At: at,
				Ref: batch.Stream + ":" + strconv.FormatInt(batch.First, 10), Outcome: reason})
		}
		if last >= first {
			for _, event := range batch.Events[first-batch.First:] {
				if event.Session != "" && !event.At.Before(at.Add(-30*24*time.Hour)) {
					events = append(events, event)
				}
			}
		}
		if err := insertLifecycle(ctx, tx, machine, events); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_lifecycle_sources (machine_id, stream, ack) VALUES ($1,$2,$3)
		  ON CONFLICT (machine_id) DO UPDATE SET stream = excluded.stream, ack = excluded.ack
		  WHERE (agent_lifecycle_sources.stream, agent_lifecycle_sources.ack) IS DISTINCT FROM (excluded.stream, excluded.ack)`,
			machine, batch.Stream, max(current.After, last))
		return err
	})
}

// AppendLifecycle inserts Manager observations that have no node cursor, including legacy coverage.
func (d *DB) AppendLifecycle(ctx context.Context, machine string, events []proto.LifecycleObservation) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT agent_lifecycle_lock()"); err != nil {
			return err
		}
		return insertLifecycle(ctx, tx, machine, events)
	})
}

// LifecyclePage provides both a chronological resume cursor and backwards history pagination.
func (d *DB) LifecyclePage(ctx context.Context, machine, session, todo string, cursor int64, forward bool, limit int, since time.Time) (proto.LifecyclePage, error) {
	where := `(e.machine_id = $1 AND (e.session_id = $5 OR (e.session_id = '' AND e.kind = 'coverage.gap')))`
	if todo != "" {
		where = `((e.todo_id <> '' AND e.todo_id = $1) OR (e.kind = 'coverage.gap' AND e.session_id = '' AND
		  EXISTS (SELECT 1 FROM todo_agents a WHERE a.todo_id = $1 AND a.machine_id = e.machine_id
		    AND a.linked_at <= e.occurred_at AND (a.released_at IS NULL OR a.released_at > e.occurred_at)
		    AND (a.dismissed_at IS NULL OR a.dismissed_at > e.occurred_at)))
		  OR (e.todo_id = '' AND e.session_id <> '' AND EXISTS (SELECT 1 FROM todo_agents a
		    WHERE a.todo_id = $1 AND a.machine_id = e.machine_id AND a.session_id = e.session_id
		    AND a.linked_at <= e.occurred_at AND (a.released_at IS NULL OR a.released_at > e.occurred_at)
		    AND (a.dismissed_at IS NULL OR a.dismissed_at > e.occurred_at))))`
	}
	order := " AND e.revision < $2 ORDER BY e.revision DESC"
	if forward {
		order = " AND e.revision > $2 ORDER BY e.revision ASC"
	} else if cursor == 0 {
		cursor = math.MaxInt64
	}
	args := []any{todo, cursor, since, limit + 1}
	if todo == "" {
		args = append([]any{machine, cursor, since, limit + 1}, session)
	}
	rows, err := d.pool.Query(ctx, `SELECT e.revision, e.machine_id, e.session_id, e.incarnation, e.kind,
	    e.occurred_at, e.received_at, e.source, e.source_ref, e.turn_id, e.outcome, e.duration_ms, e.confidence, e.space_ref, coalesce(e.activity, ''), e.todo_id
	  FROM agent_lifecycle_events e WHERE e.occurred_at >= $3 AND `+where+order+" LIMIT $4", args...)
	if err != nil {
		return proto.LifecyclePage{}, err
	}
	defer rows.Close()
	p := proto.LifecyclePage{Events: make([]proto.LifecycleEvent, 0, limit)}
	for rows.Next() {
		var e proto.LifecycleEvent
		if err := rows.Scan(&e.Revision, &e.Machine, &e.Session, &e.Incarnation, &e.Kind, &e.At, &e.Received,
			&e.Source, &e.Ref, &e.Turn, &e.Outcome, &e.DurationMS, &e.Confidence, &e.SpaceRef, &e.Activity, &e.Todo); err != nil {
			return proto.LifecyclePage{}, err
		}
		if len(p.Events) == limit {
			p.Next = p.Events[limit-1].Revision
			break
		}
		p.Events = append(p.Events, e)
	}
	if forward && len(p.Events) != 0 {
		p.Next = p.Events[len(p.Events)-1].Revision
	}
	return p, rows.Err()
}

func (d *DB) PruneLifecycle(ctx context.Context, before time.Time) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM agent_lifecycle_events WHERE revision IN
		(SELECT revision FROM agent_lifecycle_events WHERE occurred_at < $1 ORDER BY occurred_at LIMIT 10000)`, before)
	return err
}
