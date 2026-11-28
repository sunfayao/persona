package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/ownerfence"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The owner's message preempts a background round (docs/assistant/assistant.md "Interjecting while it answers",
// #4926). A round the owner did not start (a supervision message, a scheduled task or follow-up, another
// conversation's message, a task's notice) can sit behind a long foreground command for minutes. When the owner's own
// message reaches a conversation on such a round, it carries interject_background whatever the setting says, so the
// node moves the running command to the background and the model reads the owner at once. On a round of the
// background supervisor the node first waits for the round's turn to end, up to 30 s, so the owner's message goes into
// a process with every tool (node/assistant_round.go awaitRoundTurn); only past that does it preempt the round. For a
// round the owner started, the setting decides as before.
//
// A mark per conversation says which message started the round it is on. It is set when the manager sends a
// background message into a conversation not seen working (call), and it lasts as long as that round: every message
// of the owner's during it is preempting, not only the first, and reading it changes nothing, so a message that fails
// to go out costs nothing. It ends when a session list shows the conversation stopped (roundsSeen), or when another
// message of the manager's starts a round there.
//
// A background message sent into a working conversation joins the round in flight, whose starter stands, unless
// Claude has no tool boundary left to take it in at: then it runs as a turn of its own when that round ends. The
// manager cannot tell which from the list, so the message is remembered (next) and becomes the mark when the round
// is seen to end; the mark is dropped again if the conversation then stays stopped, or if the owner speaks before it
// is seen working (their message starts the round).
//
// The marks are in memory only: after a restart the setting decides until the next background round, which is what
// best effort already means here. Whether a command is running at all is the node's to say
// (assistantProc.interjects: an idle conversation is unaffected), not the manager's list, which can be seconds old.

// backgroundRoundKeep is how long a background round's mark is kept when no session list ever says it stopped.
const backgroundRoundKeep = 24 * time.Hour

// backgroundRoundStartWait is how long a mark waits for a session list to show its round working before it is
// taken for a round that never ran, or ran and ended between two lists.
const backgroundRoundStartWait = time.Minute

// roundNotice is the mark's kind for a task's notice, which carries no message source of its own.
const roundNotice = "notice"

// backgroundRound is one conversation's mark.
type backgroundRound struct {
	kind    string    // the kind of message that started the round it is on; "" while that is no background one (only next is set)
	at      time.Time // when that message was sent, or the round before it was seen to end (queued)
	running bool      // a session list has shown the conversation working since
	queued  bool      // its message was sent into a working conversation, and the round it waited behind has ended
	next    string    // the kind of a background message sent while the conversation was working, "" when none
}

type preemptBook struct {
	mu     sync.Mutex
	rounds map[string]backgroundRound // by conversation id
}

// backgroundRoundKind is the kind of round text with source starts when it is one the owner did not start, else "".
// Persona continuing the owner's own work (a usage limit that reset, a wake for what it waits on) is not one, as it
// is no background round in the apps ("Background rounds").
func backgroundRoundKind(text string, source *proto.MessageSource) string {
	if source == nil {
		if in := prompts.ParseInjected(text); in != nil && in.Kind == proto.InjectedTaskNotice {
			return roundNotice
		}
		return ""
	}
	switch source.Kind {
	case proto.SourceSupervision, proto.SourceConversation, proto.SourcePatrol:
		return source.Kind
	case proto.SourceFollowup:
		if in := prompts.ParseInjected(text); in != nil && (in.State == "limit_continue" || in.State == "wake") {
			return ""
		}
		return source.Kind
	}
	return ""
}

// roundStart is an assistant.send read before it is sent: of kind ("" when it is no background message) to
// conversation id ("" for a new conversation, named by the node's answer), which the last session list showed
// working (joins) or not.
type roundStart struct {
	id, kind string
	joins    bool
}

// roundStarting reads the assistant.send p to machine before it is sent: its conversation is working right after.
func (s *Service) roundStarting(machine string, p map[string]any, kind string) *roundStart {
	id, _ := p["session_id"].(string)
	return &roundStart{id: id, kind: kind, joins: id != "" && s.seenWorking(machine, id)}
}

// sent: the send was accepted with res. A message into a conversation not seen working starts its round, and the
// mark is that round's; a background one into a working conversation is remembered for when that round ends.
func (r *roundStart) sent(s *Service, res json.RawMessage) {
	if r == nil {
		return
	}
	id := r.id
	if id == "" {
		var out struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(res, &out) != nil || out.SessionID == "" {
			return
		}
		id = out.SessionID
	}
	b := &s.preempt
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for k, m := range b.rounds {
		if now.Sub(m.at) > backgroundRoundKeep {
			delete(b.rounds, k)
		}
	}
	if b.rounds == nil {
		b.rounds = map[string]backgroundRound{}
	}
	switch m, marked := b.rounds[id]; {
	case r.joins && r.kind == "":
	case r.joins:
		if !marked {
			m = backgroundRound{at: now, running: true} // the round in flight is not a background one
		}
		m.next = r.kind
		b.rounds[id] = m
	case r.kind == "":
		delete(b.rounds, id)
	default:
		b.rounds[id] = backgroundRound{kind: r.kind, at: now}
	}
}

