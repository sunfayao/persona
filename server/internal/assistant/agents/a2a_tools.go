package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

func SessionCaller(ref Ref) Caller {
	return Caller{Ref: ref, Owner: "session:" + ref.Machine + "/" + ref.Session}
}

func (s *Domain) Tool(ctx context.Context, ref Ref, name string, args json.RawMessage) proto.ToolResult {
	cfg := s.config(ctx)
	if !s.a2aAllowed(ctx, cfg, ref) {
		return proto.TextResult(errA2AUnavailable.Error(), true)
	}
	st, err := s.store()
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	out, err := s.a2aToolCall(ctx, st, cfg, SessionCaller(ref), name, args)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	if text, ok := out.(string); ok {
		// a2a_read: a conversation to read, as agent_read gives it; it changes no task, so nothing to deliver.
		return proto.TextResult(text, false)
	}
	s.Wake()
	data, err := json.Marshal(out)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return proto.TextResult(string(data), false)
}

type a2aToolArgs struct {
	Ref
	Task     string `json:"task_id"`
	ID       string `json:"message_id"`
	Context  string `json:"context_id"`
	Text     string `json:"text"`
	State    string `json:"state"`
	Wake     *bool  `json:"wake"`
	Before   string `json:"before"`
	CWD      string `json:"cwd"`
	Launcher string `json:"launcher"`
	Tier     string `json:"tier"`
	Turns    int    `json:"turns"`
	Presets  bool   `json:"presets"`
}

type a2aTaskView struct {
	ID           string  `json:"task_id"`
	Context      string  `json:"context_id"`
	Source       Ref     `json:"source"`
	Target       Ref     `json:"target"`
	State        string  `json:"state"`
	Queued       bool    `json:"queued,omitempty"`
	Events       []Event `json:"events"`
	Disconnected bool    `json:"target_disconnected,omitempty"`
}

func (t *Task) view() a2aTaskView {
	return a2aTaskView{ID: t.ID, Context: t.Context, Source: t.source(), Target: t.target(), State: t.State, Queued: t.State == a2aQueued, Events: t.Events, Disconnected: t.Disconnected}
}

func (s *Domain) a2aToolCall(ctx context.Context, st Store, cfg assistantwire.A2AConfig, c Caller, name string, args json.RawMessage) (any, error) {
	var a a2aToolArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errA2AInvalid
	}
	a.Ref = s.a2aResolve(ctx, cfg, a.Ref) // an id the session's conversation went by: its row now (a2a_resolve.go)
	var t *Task
	var err error
	switch name {
	case "a2a_sessions":
		if a.Presets { // and where a2a_send can start an Agent, with the Agent presets each computer can start
			return map[string]any{"sessions": s.a2aDiscover(ctx, cfg, c.Ref, a.Machine), "computers": s.a2aComputers(ctx, cfg, a.Machine)}, nil
		}
		return s.a2aDiscover(ctx, cfg, c.Ref, a.Machine), nil
	case "a2a_read":
		return s.a2aRead(ctx, cfg, c.Ref, a.Ref, a.Turns)
	case "a2a_get":
		if a.Task == "" {
			return s.a2aInbox(ctx, st, cfg, c, a.Before)
		}
		t, err = s.a2aGet(ctx, st, cfg, c, a.Task)
	case "a2a_send":
		if a.Task == "" {
			var launch *Launch
			if a.CWD != "" {
				if err := s.outbound.AllowLaunch(ctx, c.Ref); err != nil {
					return nil, err
				}
				launch = &Launch{CWD: a.CWD, Launcher: a.Launcher, Tier: strings.TrimSpace(a.Tier), Pending: true}
			} else if err := s.outbound.AllowSend(ctx, c.Ref, a.Ref, a.Text); err != nil {
				return nil, err
			}
			if old, err := DecodeTask(st.A2ARequest(ctx, c.Owner, a.ID)); err == nil && old == nil && a2aValidID(a.ID) && a2aValidText(a.Text) &&
				a.Ref != c.Ref && (launch != nil || s.a2aAllowed(ctx, cfg, a.Ref)) {
				// A new hand-off, not a retry of one already made (a2aStart answers those).
				where := a.CWD
				if launch != nil {
					// Its tier is resolved, or refused, before anything is asked or recorded (agent_presets.go), and so
					// is a start that could not run read-only where it would go (start_host.go).
					if err := s.a2aLaunchTier(ctx, cfg, a.Machine, launch); err != nil {
						return nil, err
					}
					if err := s.a2aLaunchCheck(ctx, cfg, a.Ref, a.Context, launch); err != nil {
						return nil, err
					}
					where = s.launches.DescribeTier(ctx, launch)
				}
				if err := s.a2aConfirm(ctx, c.Ref, a.Ref, where, a.Text, args); err != nil {
					return nil, err
				}
			}
			t, err = s.a2aStart(ctx, st, cfg, c, a.Ref, a.ID, a.Context, a.Text, a.Wake == nil || *a.Wake, launch)
			break
		}
		if t, err := s.a2aGet(ctx, st, cfg, c, a.Task); err == nil && t.Owner == c.Owner && t.State == a2aInput &&
			a2aValidID(a.ID) && a2aValidText(a.Text) && !slices.ContainsFunc(t.Events, func(e Event) bool { return e.ID == a.ID }) {
			// An answer that would be sent, not a retry of one already sent (a2aContinue).
			if err := s.outbound.AllowSend(ctx, c.Ref, t.target(), a.Text); err != nil {
				return nil, err
			}
			if err := s.a2aConfirm(ctx, c.Ref, t.target(), "", a.Text, args); err != nil {
				return nil, err
			}
		}
		t, err = s.a2aMutate(ctx, st, cfg, c, a.Task, func(t *Task) (bool, error) {
			if t.Owner != c.Owner {
				return false, errA2ANotFound
			}
			if err := s.outbound.AllowSend(ctx, c.Ref, t.target(), a.Text); err != nil {
				return false, err
			}
			return s.a2aContinue(t, a.ID, a.Text)
		})
	case "a2a_report":
		t, err = s.a2aMutate(ctx, st, cfg, c, a.Task, func(t *Task) (bool, error) {
			return s.a2aReport(t, c.Ref, a.ID, a.State, a.Text)
		})
	case "a2a_cancel":
		t, err = s.a2aMutate(ctx, st, cfg, c, a.Task, func(t *Task) (bool, error) {
			if t.Owner != c.Owner {
				return false, errA2ANotFound
			}
			return s.a2aCancel(t)
		})
	default:
		return nil, errA2AInvalid
	}
	if err != nil {
		return nil, err
	}
	return t.view(), nil
}

