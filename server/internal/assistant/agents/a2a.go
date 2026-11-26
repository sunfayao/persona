package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/textfmt"
)

const (
	a2aSubmitted = "submitted"
	a2aWorking   = "working"
	a2aInput     = "input_required"
	a2aAuth      = "auth_required"
	a2aComplete  = "completed"
	a2aFailed    = "failed"
	a2aCanceled  = "canceled"
	a2aQueued    = "queued"
	a2aSending   = "sending"
	a2aDelivered = "delivered"
	a2aUncertain = "uncertain"
	a2aExpired   = "unconfirmed"
	a2aDiscarded = "discarded"
	a2aMaxText   = 12000
	a2aMaxEvents = 96
)

const (
	errA2AUnavailable a2aError = "A2A is unavailable for this session"
	errA2ANotFound    a2aError = "A2A task not found"
	errA2AConflict    a2aError = "A2A task changed, retry with the same message_id"
	errA2ACancel      a2aError = "the A2A task cannot be canceled while its turn may be running"
	errA2AInvalid     a2aError = "invalid A2A request"
	errA2ADuplicate   a2aError = "message_id already identifies a different A2A message"
)

type Store interface {
	A2ACreate(context.Context, db.A2ARow) (*db.A2ARow, error)
	A2ARequest(context.Context, string, string) (*db.A2ARow, error)
	A2AGet(context.Context, string) (*db.A2ARow, error)
	A2AList(context.Context, string, string, string, string, string, string, bool, *time.Time, int) ([]db.A2ARow, error)
	A2ACount(context.Context, string, string, string, string, string, *time.Time) (int, error)
	A2AUpdate(context.Context, db.A2ARow, time.Time, *time.Time) (bool, error)
	A2AClaim(context.Context, int) ([]db.A2ARow, error)
	A2ADispatches(context.Context, string, string, string, time.Time) ([]db.A2ARow, error)
}

type Ref struct {
	Machine string `json:"machine_id"`
	Session string `json:"session_id"`
}

type Caller struct {
	Ref   Ref
	Owner string
}

type Event struct {
	// Seq is the position in the task's append-only conversation.
	Seq       int       `json:"seq"`
	ID        string    `json:"message_id"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text,omitempty"`
	State     string    `json:"state,omitempty"`
	At        time.Time `json:"at"`
	Delivery  string    `json:"delivery,omitempty"`
	Attempted time.Time `json:"attempted,omitzero"`
}

type taskBody struct {
	Wake         bool    `json:"wake"`
	Events       []Event `json:"events"`
	Launch       *Launch `json:"launch,omitempty"`
	Disconnected bool    `json:"disconnected,omitempty"`
	TargetClosed bool    `json:"target_closed,omitempty"`
}