// roundsSeen takes a node's session list (setSessions): a marked conversation it shows working is on its round, and
// one it shows stopped after that has ended it. A background message that waited behind the ended round may start
// the next one. A mark never seen working is dropped after backgroundRoundStartWait.
func (s *Service) roundsSeen(list []proto.Session) {
	b := &s.preempt
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.rounds) == 0 {
		return
	}
	now := time.Now()
	for i := range list { // by index: a range value copies every (large) session it passes
		id, status := list[i].SessionID, list[i].Status
		m, marked := b.rounds[id]
		if !marked {
			continue
		}
		switch {
		case status == proto.StatusWorking:
			if !m.running {
				m.running = true
				b.rounds[id] = m
			}
		case status != proto.StatusDone && status != proto.StatusIdle && status != proto.StatusInterrupted:
			// offline, unknown: nothing said about its round (a reconnect can come back to the same turn)
		case m.running && m.next != "":
			b.rounds[id] = backgroundRound{kind: m.next, at: now, queued: true}
		case m.running || now.Sub(m.at) > backgroundRoundStartWait:
			delete(b.rounds, id)
		}
	}
}

// ownerPreempts: the owner's message is about to go to conversation id; whether the round there is one the owner did
// not start, so the message carries interject_background. Read only: the mark stands for the owner's later messages
// in the same round, and for this one sent again.
func (s *Service) ownerPreempts(id string) bool {
	b := &s.preempt
	b.mu.Lock()
	m, ok := b.rounds[id]
	b.mu.Unlock()
	if !ok || m.kind == "" || (!m.running && time.Since(m.at) > backgroundRoundStartWait) {
		return false
	}
	s.logf("assistant: the owner's message reaches %s on a round a %s message started: it moves foreground work to the background if a command still runs there (a node or Claude Code too old for it leaves the message for the next tool boundary)", short(id), m.kind)
	return true
}

// ownerSent: the owner's message to conversation id is on its way. A mark whose message waited behind another round
// and has not been seen working since is dropped: the list shows the conversation stopped, so the owner's message
// starts the round.
func (s *Service) ownerSent(id string) {
	b := &s.preempt
	b.mu.Lock()
	defer b.mu.Unlock()
	if m, ok := b.rounds[id]; ok && m.queued && !m.running {
		delete(b.rounds, id)
	}
}

// seenWorking: machine's last session list showed conversation id working.
func (s *Service) seenWorking(machine, id string) bool {
	if s.devs != nil {
		machine = s.devs.Canonical(machine)
	}
	return s.fleetPrevious(machine)[id].Status == proto.StatusWorking
}

func (s *Service) ownerInputRef(machine, session string) ownerfence.Ref {
	if s.devs != nil {
		machine = s.devs.Canonical(machine)
	}
	return ownerfence.Ref{Machine: machine, Session: session}
}

func (s *Service) recordDirectOwnerInput(p assistantwire.UserSpokeParams) {
	if p.MachineID == "" || p.SessionID == "" || strings.TrimSpace(p.Text) == "" {
		return
	}
	s.ownerInputs.Record(s.ownerInputRef(p.MachineID, p.SessionID), ownerfence.Input{
		At: time.Now(), Text: clip(p.Text, 1200), ClientMsgID: p.ClientMsgID, ByOwner: true,
	})
}

// mainOwnerInput grants proxy authority only after the current main turn can read the exact authenticated input.
// A message still waiting in the main conversation's outbox does not give the earlier turn new authority.
func (s *Service) mainOwnerInput(ctx context.Context, supervisor ownerfence.Ref) ownerfence.Input {
	input := s.ownerInputs.Latest(supervisor)
	if input.Seq == 0 || !s.isMain(ctx, supervisor.Session) {
		return ownerfence.Input{}
	}
	var result struct{ Items []proto.Item }
	if err := s.call(ctx, supervisor.Machine, DeviceSessionItems, map[string]any{
		"session_id": supervisor.Session, "turns": 1, "lazy_images": true,
	}, &result); err != nil {
		return ownerfence.Input{}
	}
	if ownerfence.MatchesOwnerInput(result.Items, input) {
		return input
	}
	return ownerfence.Input{}
}

func (s *Service) supervisoryAction(ctx context.Context, machine, self, targetMachine, targetSession string, askedAt ...time.Time) (context.Context, error) {
	supervisor := s.ownerInputRef(machine, self)
	a := s.ownerInputs.Action(supervisor, s.ownerInputRef(targetMachine, targetSession), askedAt...)
	a.Owner = s.mainOwnerInput(ctx, supervisor)
	if input := s.ownerInputs.Check(a); input.Seq != 0 {
		return ctx, ownerfence.Refusal(input)
	}
	if len(askedAt) == 0 {
		if err := s.workerQuestionAuthority(ctx, a); err != nil {
			return ctx, err
		}
	}
	return ownerfence.WithAction(ctx, a), nil
}

