package lifecycle

import (
	"strconv"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

type inventory struct {
	at     time.Time
	status string
	turn   string
}

func (q *Queue) Inventory(list []proto.Session) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	next := make(map[string]inventory, len(list))
	nextBG := make(map[string]backgroundInfo)
	now := q.now()
	for _, s := range list {
		if !proto.ValidLifecycleSession(s.SessionID) || s.IsDeleted() || s.Starting || s.AwaitingFirstMessage {
			continue
		}
		start, before := s.StartedAt(), q.inventory[s.SessionID]
		entry := inventory{at: start, status: s.Status, turn: before.turn}
		if before.at != start {
			before = inventory{}
		}
		generation := ""
		if !start.IsZero() && start.After(now.Add(-30*24*time.Hour)) {
			generation = proto.LifecycleRef(start.UTC().Format(time.RFC3339Nano))
			if s.Launcher != "claude" {
				q.add(proto.LifecycleObservation{Session: s.SessionID, Kind: proto.LifecycleSessionStarted, At: start,
					Source: "inventory", Ref: generation, Incarnation: generation})
			}
		}
		if s.ParentSessionID != "" {
			at := start
			if at.IsZero() {
				at = s.UpdatedAt
			}
			if !at.IsZero() {
				q.add(proto.LifecycleObservation{Session: s.SessionID, Kind: proto.LifecycleChildLinked, At: at,
					Source: "inventory", Ref: proto.LifecycleRef(s.ParentSessionID)})
			}
		}
		if s.Status == proto.StatusWorking && before.status != "" && before.status != proto.StatusWorking && s.Launcher != "claude" && s.Launcher != "codex" {
			entry.turn = eventTurn(s, generation)
			q.add(proto.LifecycleObservation{Session: s.SessionID, Kind: proto.LifecycleTurnStarted, At: now,
				Source: "inventory", Ref: entry.turn, Turn: entry.turn, Incarnation: generation})
		}
		turn := q.state.Open[s.SessionID].ID
		if turn == "" {
			turn = entry.turn
		}
		for _, bg := range s.Background {
			q.background(s.SessionID, bg, turn, nextBG)
		}
		for _, bg := range s.BackgroundEnded() {
			q.background(s.SessionID, bg, turn, nextBG)
		}
		if s.Launcher != "codex" && s.Status != proto.StatusWorking && s.TurnErrorKind != "" && !s.UpdatedAt.IsZero() && s.UpdatedAt.After(now.Add(-30*24*time.Hour)) {
			outcome := "runtime_error"
			if s.TurnErrorKind == proto.TurnErrorStopped {
				outcome = "interrupted"
			}
			q.add(proto.LifecycleObservation{Session: s.SessionID, Kind: proto.LifecycleTurnFailed, At: s.UpdatedAt,
				Source: "inventory", Ref: eventTurn(s, generation), Turn: before.turn, Incarnation: generation, Outcome: outcome})
		}
		if before.status == proto.StatusWorking && before.turn != "" && s.TurnErrorKind == "" &&
			(s.Status == proto.StatusDone || s.Status == proto.StatusIdle) {
			q.add(proto.LifecycleObservation{Session: s.SessionID, Kind: proto.LifecycleTurnFinished, At: now,
				Source: "inventory", Ref: before.turn, Turn: before.turn, Incarnation: generation})
			entry.turn = ""
		}
		next[s.SessionID] = entry
	}
	q.inventory, q.backgrounds = next, nextBG
	return q.save()
}

func eventTurn(s proto.Session, generation string) string {
	return proto.LifecycleRef(generation, s.UpdatedAt.UTC().Format(time.RFC3339Nano), strconv.FormatInt(s.Rev, 10))
}

// backgroundInfo is what the first sighting of a background task settled: the turn that started it and its activity
// class, kept while the inventory still lists the task so its end carries the same values.
type backgroundInfo struct{ turn, activity string }