// errA2ADeclined: the user declined a hand-off on the phone.
const errA2ADeclined a2aError = "the user declined on the phone: nothing was sent"

// a2aConfirm asks the phone before an assistant conversation hands text to another session, or to a new one in
// folder where, through A2A (health.go confirmOutbound: at once when the text needs no confirming). Other senders are
// not asked.
func (s *Domain) a2aConfirm(ctx context.Context, from, to Ref, where, text string, args json.RawMessage) error {
	if !s.outbound.NeedsConfirmation(ctx, from) {
		return nil
	}
	label := to.Machine
	if m, ok := s.machines.Lookup(ctx, to.Machine); ok && m.Name != "" {
		label = m.Name
	}
	if where != "" {
		label += " · " + where
	}
	ok, err := s.outbound.Confirm(ctx, from, text, label+"\n\n"+text, args)
	if err != nil {
		return err
	}
	if !ok {
		return errA2ADeclined
	}
	return nil
}

func (s *Domain) a2aReport(t *Task, sender Ref, id, state, text string) (bool, error) {
	if sender != t.target() {
		return false, errA2ANotFound
	}
	if !a2aValidID(id) || !a2aValidText(text) {
		return false, errA2AInvalid
	}
	kind := "status"
	switch state {
	case "progress":
		kind, state = "progress", ""
	case a2aInput, a2aComplete, a2aFailed:
	default:
		return false, fmt.Errorf("%w: state must be progress, input_required, completed or failed", errA2AInvalid)
	}
	ev := a2aReturn(t, Event{ID: id, Kind: kind, State: state, Text: text})
	if !slices.ContainsFunc(t.Events, func(e Event) bool { return e.ID == id }) {
		for i := len(t.Events) - 1; i >= 0; i-- {
			e := &t.Events[i]
			if e.Kind != "message" {
				continue
			}
			// A report from the verified recipient can arrive ahead of the send's acknowledgment.
			if e.Delivery == a2aSending || e.Delivery == a2aUncertain {
				e.Delivery = a2aDelivered
			}
			if e.Delivery != a2aDelivered {
				return false, errA2AConflict
			}
			if t.State == a2aSubmitted && kind == "progress" {
				s.appendEvent(t, Event{ID: s.newID(), Kind: "status", State: a2aWorking})
			}
			break
		}
	}
	return s.a2aAppendOnce(t, ev)
}

func (s *Domain) a2aInbox(ctx context.Context, st Store, cfg assistantwire.A2AConfig, c Caller, before string) (any, error) {
	out := []a2aTaskView{}
	all, err := st.A2AList(ctx, c.Owner, c.Ref.Machine, c.Ref.Session, "", "", before, true, nil, 21)
	if err != nil {
		return nil, err
	}
	next := ""
	for i, row := range all {
		if i == 20 {
			next = all[i-1].ID
			break
		}
		t, err := DecodeTask(&row, nil)
		if err != nil {
			return nil, err
		}
		if s.a2aTaskVisible(ctx, cfg, c, t) {
			v := t.view()
			// The full history is available when the caller selects one task.
			v.Events = v.Events[len(v.Events)-1:]
			out = append(out, v)
		}
	}
	return map[string]any{"tasks": out, "next": next}, nil
}

func (s *Domain) a2aDiscover(ctx context.Context, cfg assistantwire.A2AConfig, self Ref, machine string) any {
	type peer struct {
		Ref
		Title  string `json:"title"`
		Agent  string `json:"agent,omitempty"`
		Status string `json:"status"`
	}
	out := []peer{}
	machines := cfg.Machines
	if machines == nil {
		for _, d := range s.machines.List(ctx) {
			machines = append(machines, d.ID)
		}
	}
	for _, mid := range machines {
		if machine != "" && mid != machine {
			continue
		}
		for _, sess := range s.a2aSessionsOn(ctx, mid) {
			ref := Ref{mid, sess.SessionID}
			if ref != self && s.a2aSendable(sess) && s.a2aAllowed(ctx, cfg, ref) {
				out = append(out, peer{ref, sess.Title, sess.Agent, sess.Status})
			}
		}
	}
	return out
}