// admitSupervisedWrite rechecks after all confirmations, task lookups and resume preparation, at the node write.
// The same context survives offline task sends, compaction and interrupt-with-follow-up.
func (s *Service) admitSupervisedWrite(ctx context.Context, machine, method string, params any) error {
	a, ok := ownerfence.ActionOf(ctx)
	if !ok {
		return nil
	}
	session, text := ownerfence.WriteTarget(method, params)
	if session == "" || s.ownerInputRef(machine, session) != a.Target {
		return nil
	}
	if err := s.ownerInputs.CheckContext(ctx); err != nil {
		return err
	}
	if err := s.workerQuestionAuthority(ctx, a); err != nil {
		return err
	}
	var relayed ownerfence.Input
	if a.Owner.Seq != 0 && strings.TrimSpace(text) != "" && !exitText(text) {
		relayed = ownerfence.Input{At: time.Now(), Text: a.Owner.Text, ByOwner: true, RelayOf: a.Owner.Seq}
	}
	if input := s.ownerInputs.Admit(a, relayed); input.Seq != 0 {
		return ownerfence.Refusal(input)
	}
	return nil
}

func (s *Service) recordTaskOwnerDecision(t Todo, said string, observer ownerfence.Ref) {
	input := ownerfence.Input{At: time.Now(), Text: clip(said, 1200), ByOwner: true}
	for _, agent := range t.Agents {
		if agent.SessionID != "" && agent.Source != sourceLeader {
			target := s.ownerInputRef(agent.MachineID, agent.SessionID)
			s.ownerInputs.Observe(observer, target, s.ownerInputs.Record(target, input))
		}
	}
}

func (s *Service) ownerInputAfterSignal(t Todo, g superviseSignal) bool {
	if g.Session != "" {
		return s.ownerInputs.Latest(s.ownerInputRef(g.Machine, g.Session)).At.After(g.At)
	}
	if agent, ok := workerAgent(t); ok {
		return s.ownerInputs.Latest(s.ownerInputRef(agent.MachineID, agent.SessionID)).At.After(g.At)
	}
	return false
}

// On an assigned worker question the leader gets the ordinary wake. Once it escalates the decision, the upper
// conversation owns the answer. An authenticated instruction from the owner always takes precedence.
func (s *Service) workerQuestionAuthority(ctx context.Context, a ownerfence.Action) error {
	if a.Owner.Seq != 0 || a.Supervisor.Session == "" {
		return nil
	}
	s.fleet.mu.Lock()
	for _, p := range s.fleet.interactions {
		if p.claimed || p.ix.Kind != proto.IxQuestion || p.ix.MachineID != a.Target.Machine || p.ix.SessionID != a.Target.Session {
			continue
		}
		if p.ownerMain.SessionID != "" {
			s.fleet.mu.Unlock()
			return ownerfence.WorkerQuestionError(true, true, false)
		}
		if to := p.route.at(); to != "" && to != a.Supervisor.Session && to != leaderRef(a.Supervisor.Machine, a.Supervisor.Session) {
			s.fleet.mu.Unlock()
			return ownerfence.WorkerQuestionError(false, false, true)
		}
	}
	s.fleet.mu.Unlock()
	t, ok := s.taskOf(ctx, a.Target.Machine, a.Target.Session)
	if !ok {
		return nil
	}
	lead, _, reachable := s.leaderSupervisor(t)
	if !reachable {
		return nil
	}
	isLeader := a.Supervisor == s.ownerInputRef(lead.MachineID, lead.SessionID)
	waitingOnOwner := t.Supervision.state(t) == reviewNeedsUser
	waitingOnLeader := false
	if row, ok := s.decoratedSession(a.Target.Machine, a.Target.Session); ok {
		waitingOnLeader = row.State == proto.StateNeedsYou || row.PendingInteractions > 0
	}
	return ownerfence.WorkerQuestionError(isLeader, waitingOnOwner, waitingOnLeader)
}

type relayedOwnerDecisionKey struct{}

func (s *Service) supervisedWriteDelivered(ctx context.Context, machine, method string, params any) {
	sent, ok := ctx.Value(relayedOwnerDecisionKey{}).(string)
	if !ok || method != DeviceSessionSend && method != DeviceSessionStart {
		return
	}
	a, ok := ownerfence.ActionOf(ctx)
	if !ok || a.Owner.Seq == 0 || s.canonical(machine) != a.Target.Machine {
		return
	}
	p, ok := params.(map[string]any)
	if !ok {
		return
	}
	if method == DeviceSessionStart && p["prompt"] == nil { // only the second, post-compaction request carries the instruction
		return
	}
	id := s.activeTodoOf(a.Target.Machine, a.Target.Session)
	if id == "" {
		return
	}
	// The async receiver shares the task action lock with a card, a default or task_decided.
	s.inOrder(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveSendWait)
		defer cancel()
		s.relayedOwnerDecision(ctx, id, sent, a)
	})
}