type Launch struct {
	CWD        string `json:"cwd"`
	Worktree   string `json:"worktree,omitempty"`
	Launcher   string `json:"launcher,omitempty"`
	Tier       string `json:"tier,omitempty"` // the Agent preset the caller named; a retry by message_id names it too
	Run        Run    `json:"run,omitzero"`   // what Tier resolved to when the launch was accepted (agent_presets.go)
	Pending    bool   `json:"pending"`
	Dispatch   bool   `json:"dispatch,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Title      string `json:"title,omitempty"`
	PromptHash string `json:"prompt_hash,omitempty"`
}

type Task struct {
	db.A2ARow
	taskBody
}

func (t Task) target() Ref { return Ref{t.TargetMachine, t.TargetSession} }
func (t Task) source() Ref { return Ref{t.SourceMachine, t.SourceSession} }

func (t Task) participant(c Caller) bool {
	return t.Owner == c.Owner || c.Ref != (Ref{}) && t.target() == c.Ref
}

func a2aTerminal(state string) bool {
	return state == a2aComplete || state == a2aFailed || state == a2aCanceled
}

func (s *Domain) appendEvent(t *Task, e Event) {
	e.Seq, e.At = len(t.Events)+1, s.clock.Now().UTC()
	if a2aTerminal(e.State) {
		for i := range t.Events {
			old := &t.Events[i]
			if old.Kind != "message" && old.Delivery == a2aQueued {
				old.Delivery = a2aDiscarded
			}
		}
	}
	t.Events = append(t.Events, e)
	if e.State != "" {
		t.State = e.State
	}
}

// MarshalBody encodes the persisted A2A event history and launch.
func (t *Task) MarshalBody() ([]byte, error) { return json.Marshal(t.taskBody) }

// DecodeTask decodes a persisted A2A row and its body.
func DecodeTask(row *db.A2ARow, err error) (*Task, error) {
	if row == nil || err != nil {
		return nil, err
	}
	t := &Task{A2ARow: *row}
	if err := json.Unmarshal(row.Body, &t.taskBody); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Domain) a2aMachineAllowed(ctx context.Context, cfg assistantwire.A2AConfig, machine string) bool {
	return machine != "" && (cfg.Machines == nil || slices.Contains(cfg.Machines, machine)) && s.machines.Trusted(ctx, machine)
}

// Persona binds callers to Node-attested sessions and keeps discovery and delivery
// within the owning manager's confirmed computers.
func (s *Domain) a2aAllowed(ctx context.Context, cfg assistantwire.A2AConfig, ref Ref) bool {
	if ref.Session == "" || !s.a2aMachineAllowed(ctx, cfg, ref.Machine) || !s.visibility.Available(ctx, ref) {
		return false
	}
	if machine := s.nodes.ExecutionMachine(ctx, ref); machine != ref.Machine && !s.a2aMachineAllowed(ctx, cfg, machine) {
		return false
	}
	if row, ok := s.nodes.Listed(ref); ok && (!s.a2aVisible(row) || s.visibility.HiddenByDirectory(ref.Machine, row)) {
		return false
	}
	return !s.visibility.Private(ctx, ref)
}

func (s *Domain) a2aVisible(session proto.Session) bool {
	return !session.Archived && !session.IsDeleted() && !session.HiddenByDirectory() && !session.IsSubagent &&
		!s.visibility.InTrack(session)
}

func (s *Domain) a2aSendable(session proto.Session) bool {
	return s.a2aVisible(session) &&
		(session.Capabilities == nil && session.SourceKind == "" || session.Capabilities != nil && session.Capabilities.Send)
}

func (s *Domain) a2aSession(ctx context.Context, cfg assistantwire.A2AConfig, ref Ref) (proto.Session, error) {
	if !s.a2aAllowed(ctx, cfg, ref) {
		return proto.Session{}, errA2AUnavailable
	}
	for _, sess := range s.a2aSessionsOn(ctx, ref.Machine) {
		if sess.SessionID == ref.Session && s.a2aSendable(sess) {
			return sess, nil
		}
	}
	return proto.Session{}, errA2ANotFound
}

func (s *Domain) a2aSessionsOn(ctx context.Context, machine string) []proto.Session {
	return s.nodes.Sessions(ctx, machine)
}

func a2aValidID(id string) bool {
	return id != "" && len(id) <= 200 && strings.IndexFunc(id, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) < 0
}

func a2aValidText(text string) bool {
	return len(text) <= a2aMaxText && strings.TrimSpace(text) != "" && strings.IndexFunc(text, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t'
	}) < 0
}

func a2aCleanText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	return textfmt.Clip(strings.TrimSpace(text), 2800)
}

func (s *Domain) a2aSave(ctx context.Context, st Store, t *Task) error {
	raw, err := t.MarshalBody()
	if err != nil {
		return err
	}
	t.Body = raw
	var next *time.Time
	if t.State != a2aQueued && (!a2aTerminal(t.State) || slices.ContainsFunc(t.Events, a2aActiveDelivery)) {
		now := s.clock.Now().Add(time.Second)
		next = &now
	}
	status := t.Created
	for i := len(t.Events) - 1; i >= 0; i-- {
		if t.Events[i].State != "" {
			status = t.Events[i].At
			break
		}
	}
	ok, err := st.A2AUpdate(ctx, t.A2ARow, status, next)
	if err != nil {
		return err
	}
	if !ok {
		return errA2AConflict
	}
	t.Revision++
	if a2aTerminal(t.State) {
		s.Wake()
	}
	return nil
}

func a2aActiveDelivery(e Event) bool {
	return e.Delivery == a2aQueued || e.Delivery == a2aSending || e.Delivery == a2aUncertain
}

func (s *Domain) a2aStart(ctx context.Context, st Store, cfg assistantwire.A2AConfig, c Caller, to Ref, id, contextID, text string, wake bool, launch *Launch) (*Task, error) {
	if launch != nil {
		if c.Ref.Session == "" || to.Session != "" || contextID != "" {
			return nil, errA2AInvalid
		}
		if cfg.Machines == nil && !s.admission.Enabled(ctx) {
			return nil, fmt.Errorf("%w: starting a remote session requires Agent dispatch to be enabled", errA2AUnavailable)
		}
		dev, ok := s.machines.Resolve(ctx, to.Machine)
		if !ok || dev.ID != to.Machine || !a2aValidText(launch.CWD) || s.visibility.PrivateLaunchPath(ctx, launch.CWD) {
			return nil, errA2AUnavailable
		}
		launch.CWD = proto.ExpandHome(launch.CWD, dev.Home)
		to.Session = s.newSessionID()
		unlock := s.admission.Lock()
		defer unlock()
	}
	if !a2aValidID(id) || !a2aValidText(text) || contextID != "" && !a2aValidID(contextID) || to == c.Ref {
		return nil, errA2AInvalid
	}
	if !s.a2aAllowed(ctx, cfg, to) {
		return nil, errA2AUnavailable
	}
	if old, err := DecodeTask(st.A2ARequest(ctx, c.Owner, id)); old != nil || err != nil {
		return a2aCheckRetry(old, err, to, contextID, text, wake, launch)
	}
	if launch == nil {
		if _, err := s.a2aSession(ctx, cfg, to); err != nil {
			return nil, err
		}
	} else if err := s.admission.CheckCapacity(ctx, c.Ref, s.clock.Now()); err != nil {
		return nil, err
	}
	if contextID == "" {
		contextID = s.newID()
	} else {
		rows, err := st.A2AList(ctx, c.Owner, to.Machine, to.Session, contextID, "", "", false, nil, 1)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, errA2ANotFound
		}
	}
	t := Task{A2ARow: db.A2ARow{ID: s.newID(), Owner: c.Owner, Request: id, Context: contextID,
		TargetMachine: to.Machine, TargetSession: to.Session, SourceMachine: c.Ref.Machine, SourceSession: c.Ref.Session},
		taskBody: taskBody{Wake: wake, Launch: launch}}
	s.appendEvent(&t, Event{ID: id, Kind: "message", Text: text, State: a2aSubmitted, Delivery: a2aQueued})
	t.Body, _ = t.MarshalBody()
	created, err := DecodeTask(st.A2ACreate(ctx, t.A2ARow))
	return a2aCheckRetry(created, err, to, contextID, text, wake, launch)
}

func a2aCheckRetry(t *Task, err error, to Ref, contextID, text string, wake bool, launch *Launch) (*Task, error) {
	if err != nil {
		return nil, err
	}
	if t == nil || len(t.Events) == 0 {
		return nil, errA2ANotFound
	}
	targetMatches := t.target() == to && t.Launch == nil && launch == nil
	textMatches := t.Events[0].Text == text
	if t.Launch != nil && launch != nil {
		targetMatches = t.TargetMachine == to.Machine && t.Launch.CWD == launch.CWD && t.Launch.Launcher == launch.Launcher &&
			t.Launch.Dispatch == launch.Dispatch && t.Launch.Branch == launch.Branch
		targetMatches = targetMatches && t.Launch.Tier == launch.Tier // the tier as named, not what it resolved to
		if t.Launch.Dispatch {
			textMatches = t.Launch.PromptHash == launch.PromptHash
		}
	}
	if !targetMatches || contextID != "" && t.Context != contextID || !textMatches || t.Wake != wake {
		return nil, errA2ADuplicate
	}
	return t, nil
}

func (s *Domain) a2aGet(ctx context.Context, st Store, cfg assistantwire.A2AConfig, c Caller, id string) (*Task, error) {
	t, err := DecodeTask(st.A2AGet(ctx, id))
	if err != nil {
		return nil, err
	}
	if t == nil || !t.participant(c) || !s.a2aTaskVisible(ctx, cfg, c, t) {
		return nil, errA2ANotFound
	}
	return t, nil
}

func (s *Domain) a2aTaskVisible(ctx context.Context, cfg assistantwire.A2AConfig, c Caller, t *Task) bool {
	if t.SourceSession != "" && !s.a2aAllowed(ctx, cfg, t.source()) {
		return false
	}
	return s.a2aAllowed(ctx, cfg, t.target()) ||
		t.Owner == c.Owner && s.a2aMachineAllowed(ctx, cfg, t.TargetMachine) &&
			(t.TargetClosed || a2aTerminal(t.State) && s.a2aEndpointClosed(ctx, cfg, t.target()))
}

func (s *Domain) a2aMutate(ctx context.Context, st Store, cfg assistantwire.A2AConfig, c Caller, id string, change func(*Task) (bool, error)) (*Task, error) {
	for range 5 {
		t, err := s.a2aGet(ctx, st, cfg, c, id)
		if err != nil {
			return nil, err
		}
		changed, err := change(t)
		if err != nil || !changed {
			return t, err
		}
		if err = s.a2aSave(ctx, st, t); !errors.Is(err, errA2AConflict) {
			return t, err
		}
	}
	return nil, errA2AConflict
}

func (s *Domain) a2aAppendOnce(t *Task, ev Event) (bool, error) {
	for _, old := range t.Events {
		if old.ID == ev.ID {
			if old.Kind != ev.Kind || old.Text != ev.Text || old.State != ev.State {
				return false, errA2ADuplicate
			}
			return false, nil
		}
	}
	limit := a2aMaxEvents - 2
	if a2aTerminal(ev.State) {
		limit = a2aMaxEvents
	}
	if a2aTerminal(t.State) || len(t.Events) >= limit {
		return false, fmt.Errorf("%w: task is closed or its history is full", errA2AInvalid)
	}
	s.appendEvent(t, ev)
	return true, nil
}

func a2aReturn(t *Task, ev Event) Event {
	if t.SourceSession != "" && t.Wake && ev.Kind == "status" {
		ev.Delivery = a2aQueued
	}
	return ev
}

func (s *Domain) a2aContinue(t *Task, id, text string) (bool, error) {
	if !a2aValidID(id) || !a2aValidText(text) {
		return false, errA2AInvalid
	}
	ev := Event{ID: id, Kind: "message", Text: text, State: a2aSubmitted, Delivery: a2aQueued}
	if slices.ContainsFunc(t.Events, func(e Event) bool { return e.ID == id }) {
		return s.a2aAppendOnce(t, ev)
	}
	if t.State != a2aInput {
		return false, fmt.Errorf("%w: task is not waiting for an answer", errA2AInvalid)
	}
	changed, err := s.a2aAppendOnce(t, ev)
	if changed {
		for i := len(t.Events) - 2; i >= 0; i-- {
			if t.Events[i].State == a2aInput {
				if t.Events[i].Delivery == a2aQueued {
					t.Events[i].Delivery = a2aDiscarded
				}
				break
			}
		}
	}
	return changed, err
}

func (s *Domain) a2aCancel(t *Task) (bool, error) {
	if t.State == a2aCanceled {
		return false, nil
	}
	if t.State == a2aInput {
		s.appendEvent(t, Event{ID: s.newID(), Kind: "status", State: a2aCanceled})
		return true, nil
	}
	if t.State != a2aSubmitted && t.State != a2aQueued {
		return false, errA2ACancel
	}
	for i := len(t.Events) - 1; i >= 0; i-- {
		e := &t.Events[i]
		if e.Kind != "message" {
			continue
		}
		if e.Delivery != a2aQueued {
			break
		}
		e.Delivery = a2aDiscarded
		s.appendEvent(t, Event{ID: s.newID(), Kind: "status", State: a2aCanceled})
		return true, nil
	}
	return false, errA2ACancel
}