// background records a task's start and end. turn is the turn open now; it is attributed to the task only when the task
// is still running at its first sighting, because an ended task may belong to an earlier turn. The task's command and
// description are read for their class and never stored.
func (q *Queue) background(session string, bg proto.BackgroundTask, turn string, next map[string]backgroundInfo) {
	if bg.ID == "" {
		return
	}
	ref := proto.LifecycleRef(bg.ID, bg.StartedAt.UTC().Format(time.RFC3339Nano))
	k := session + "\x00" + ref
	info, known := q.backgrounds[k]
	if !known {
		info.activity = transcript.CommandClass(bg.Command, bg.Description)
		if bg.EndedAt == nil || bg.EndedAt.IsZero() {
			info.turn = turn
		}
	}
	next[k] = info
	base := proto.LifecycleObservation{Session: session, Source: "inventory", Ref: ref, Turn: info.turn, Activity: info.activity}
	if !bg.StartedAt.IsZero() {
		base.Kind, base.At = proto.LifecycleBackgroundStarted, bg.StartedAt
		q.add(base)
	}
	if bg.EndedAt != nil && !bg.EndedAt.IsZero() {
		base.Kind, base.At = proto.LifecycleBackgroundEnded, *bg.EndedAt
		base.Outcome = proto.LifecycleBackgroundOutcome(bg.Status)
		q.add(base)
	}
}

// Transcript keeps native boundaries, including a whole turn between two inventory updates.
func (q *Queue) Transcript(session string, items []proto.Item, span transcript.Span, hookCovered, codex bool) error {
	if !proto.ValidLifecycleSession(session) {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.history == nil {
		q.history = make(map[string]string)
	}
	if before := q.history[session]; before != "" && before != span.History {
		q.add(proto.LifecycleObservation{Session: session, Kind: proto.LifecycleGap, At: q.now(), Source: "coverage",
			Ref: proto.LifecycleRef(before, span.History), Outcome: "history_changed"})
	}
	if len(q.history) < capacity || q.history[session] != "" {
		q.history[session] = span.History
	}
	now := q.now()
	for _, it := range items {
		if it.TS.IsZero() || it.TS.Before(now.Add(-30*24*time.Hour)) {
			continue
		}
		ref := proto.LifecycleRef(span.History, strconv.Itoa(it.Seq), it.ID)
		e := proto.LifecycleObservation{Session: session, At: it.TS, Source: "transcript", Ref: ref,
			Incarnation: proto.LifecycleRef(span.History)}
		switch {
		case it.TurnDuration != nil && !hookCovered:
			d := it.TurnDuration
			if d.TurnID != "" {
				e.Turn, e.Ref = proto.LifecycleRef(d.TurnID), proto.LifecycleRef(d.TurnID)
				if d.Source == "codex_task_complete" {
					e.Source, e.Incarnation, e.Outcome = "codex", "", "completed"
				}
			}
			e.Kind, e.DurationMS = proto.LifecycleTurnFinished, max(0, d.DurationMS)
		case it.Kind == proto.KindNotice && it.Text == transcript.InterruptedNotice:
			e.Kind, e.Outcome = proto.LifecycleTurnFailed, "interrupted"
		case it.Role == "user" && it.Kind == proto.KindText && !it.Queued && !hookCovered && !codex:
			e.Kind, e.Turn = proto.LifecycleTurnStarted, proto.LifecycleRef(it.ID)
		default:
			continue
		}
		q.add(e)
	}
	return q.save()
}

// CodexTurn records app-server notifications before a short turn can disappear from the inventory.
func (q *Queue) CodexTurn(session, turn, method, status string) error {
	if !proto.ValidLifecycleSession(session) || turn == "" {
		return nil
	}
	ref := proto.LifecycleRef(turn)
	e := proto.LifecycleObservation{Session: session, Source: "codex", Ref: ref, Turn: ref, At: q.now()}
	switch method {
	case "turn/started":
		e.Kind = proto.LifecycleTurnStarted
	case "turn/completed":
		switch status {
		case "failed":
			e.Kind, e.Outcome = proto.LifecycleTurnFailed, "runtime_error"
		case "interrupted":
			e.Kind, e.Outcome = proto.LifecycleTurnFailed, "interrupted"
		case "completed", "":
			e.Kind, e.Outcome = proto.LifecycleTurnFinished, "completed"
		default:
			return nil
		}
	default:
		return nil
	}
	return q.Append(e)
}
