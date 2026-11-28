package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/autosupervise"
	"github.com/sunfayao/persona/server/internal/assistant/membership"
	"github.com/sunfayao/persona/server/internal/assistant/signalrule"
	"github.com/sunfayao/persona/server/internal/assistant/taskleader"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// Direction leads supervise the tasks they plan; main supervises its own work (#13362). Explicit transfers
// override that initial assignment (#13344). Ordinary signals and start remedies stay with that supervisor;
// owner-decision escalations go to main. Task leaders' workers keep their leader Agent until it closes.
// Workers cannot hand out Persona tasks and use their runtime's in-session subagents instead.

// defaultPlanSupervisor assigns main. A task leader's
// workers and active reporting conversations retain their specialized topology.
func (s *Service) defaultPlanSupervisor(ctx context.Context, conversation, parent string) (string, bool) {
	if parent != "" || conversation == "" {
		return "", false
	}
	if _, reports := s.reporterOf(ctx, conversation); reports {
		return "", false
	}
	main := s.settings(ctx).Main
	return main, main != ""
}

// planTaskSupervisor resolves ownership before dispatch, including failed starts. Retrying or continuing an
// assigned task must not overwrite a later transfer with the saved plan's original conversation.
func (s *Service) planTaskSupervisor(ctx context.Context, conversation string, task PlanTask) (string, error) {
	cfg := s.settings(ctx)
	supervisor := ""
	for _, id := range []string{task.TodoID, task.ReuseTodo} {
		if t, ok := s.todoNow(id); ok && t.Supervision.assigned() {
			supervisor = t.Supervision.Supervisor
			break
		}
	}
	if supervisor == "" {
		if task.Parent != "" {
			return conversation, nil
		}
		if ref := s.requestSupervisor(ctx, task.PartOf); ref != "" && task.ReuseTodo == "" {
			return ref, nil // a piece of a request its supervisor Agent runs is that Agent's worker
		}
		supervisor, _ = s.defaultPlanSupervisor(ctx, conversation, "")
		if conversation != cfg.Main {
			if c, ok := s.session(ctx, cfg.MachineID, conversation); ok && c.Assistant && runtimeSessionSupportsSupervisionPolicy(c) && !s.isBackground(ctx, conversation) {
				supervisor = conversation
			}
		}
	}
	if supervisor == "" {
		return "", nil
	}
	machine := cfg.MachineID
	if supervisor == cfg.Main {
		machine = cfg.mainRouteMachine()
	}
	if !s.runtimeWithoutSupervisionPolicy(ctx, machine, supervisor) {
		return supervisor, nil
	}
	if !s.runtimeWithoutSupervisionPolicy(ctx, cfg.mainRouteMachine(), cfg.Main) {
		return cfg.Main, nil
	}
	if background := s.runtimeSupervisorFallback(ctx, cfg); background != cfg.Main {
		return cfg.Main, nil
	}
	return "", planInvalid{i18n.Err("assistant-conversations.supervision_reviewer_required")}
}

// effectiveSupervisor resolves current plan/transfer ownership, including needs_user.
func (s *Service) effectiveSupervisor(ctx context.Context, cfg Settings, t Todo) string {
	if t.Supervision == nil || t.Supervision.Supervisor == "" {
		return cfg.Main
	}
	return t.Supervision.Supervisor
}

// requestSupervisor is the reference of the supervisor Agent of the open request id (a piece's part_of) while it has room
// for another worker, else "".
func (s *Service) requestSupervisor(ctx context.Context, id string) string {
	root, ok := s.todoNow(id)
	lead, has := root.supervisorAgent()
	if !ok || !has || root.DoneAt != nil || !autosupervise.On(s.settings(ctx).TodoSupervisor) ||
		s.checkLeaderRoom(ctx, &leaderScope{task: root, agent: lead}, 1) != nil {
		return ""
	}
	return leaderRef(lead.MachineID, lead.SessionID)
}

// rehomeSupervisedTodos brings open legacy side-supervised tasks under main without rewriting their review state or
// criteria. Explicit leader assignments and task-leader workers are the two specialized topologies to keep (#5732).
// A legacy background ledger also stays: that conversation may still accept the old task it already supervised.
func (s *Service) rehomeSupervisedTodos(ctx context.Context) {
	main := s.settings(ctx).Main
	if main == "" {
		return
	}
	todos := s.todosOf(ctx, "*")
	web := s.webBase(ctx)
	// A crash may have persisted Supervisor=main without reaching the queue reconciliation below. Recover those
	// orphaned queued signals first; the candidate loop alone would now skip the task as already rehomed.
	s.rehomeMainQueuedSignals(ctx, main, todos, web)
	rehomed := map[string]map[string]bool{} // old conversation → the tasks taken from its queued supervision
	for _, t := range todos {
		if t.DoneAt != nil || t.Supervision == nil {
			continue
		}
		sup := t.Supervision.Supervisor
		if t.Supervision.assigned() {
			continue
		}
		_, _, worker := parseLeaderRef(sup)
		capableRuntime := sup != "" && t.Origin == sup && s.runtimeWithSupervisionPolicy(ctx, s.settings(ctx).MachineID, sup)
		if sup == main || worker || capableRuntime || s.isBackground(ctx, sup) {
			continue
		}
		if _, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
			if v.Supervisor == main {
				return false
			}
			v.Supervisor = main
			return true
		}); err != nil {
			s.logf("task %s: rehome supervision: %v", t.ID, err)
			continue
		}
		if rehomed[sup] == nil {
			rehomed[sup] = map[string]bool{}
		}
		rehomed[sup][t.ID] = true
		if t.Origin != main {
			s.taskCard(ctx, main, t.ID, "")
		}
	}
	if len(rehomed) > 0 {
		for sup, ids := range rehomed {
			s.rehomeQueuedSupervision(ctx, sup, ids, todos, web)
		}
	}
}

// rehomeMainQueuedSignals recovers queue entries left behind when a previous process stopped after the supervisor write.
// Structured signals name their tasks directly; a pre-structured message uses the same text-id fallback as normal rehome.
func (s *Service) rehomeMainQueuedSignals(ctx context.Context, main string, todos []Todo, web string) {
	mainTasks := map[string]bool{}
	for _, t := range todos {
		if t.DoneAt == nil && t.Supervision != nil && t.Supervision.Supervisor == main {
			mainTasks[t.ID] = true
		}
	}
	if len(mainTasks) == 0 {
		return
	}
	r := &s.relays
	r.mu.Lock()
	if !s.relayLocked(ctx) {
		r.mu.Unlock()
		return
	}
	byConversation := map[string]map[string]bool{}
	for conversation, queue := range r.st.Queue {
		if conversation == main || s.isBackground(ctx, conversation) {
			// An entry explicitly queued for the configured legacy background is intentional compatibility delivery,
			// not an orphaned side signal left by the supervisor write.
			continue
		}
		for _, msg := range queue {
			if msg.Tool != superviseTool {
				continue
			}
			if signals, structured := decodeDigestSignals(msg.Signals); structured {
				for _, g := range signals {
					if mainTasks[g.Todo] {
						if byConversation[conversation] == nil {
							byConversation[conversation] = map[string]bool{}
						}
						byConversation[conversation][g.Todo] = true
					}
				}
				continue
			}
			for id := range mainTasks {
				if strings.Contains(msg.Text, id) {
					if byConversation[conversation] == nil {
						byConversation[conversation] = map[string]bool{}
					}
					byConversation[conversation][id] = true
				}
			}
		}
	}
	r.mu.Unlock()
	for old, ids := range byConversation {
		s.rehomeQueuedSupervision(ctx, old, ids, todos, web)
	}
}

// rehomeQueuedSupervision takes the rehomed tasks' signals out of an old side conversation's durable queue and gathers
// them again under their new supervisor. Queue admission already acknowledged their outbox entries, and their old dedup
// reservation would otherwise acknowledge the replay without delivering it. Other tasks in a mixed digest are regathered
// too, exactly as dropQueuedSupervisionTasks preserves them for /stop (#5732).
func (s *Service) rehomeQueuedSupervision(ctx context.Context, old string, ids map[string]bool, known []Todo, web string) {
	b := &s.supervise
	b.dedupMu.Lock()
	defer b.dedupMu.Unlock()
	b.mu.Lock()
	b.init()
	var memory, remaining []superviseSignal
	for _, g := range b.queued[old] {
		if ids[g.Todo] {
			memory = append(memory, g)
		} else {
			remaining = append(remaining, g)
		}
	}
	var replay []superviseSignal
	_, _ = s.dropQueuedSupervisionTasksAtomic(old, ids, known, func(pending map[string]superviseSignal, queued, regather []superviseSignal) bool {
		replay = mergeSignals(regather, remaining)
		replay = mergeSignals(replay, queued)
		replay = mergeSignals(replay, memory)
		for _, g := range pending { // an earlier attempt may already have committed the replay before its supervisor write
			if ids[g.Todo] {
				replay = mergeSignals(replay, []superviseSignal{g})
			}
		}
		dirty := false
		for _, g := range replay {
			cur, ok := b.pending[g.id()]
			if !ok {
				// First-load rehoming precedes the normal outbox restore. A newer durable stretch with the same id
				// must win over this older queued replay rather than being overwritten before that restore reads it.
				cur, ok = pending[g.id()]
			}
			if !ok {
				cur = g
			}
			if old, exists := pending[g.id()]; !exists || old.Key != cur.Key {
				dirty = true
			}
			b.pending[g.id()] = cur
			pending[g.id()] = cur
		}
		return dirty
	})
	if len(memory) > 0 {
		b.setQueuedLocked(old, nil)
	}
	for _, g := range replay {
		delete(b.sent, g.id())
	}
	b.mu.Unlock()

	if len(replay) > 0 {
		s.forgetSupervision(ctx, replay, web)
		b.mu.Lock()
		s.scheduleDigestLocked(time.Now())
		b.mu.Unlock()
	}
}

// digestTo routes supervision to the persisted planning/transfer assignment. Explicit escalations and card rungs
// keep their special target; unmarked legacy tasks retain their compatibility policy. Task
// leaders' Agent-supervised workers are handled by leaderRoute before this function.
func (s *Service) digestTo(ctx context.Context, cfg Settings, g superviseSignal) string {
	// Explicit escalations keep their destination; needs_user alone is not an escalation.
	if t, ok := s.todoNow(g.Todo); ok && t.Supervision != nil && t.Supervision.assigned() &&
		g.Kind != sigAsk && g.Kind != sigEscalated {
		if t.memberSupervised() {
			return s.conversationAbove(ctx, t) // what its supervisor Agent does not judge
		}
		if !s.agentSupervised(t) {
			return s.effectiveSupervisor(ctx, cfg, t)
		}
	}
	own := s.ownSupervisor(cfg)
	if g.Kind == sigTodoReady || g.Kind == sigParentCloseout {
		// Dependent readiness and parent closeout wakes go to the owning supervisor conversation (Leader or Main chat),
		// never to the background supervisor.
		if g.To != "" && g.To != cfg.BackgroundSupervisor {
			return g.To
		}
		return cfg.Main
	}
	if g.To != "" {
		switch {
		case (g.Kind == sigDecide || g.Kind == sigCheck) && g.To == cfg.Main:
			// A task leader's decision, or the owner's answer to a check's decision on its worker, goes to the conversation
			// above it. ownSupervisor is main for new work (#5732); only legacy queued work keeps a background route.
			return own
		case g.To == cfg.BackgroundSupervisor && g.To != own &&
			(cfg.MainSupervisesOwnTasks != nil && *cfg.MainSupervisesOwnTasks || cfg.BackgroundSupervisorMachineID != cfg.MachineID):
			return cfg.Main // a legacy card named a background supervisor that was switched off or moved
		}
		return g.To
	}
	switch g.Kind {
	case sigEscalated, sigStarted:
		return cfg.Main
	}
	if t, ok := s.todoNow(g.Todo); ok && t.Supervision != nil {
		sup := s.effectiveSupervisor(ctx, cfg, t)
		if g.Kind == sigFailed {
			// Only an explicitly assigned manual leader keeps this route. New task_plan work, including a capable-runtime
			// side chat's, still uses main's failed-start remedies (#5732).
			return cfg.Main
		}
		// The explicit capable-runtime topology is the one side-chat exception to universal main supervision.
		if sup != cfg.Main && sup == t.Origin && s.runtimeWithSupervisionPolicy(ctx, cfg.MachineID, sup) {
			return sup
		}
	}
	return cfg.Main
}

// runtimeSupervisorFallback is the existing background/main compatibility route for work whose runtime supervisor
// cannot receive policy. It is not the supervisor for ordinary new tasks: ownSupervisor deliberately keeps those on main.
func (s *Service) runtimeSupervisorFallback(ctx context.Context, cfg Settings) string {
	if id := cfg.BackgroundSupervisor; s.backgroundOn(cfg) && id != "" && id != cfg.Main &&
		cfg.BackgroundSupervisorMachineID == cfg.MachineID && !s.runtimeWithoutSupervisionPolicy(ctx, cfg.MachineID, id) {
		return id
	}
	return cfg.Main
}

// digestRuntimeTarget applies the runtime policy boundary to a target chosen by digestTo or recheckDigestTo. A main
// target moves to the compatibility route only for a task handed out by an incompatible runtime; ordinary main work
// never inherits the legacy background route.
func (s *Service) digestRuntimeTarget(ctx context.Context, cfg Settings, g superviseSignal, to string) string {
	if to == "" || to == cfg.BackgroundSupervisor {
		return to
	}
	if t, ok := s.todoNow(g.Todo); ok && t.Supervision.assigned() && to == t.Supervision.Supervisor && to != cfg.Main && s.runtimeWithoutSupervisionPolicy(ctx, cfg.MachineID, to) {
		return "" // retain the assigned lead's work until it can receive policy; never route it to main
	}
	runtimeOrigin := false
	if t, ok := s.todoNow(g.Todo); ok {
		runtimeOrigin = s.runtimeWithoutSupervisionPolicy(ctx, cfg.MachineID, t.Origin)
	}
	if to == cfg.Main && !runtimeOrigin {
		return to
	}
	if to != cfg.Main && !s.runtimeWithoutSupervisionPolicy(ctx, cfg.MachineID, to) {
		return to
	}
	if g.Kind == sigFailed || g.Kind == sigTodoReady || g.Kind == sigParentCloseout {
		return cfg.Main // the background supervisor has no start remedies and never receives todo_ready or parent_closeout wakes
	}
	own := s.runtimeSupervisorFallback(ctx, cfg)
	s.logf("supervise: %s runs on a runtime that cannot take supervision policy: its signal goes to %s", short(to), short(own))
	return own
}

// digestTarget is digestTo with the runtime policy boundary applied at delivery time.
func (s *Service) digestTarget(ctx context.Context, cfg Settings, g superviseSignal) string {
	return s.digestRuntimeTarget(ctx, cfg, g, s.digestTo(ctx, cfg, g))
}

// askedIn: one of the entries is a card that came to the supervisor before the user (ask_route.go).
func askedIn(list []prompts.SuperviseEntry) bool {
	return slices.ContainsFunc(list, func(e prompts.SuperviseEntry) bool { return e.Kind == sigAsk })
}

// escalateToMain tells the main conversation that leader task t needs the user (task_review needs_user from its leader):
// why, and the option the leader recommends. The user's decision card goes out as for any needs_user.
func (s *Service) escalateToMain(t Todo, round int, why, recommend string) {
	s.signal(superviseSignal{Todo: t.ID, Kind: sigEscalated, Key: fmt.Sprintf("needs_user@%d@%s", round, time.Now().UTC().Format(time.RFC3339Nano)),
		Detail: clip(oneLine(why), 200), Ask: clip(oneLine(recommend), 200)})
}

// workerRefusal refuses task_plan and agent_start from an agent handed out as a task's agent: it does the work itself,
// with its own in-session subagents, so tasks do not spawn tasks without end. The main conversation and leaders hand
// out freely.
func (s *Service) workerRefusal(ctx context.Context, machine, self string) error {
	if self == "" || s.isMain(ctx, self) {
		return nil
	}
	if s.devs != nil {
		machine = s.devs.Canonical(machine) // as the task's agents are kept
	}
	t, ok := s.taskOf(ctx, machine, self)
	if !ok {
		return nil
	}
	return fmt.Errorf("you are the agent of task %s (%s): an agent handed out as a task does not hand out Persona tasks "+
		"(task_plan, agent_start). Do the work here, with your own in-session subagents if it splits; if it needs another "+
		"task, say so to your supervisor (report_to_supervisor)", t.ID, t.Title)
}

// ---- a cancelled task's agents ----

// cancelStopWait is how long a cancelled task's closed agent may still read busy before it is left unarchived;
// cancelStopPoll how often it is looked at meanwhile.
var cancelStopWait, cancelStopPoll = 30 * time.Second, 500 * time.Millisecond

// stopTaskAgents stops what task t's agents still ask of the user once t is cancelled: each agent on it is closed on
// its computer (session.close: its tab and process go, its transcript stays, and where its engine can resume it a
// message does), its
// cards still waiting end, and it is archived (reversible, nothing deleted). It runs in the background: closing can
// take a while and the review is answered first.
func (s *Service) stopTaskAgents(ctx context.Context, t Todo) {
	var agents []TodoAgent
	for _, a := range t.Agents {
		if a.ReleasedAt == nil && a.DismissedAt == nil && a.SessionID != "" {
			agents = append(agents, a)
		}
	}
	if len(agents) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		for _, a := range agents {
			s.stopCancelledAgent(ctx, t.ID, a.MachineID, a.SessionID)
		}
	}()
}

// stopCancelledAgent stops, ends the cards of and archives one agent of cancelled task id; one that works for another
// open task now, an assistant conversation (the user's to stop) or a session its machine does not list is left alone.
// The agent quits and its terminal stays (session.stop, #4754); a session with no terminal (another tool's, an engine
// that runs agents itself) is closed as before.
func (s *Service) stopCancelledAgent(ctx context.Context, id, machine, session string) {
	s.todos.mu.RLock()
	other, linked := s.todos.activeBySession[[2]string{machine, session}]
	s.todos.mu.RUnlock()
	if linked && other != id {
		s.logf("task %s cancelled: agent %s/%s works for %s now, left alone", id, machine, short(session), other)
		return
	}
	c, err := s.agentArchiveTarget(ctx, machine, session) // refuses an assistant conversation and an unknown session
	if err != nil {
		s.logf("task %s cancelled: agent %s/%s left alone: %v", id, machine, short(session), err)
		return
	}
	machine = c.MachineID
	closed := false
	if !agentCloseBusy(c) && s.closeEndedTerminals(ctx) {
		// A cancelled task ends its idle Agent's session: with the owner's close-on-end setting its terminal goes
		// too (#10355); a busy one is only stopped below and keeps its terminal.
		var err error
		if closed, err = s.closeEndedTerminal(ctx, machine, session, proto.StopUser); err != nil {
			s.logf("task %s cancelled: close agent %s/%s: %v", id, machine, short(session), err)
		}
	}
	if c.Status != proto.StatusOffline && !closed {
		cctx, cancel := context.WithTimeout(ctx, agentArchiveCloseTimeout)
		err := s.call(cctx, machine, deviceSessionStop, map[string]any{"session_id": session}, nil)
		if re := (*rpc.Error)(nil); errors.As(err, &re) && re.Code == "no_terminal" {
			err = s.call(cctx, machine, "session.close", map[string]any{"session_id": session}, nil)
		}
		if err != nil {
			s.logf("task %s cancelled: stop agent %s/%s: %v", id, machine, short(session), err)
		}
		cancel()
	}
	s.endSessionCards(machine, session)
	// The archive waits for the list to show it closed: a row still busy there (working, a card waiting) would bring
	// it straight back (archivedSessions).
	var row proto.Session
	for deadline := time.Now().Add(cancelStopWait); ; time.Sleep(cancelStopPoll) {
		r, err := s.agentArchiveTarget(ctx, machine, session)
		if err != nil {
			s.logf("task %s cancelled: archive agent %s/%s: %v", id, machine, short(session), err)
			return
		}
		if !agentArchiveBusy(r) {
			row = r
			break
		}
		if time.Now().After(deadline) {
			s.logf("task %s cancelled: agent %s/%s still busy, left unarchived", id, machine, short(session))
			return
		}
	}
	changedAt, err := s.noteAgentArchiveAt(ctx, row, true)
	if err != nil {
		s.logf("task %s cancelled: archive agent %s/%s: %v", id, machine, short(session), err)
		return
	}
	row.ArchiveChangedAt = archiveChangedAt(changedAt)
	publishMark, err := s.noteAgentArchiveClosed(ctx, row, time.Now().UTC(), false) // what it writes as it goes is not new work
	if err != nil {
		s.logf("task %s cancelled: archive agent %s/%s: %v", id, machine, short(session), err)
		s.publishList(row.MachineID) // the archive itself committed; secondary metadata failure must not hide it
	} else if publishMark != 0 {
		s.publishListAtArchiveMark(row.MachineID, publishMark)
	}
	s.logf("task %s cancelled: closed and archived agent %s/%s", id, machine, short(session))
}

// endSessionCards ends the cards machine/session still waits on (cancelled in the history), as when its node goes away.
func (s *Service) endSessionCards(machine, session string) {
	s.fleet.mu.Lock()
	var ids []string
	for id, p := range s.fleet.interactions {
		if p.ix.SessionID == session && s.sameMachine(p.ix.MachineID, machine) && (p.prepared != nil || !p.claimed) {
			if p.prepared == nil {
				p.claimed = true
			}
			ids = append(ids, id)
		}
	}
	s.fleet.mu.Unlock()
	s.feishuDetach(ids)
	for _, id := range ids {
		if s.endPreparedByID(id, "cancelled", "The conversation that owned this approval ended; nothing changed.") {
			continue
		}
		s.recordEnd(id, "cancelled", nil)
		s.finishInteraction(id)
	}
}

// ---- the supervisor Agent the hub attaches ----

// superviseAuto: signal g, which would go to the main conversation, is one a supervisor Agent judges
// (taskleader.NeedsJudgment) about an open to-do that conversation dispatched and supervises. The hub then attaches the
// supervisor Agent of its request (attachSupervisor), one start per request (autosupervise.Gate), and the signal waits
// for it: true. False: nothing is attached (the setting is explicit, the to-do is no such to-do, the last attempt failed
// less than autosupervise.Cooldown ago) and the signal goes to the main conversation as before.
func (s *Service) superviseAuto(ctx context.Context, here Settings, g superviseSignal) bool {
	t, ok := s.todoNow(g.Todo)
	if !ok || !autosupervise.On(here.TodoSupervisor) || !taskleader.NeedsJudgment(g.Kind, g.Report) || t.DoneAt != nil || t.Cancelled() ||
		t.Supervision == nil || t.Supervision.Supervisor != here.Main || t.Origin != here.Main {
		return false
	}
	if _, leads := t.supervisorAgent(); leads {
		return false // a supervisor's own to-do (role leader): what reaches main from it is its completion or its question
	}
	root := t
	if p, ok := s.todoNow(t.PartOf); ok && t.PartOf != "" && p.DoneAt == nil && !p.Cancelled() && p.Origin == here.Main {
		root = p
	}
	gate := s.autoGate()
	switch gate.Begin(root.ID) {
	case autosupervise.Fallback:
		return false
	case autosupervise.Wait:
		return true
	}
	s.taskStarting.Add(1)
	go func() {
		defer s.taskStarting.Add(-1)
		err := s.attachSupervisor(root.ID, t.ID, here.Main)
		gate.End(root.ID, err == nil)
		if err != nil {
			s.logf("supervise: request %s: no supervisor Agent for %s (its signals go to the main conversation for %s): %v", root.ID, t.ID, autosupervise.Cooldown, err)
		}
		s.supervise.mu.Lock()
		s.scheduleDigestLocked(time.Now())
		s.supervise.mu.Unlock()
	}()
	return true
}

// autoGate is the book of the supervisors the hub attaches by itself.
func (s *Service) autoGate() *autosupervise.Gate {
	b := &s.supervise
	b.autoOnce.Do(func() {
		b.auto = autosupervise.New(autosupervise.ClockFunc(func() time.Time {
			if b.autoNow != nil {
				return b.autoNow()
			}
			return time.Now()
		}))
	})
	return b.auto
}

// attachSupervisor gives the request root a supervisor Agent (the one it has, else a new one) and moves the ledgers of
// the to-dos it takes over to it (adoptUnder). It fails when trigger, the to-do whose signal asked, stays with above.
func (s *Service) attachSupervisor(rootID, triggerID, above string) error {
	ctx := context.Background()
	root, ok := s.todoNow(rootID)
	if !ok || root.DoneAt != nil {
		return nil // closed meanwhile: nothing to supervise
	}
	lead, has := root.supervisorAgent()
	var err error
	if !has {
		lead, err = s.startSupervisorAgent(ctx, root, above)
	} else if _, listed := s.session(ctx, lead.MachineID, lead.SessionID); !listed {
		err = fmt.Errorf("its supervisor Agent %s is not listed", short(lead.SessionID))
	}
	if err != nil {
		return err
	}
	s.adoptUnder(ctx, root, lead, above, triggerID)
	if t, ok := s.todoNow(triggerID); ok && t.DoneAt == nil && (t.Supervision == nil || t.Supervision.Supervisor != leaderRef(lead.MachineID, lead.SessionID)) {
		return fmt.Errorf("its supervisor Agent %s has no room for it", short(lead.SessionID))
	}
	return nil
}

// adoptable are the to-dos the supervisor Agent of request root takes over from conversation above: root itself when
// above supervises it and Agents of its own work on it, and each open dispatched piece above supervises, trigger first,
// as many as fit under the supervisor's bound given open workers; left are the pieces that do not.
func (s *Service) adoptable(root Todo, above, trigger string, open int) (take, left []Todo) {
	mine := func(t Todo) bool {
		return t.DoneAt == nil && !t.Cancelled() && t.Supervision != nil && t.Supervision.Supervisor == above
	}
	if mine(root) && slices.ContainsFunc(root.Agents, func(a TodoAgent) bool { return a.active() && a.Source != sourceLeader }) {
		take = append(take, root)
	}
	s.todos.mu.RLock()
	var pieces []Todo
	for _, t := range s.todos.todos {
		if t.PartOf == root.ID && t.ID != root.ID && mine(*t) {
			pieces = append(pieces, copyTodoLocked(t))
		}
	}
	s.todos.mu.RUnlock()
	slices.SortFunc(pieces, func(a, b Todo) int {
		return cmp.Or(-cmp.Compare(btoi(a.ID == trigger), btoi(b.ID == trigger)), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	ids := make([]string, len(pieces))
	for i, p := range pieces {
		ids[i] = p.ID
	}
	fit, _ := autosupervise.Fit(open, ids, leaderWorkersMost)
	take = append(take, pieces[:len(fit)]...)
	return take, pieces[len(fit):]
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// adoptUnder moves the ledger supervisor of what adoptable names to supervisor Agent lead (SupervisorSource auto, Above
// the conversation it came from) and has what waited in that conversation's queue gathered again. Agent rows stay as
// they are. What does not fit under the supervisor's bound stays with above.
func (s *Service) adoptUnder(ctx context.Context, root Todo, lead TodoAgent, above, trigger string) {
	ref := leaderRef(lead.MachineID, lead.SessionID)
	take, left := s.adoptable(root, above, trigger, len(s.openLeaderWorkers(root)))
	moved := map[string]bool{}
	for _, t := range take {
		if s.giveToSupervisor(ctx, t.ID, ref, above) {
			moved[t.ID] = true
		}
	}
	if len(left) > 0 {
		s.logf("supervise: supervisor Agent %s of %s has room for no more workers: %d piece(s) stay with %s", short(lead.SessionID), root.ID, len(left), short(above))
	}
	if len(moved) > 0 {
		s.logf("supervise: supervisor Agent %s of %s took over %d to-do(s)", short(lead.SessionID), root.ID, len(moved))
		s.rehomeQueuedSupervision(ctx, above, moved, s.todosOf(ctx, "*"), s.webBase(ctx))
	}
}

// giveToSupervisor makes supervisor Agent ref the ledger supervisor of to-do id, which above supervised or ref already
// does, and records above as the conversation above it. False when the ledger names another supervisor.
func (s *Service) giveToSupervisor(ctx context.Context, id, ref, above string) bool {
	_, changed, err := s.updateSupervision(ctx, id, func(v *Supervision) bool {
		if v.Supervisor != ref && v.Supervisor != above {
			return false
		}
		v.Supervisor, v.SupervisorSource, v.Above = ref, autosupervise.Auto, above
		return true
	})
	if err != nil {
		s.logf("task %s: supervisor Agent: %v", id, err)
	}
	return err == nil && changed
}

// startSupervisorAgent starts the supervisor Agent of request root with the leader defaults (leaderDefaults), linked
// to it as a task leader's Agent is. It goes around takeTodoForTask: the Agents already working on root are not stopped,
// released or briefed again, no to-do or plan card is made, and the owner is asked nothing. The to-do's one start on its
// way (claimTaskStart) keeps a second supervisor from starting beside it.
func (s *Service) startSupervisorAgent(ctx context.Context, root Todo, above string) (TodoAgent, error) {
	st, claimed := s.claimTaskStart(root.ID)
	if !claimed {
		return TodoAgent{}, errors.New(errStartingAgain)
	}
	defer s.endTaskStart(root.ID, st, proto.Session{})
	s.leaderStarts.Add(1) // legacy one-shot tool discovery may wait for this link (task_leader.go)
	defer s.leaderStarts.Add(-1)
	take, _ := s.adoptable(root, above, root.ID, 0)
	note := prompts.TaskSupervisorAttached{}
	for _, t := range take {
		e := prompts.SupervisedTodo{ID: t.ID, Title: t.Title, Criteria: t.Supervision.DoneWhen}
		if a, ok := workerAgent(t); ok {
			e.Machine = a.MachineID
			if d, ok := s.device(ctx, a.MachineID); ok {
				e.Machine = d.Name
			}
		}
		note.Todos = append(note.Todos, e)
	}
	t := PlanTask{Title: root.Title, Role: roleLeader, TodoID: root.ID, supervising: true, Prompt: prompts.RenderTaskSupervisorAttached(note)}
	s.leaderDefaults(ctx, &t)
	if !s.placeTask(ctx, &t) {
		return TodoAgent{}, fmt.Errorf("no computer to start it on (%q)", t.Machine)
	}
	sctx, cancel := context.WithTimeout(ctx, taskStartTimeout)
	defer cancel()
	mid, sid, err := s.startTaskAgent(sctx, &t, false)
	if t.admissionRelease != nil {
		defer func() { t.admissionRelease() }()
	}
	if err != nil {
		return TodoAgent{}, err
	}
	lctx, lcancel := context.WithTimeout(context.WithoutCancel(ctx), taskFailTimeout)
	defer lcancel()
	if err := s.linkPlanAgent(lctx, t, mid, sid, false); err != nil {
		go s.stopCancelledAgent(context.WithoutCancel(ctx), root.ID, mid, sid) // started, but the request closed meanwhile
		return TodoAgent{}, fmt.Errorf("link its supervisor Agent: %w", err)
	}
	cur, _ := s.todoNow(root.ID)
	lead, ok := cur.supervisorAgent()
	if !ok {
		return TodoAgent{}, errors.New("its supervisor Agent was not linked")
	}
	return lead, nil
}

// ---- several Agents on one to-do (membership) ----

// memberView is open to-do id as attach, detach and move see it for caller (a conversation or a supervisor Agent's ref).
// A to-do run by a supervisor Agent for its own pieces (a leader's own to-do) takes no members.
func (s *Service) memberView(caller, id string) (Todo, membership.View, error) {
	t, ok := s.todoNow(strings.TrimSpace(id))
	if !ok {
		return Todo{}, membership.View{}, fmt.Errorf("no to-do %q; nothing was changed", id)
	}
	if _, has := t.supervisorAgent(); has && !t.memberSupervised() {
		return t, membership.View{}, fmt.Errorf("to-do %s is run by its supervisor Agent, which hands out its pieces: nothing was changed", t.ID)
	}
	state := t.Supervision.state(t)
	return t, membership.View{ID: t.ID, Open: t.DoneAt == nil && !t.Cancelled(), Supervised: t.Supervision != nil && t.Supervision.Supervisor == caller,
		InProgress: t.DoneAt == nil && (state == "" || state == reviewRework), Members: t.members()}, nil
}

// setAssignment writes what the Agent machine/session of to-do id answers for (nil: the whole to-do).
func (s *Service) setAssignment(ctx context.Context, id, machine, session string, a *membership.Assignment) error {
	now := time.Now().UTC()
	s.todos.writing.Lock()
	if st, ok := s.todoDB().(interface {
		SetTodoAgentAssignment(ctx context.Context, todoID, machineID, sessionID string, assignment json.RawMessage, at time.Time) error
	}); ok {
		if err := st.SetTodoAgentAssignment(ctx, id, machine, session, a.Encode(), now); err != nil {
			s.todos.writing.Unlock()
			return err
		}
	}
	s.todos.mu.Lock()
	if t := s.todos.todos[id]; t != nil {
		for i := range t.Agents {
			if x := &t.Agents[i]; x.MachineID == machine && x.SessionID == session && x.ReleasedAt == nil {
				x.Assignment, t.UpdatedAt = a.Clone(), now
			}
		}
	}
	s.todos.mu.Unlock()
	s.todos.writing.Unlock()
	s.todosChanged(ctx)
	return nil
}

// memberBrief is what a member is told first: the to-do's goal, its part and the criteria it answers for by number.
func (s *Service) memberBrief(t Todo, a membership.Assignment) string {
	m := prompts.TaskMember{ID: t.ID, Title: t.Title, Goal: t.Title, Part: a.Title, Expect: a.ExpectMinutes}
	if b, ok := s.briefOfTodo(t.ID); ok && b.Goal != "" {
		m.Goal = b.Goal
	}
	var all []string
	if t.Supervision != nil {
		all = t.Supervision.DoneWhen
	}
	for _, n := range a.Numbers(all) {
		m.Criteria = append(m.Criteria, prompts.MemberCriterion{Number: n, Text: all[n-1]})
	}
	return prompts.RenderTaskMember(m)
}

// onlyMember is t with the Agent session alone among its Agents ("": t as it is).
func onlyMember(t Todo, session string) Todo {
	if session != "" {
		t.Agents = slices.DeleteFunc(slices.Clone(t.Agents), func(a TodoAgent) bool { return a.SessionID != session })
	}
	return t
}

// memberFor is the session a message about t is for: the one named (session_id), else its only working member, "" for a
// to-do with none (its one Agent is then as before). Several and none named: a refusal that lists them, and nothing is done.
func (s *Service) memberFor(t Todo, named string) (string, error) {
	title := func(m membership.Member) string {
		if i := slices.IndexFunc(t.Agents, func(a TodoAgent) bool { return a.SessionID == m.Session }); i >= 0 {
			return t.Agents[i].Title
		}
		return ""
	}
	m, found, refusal := membership.Pick(t.members(), named, title)
	if refusal != "" {
		return "", fmt.Errorf("to-do %s: %s", t.ID, refusal)
	}
	if !found {
		return "", nil
	}
	return m.Session, nil
}

// sendToMember is sendToTask to one member of t.
func (s *Service) sendToMember(ctx context.Context, t Todo, session, text string) (string, error) {
	return s.sendToTask(ctx, onlyMember(t, session), text)
}

// reworkMember puts the member's part back to work after a rework sent to it: a round more.
func (s *Service) reworkMember(ctx context.Context, t Todo, session string) {
	for _, a := range t.Agents {
		if a.SessionID == session && a.active() && a.Assignment != nil {
			next := a.Assignment.Clone()
			next.State, next.Round = membership.StateWorking, next.Round+1
			if err := s.setAssignment(ctx, t.ID, a.MachineID, a.SessionID, next); err != nil {
				s.logf("task %s: member %s: %v", t.ID, short(session), err)
			}
		}
	}
}

// memberJoin links agent c of machine am to the to-do id that caller supervises as a member answering for part (action
// attach), or moves it there from the to-do it is on (move). The Agents already there are left as they are. It returns the
// to-do and what the member is told first.
func (s *Service) memberJoin(ctx context.Context, caller string, action membership.Action, id string, am Device, c proto.Session, title string, doneWhen []string, expect int) (Todo, string, error) {
	t, view, err := s.memberView(caller, id)
	if err != nil {
		return Todo{}, "", err
	}
	var from *membership.View
	if cur, on := s.openTodoOfAgent(am.ID, c.SessionID); on && cur.ID != t.ID {
		if action != membership.Move {
			return Todo{}, "", fmt.Errorf("%s works for to-do %s (%q) already: move takes it from there; nothing was changed", agentLabel(c), cur.ID, cur.Title)
		}
		if _, v, err := s.memberView(caller, cur.ID); err != nil {
			return Todo{}, "", err
		} else {
			from = &v
		}
	}
	if err := membership.Check(action, am.ID, c.SessionID, view, from); err != nil {
		return Todo{}, "", err
	}
	var criteria []string
	if t.Supervision != nil {
		criteria = t.Supervision.DoneWhen
	}
	part := cutRunes(oneLine(cmp.Or(strings.TrimSpace(title), strings.TrimSpace(c.Title), agentLabel(c))), 60)
	a, err := membership.NewAssignment(part, doneWhen, expect, criteria)
	if err != nil {
		return Todo{}, "", fmt.Errorf("%w; nothing was changed", err)
	}
	if err := s.linkTodoAgentWithSource(ctx, t.ID, am.ID, c.SessionID, a.Title, c.Cwd, c.Agent, "", "manual", ""); err != nil {
		return Todo{}, "", fmt.Errorf("link the agent: %w", err)
	}
	if err := s.setAssignment(ctx, t.ID, am.ID, c.SessionID, &a); err != nil {
		return Todo{}, "", fmt.Errorf("record its part: %w (it is linked to the to-do as a whole)", err)
	}
	cur, _ := s.todoNow(t.ID)
	return cur, s.memberBrief(cur, a), nil
}

// memberAction is agent_supervise with a todo_id: attach links the Agent as a member, detach lets it go (it keeps
// running and is no longer the to-do's), move does both between two to-dos the caller supervises.
func (s *Service) memberAction(ctx context.Context, self string, action membership.Action, id string, am Device, c proto.Session, title string, doneWhen []string, expect int) (string, error) {
	if action == membership.Detach {
		t, view, err := s.memberView(self, id)
		if err == nil {
			err = membership.Check(action, am.ID, c.SessionID, view, nil)
		}
		if err != nil {
			return "", err
		}
		i := slices.IndexFunc(t.Agents, func(a TodoAgent) bool { return a.MachineID == am.ID && a.SessionID == c.SessionID && a.active() })
		if err := s.releaseStoppedAgents(ctx, t.ID, []TodoAgent{t.Agents[i]}); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s is no member of to-do %s any more: it keeps running, and its work is no longer the to-do's.", agentLabel(c), t.ID), nil
	}
	t, brief, err := s.memberJoin(ctx, self, action, id, am, c, title, doneWhen, expect)
	if err != nil {
		return "", err
	}
	params := map[string]any{"session_id": c.SessionID, "text": brief, "client_msg_id": "member:" + t.ID + ":" + c.SessionID}
	if err := s.call(ctx, am.ID, DeviceSessionSendOnce, params, nil); err != nil {
		return "", fmt.Errorf("%s is a member of to-do %s, but its brief was not delivered: %w. Tell it with agent_send", agentLabel(c), t.ID, err)
	}
	return fmt.Sprintf("%s (%s) is now a member of to-do %s (%q) and was told its part; %d work on it.", agentLabel(c), am.Name, t.ID, t.Title, len(t.workingMembers())), nil
}

// attachMember is agent_send's task.todo_id on a to-do with no supervisor Agent that self supervises: the Agent, which works
// on no open task, joins it as a member whose part is task's; the brief goes with the text.
func (s *Service) attachMember(ctx context.Context, self string, am Device, c proto.Session, task sendTask) (*handedTask, error) {
	if t, busy := s.openTodoOfAgent(am.ID, c.SessionID); busy {
		return nil, fmt.Errorf("%s works for task %s (%q) already: only an Agent on no open task joins a to-do; nothing was sent", agentLabel(c), t.ID, t.Title)
	}
	t, brief, err := s.memberJoin(ctx, self, membership.Attach, task.TodoID, am, c, task.Title, task.DoneWhen, task.ExpectMinutes)
	if err != nil {
		return nil, fmt.Errorf("%w; nothing was sent", err)
	}
	return &handedTask{todo: t, self: self, supervisor: self, brief: brief, member: true, run: SessionRef{MachineID: am.ID, SessionID: c.SessionID}}, nil
}

// joinTasks is task_plan with join: each task starts one more Agent on the open to-do its todo_id names, which caller (a
// conversation, or a supervisor Agent's ref) supervises. The Agents there are not stopped, released or briefed again; the
// to-do keeps its ledger and its one acceptance, and the new member's part is the task's title, done_when (criteria of
// the to-do, by number or text) and expect_minutes. It starts at once with no card, so the main conversation's join
// needs what lets any plan start at once. Refusals come now; a start that fails is told as a failed start.
func (s *Service) joinTasks(ctx context.Context, caller string, tasks []PlanTask, scope *leaderScope) (string, error) {
	type join struct {
		t    PlanTask
		todo Todo
		a    membership.Assignment
	}
	var joins []join
	adding := map[string]int{}
	for i, t := range tasks {
		id := t.ReuseTodo
		if !t.join || id == "" {
			return "", fmt.Errorf("task %d: join adds an Agent to the open to-do todo_id names, and every task of the call joins", i+1)
		}
		todo, view, err := s.memberView(caller, id)
		if err == nil {
			for range adding[id] { // the members this call adds count toward the most
				view.Members = append(view.Members, membership.Member{Session: "new", Active: true})
			}
			err = membership.Check(membership.Attach, "", "", view, nil)
		}
		if err == nil && len(todo.workingMembers()) == 0 {
			err = fmt.Errorf("to-do %s has no Agent working yet: plan it with todo_id alone", todo.ID)
		}
		if err != nil {
			return "", fmt.Errorf("task %d: %w", i+1, err)
		}
		adding[id]++
		if t.Role == roleLeader {
			return "", fmt.Errorf("task %d: a member is a worker: role must be \"worker\"", i+1)
		}
		var all []string
		if todo.Supervision != nil {
			all = todo.Supervision.DoneWhen
		}
		a, err := membership.NewAssignment(t.Title, t.DoneWhen, t.ExpectMinutes, all)
		if err != nil {
			return "", fmt.Errorf("task %d: %w", i+1, err)
		}
		joins = append(joins, join{t, todo, a})
	}
	if scope == nil && !s.planStartsAtOnce(ctx, tasks) { // under a supervisor Agent, taskPlanFrom checked its room already

		return "", errors.New("a join starts at once and has no card for the owner: set readonly, or user_requested when the owner asked for it; nothing was started")
	}
	s.taskStarting.Add(1)
	go func() {
		defer s.taskStarting.Add(-1)
		for _, j := range joins {
			if err := s.startMember(ctx, j.t, j.todo, j.a); err != nil {
				s.logf("task %s: member %q did not start: %v", j.todo.ID, j.a.Title, err)
				now := time.Now().UTC()
				s.signal(superviseSignal{Todo: j.todo.ID, Kind: sigFailed, Machine: j.t.Machine, Key: "member@" + now.Format(time.RFC3339Nano), At: now,
					Generation: now, Detail: fmt.Sprintf("its new member %q did not start on %s: %v", j.a.Title, j.t.Machine, err)})
			}
		}
	}()
	var b strings.Builder
	for _, j := range joins {
		fmt.Fprintf(&b, "- %s joins to-do %s on %s (dispatching)\n", j.a.Title, j.todo.ID, j.t.Machine)
	}
	return b.String() + "This is not confirmation that any Agent started: you are told when one fails to. The Agents already on the to-do were not touched.", nil
}

// startMember starts the Agent of one join task and links it to its to-do with its assignment, as startSupervisorAgent
// does for a supervisor: around takeTodoForTask, so no Agent there is stopped, released or briefed again and no card is made.
func (s *Service) startMember(ctx context.Context, t PlanTask, todo Todo, a membership.Assignment) error {
	ctx = context.WithoutCancel(ctx)
	st, claimed := s.claimTaskStart(todo.ID)
	if !claimed {
		return errors.New(errStartingAgain)
	}
	defer s.endTaskStart(todo.ID, st, proto.Session{})
	t.TodoID, t.Parent, t.DoneWhen, t.Prompt = todo.ID, "", nil, s.memberBrief(todo, a)+"\n\n"+t.Prompt
	sctx, cancel := context.WithTimeout(ctx, taskStartTimeout)
	defer cancel()
	mid, sid, err := s.startTaskAgent(sctx, &t, false)
	if t.admissionRelease != nil {
		defer func() { t.admissionRelease() }()
	}
	if err != nil {
		return err
	}
	lctx, lcancel := context.WithTimeout(ctx, taskFailTimeout)
	defer lcancel()
	if err := s.linkPlanAgent(lctx, t, mid, sid, false); err != nil {
		go s.stopCancelledAgent(ctx, todo.ID, mid, sid) // started, but the to-do closed meanwhile
		return fmt.Errorf("link it: %w", err)
	}
	return s.setAssignment(lctx, todo.ID, mid, sid, &a)
}

// memberDone is a done report from the Agent machine/session of t: when it is a member with a part, the part is marked
// done. It says whether other members still work: then the report is that member's alone, and the to-do's completion waits
// for the last of them (its review is raised when every working member has finished or stopped).
func (s *Service) memberDone(ctx context.Context, t Todo, machine, session string) (othersWork bool) {
	machine = s.canonical(machine)
	i := slices.IndexFunc(t.Agents, func(a TodoAgent) bool { return a.MachineID == machine && a.SessionID == session && a.active() })
	if i < 0 || t.Agents[i].Assignment == nil && len(t.workingMembers()) < 2 {
		return false // one Agent on the whole to-do: its done report completes it, as ever
	}
	next := t.Agents[i].Assignment.Clone()
	if next == nil { // a member from before the others joined answers for what the others do not
		next = &membership.Assignment{Title: t.Agents[i].Title}
	}
	if next.State != membership.StateDone {
		next.State = membership.StateDone
		if err := s.setAssignment(ctx, t.ID, machine, session, next); err != nil {
			s.logf("task %s: member %s: %v", t.ID, short(session), err)
		}
	}
	live := map[string]bool{}
	for _, c := range s.taskAgents(ctx, t) {
		live[c.SessionID] = c.State == proto.StateWorking || c.State == proto.StateNeedsYou || c.PendingInteractions > 0 || c.Starting
	}
	return membership.Others(t.members(), machine, session, func(m membership.Member) bool { return live[m.Session] }) > 0
}

// memberDueKey names the estimate of member m that is due: its session and the moment it fell due.
func memberDueKey(m membership.Member) string {
	return "member@" + m.Session + "@" + m.DueAt().UTC().Format(time.RFC3339)
}

// memberClocks runs the estimate clock of each working member that has an estimate of its own (the others keep the
// to-do's, briefClock): a member that made progress lately gets its estimate moved later on its own, as a to-do's is
// (extendEstimate); else its supervisor hears that it is due, an estimate_due signal that names the member's session.
func (s *Service) memberClocks(ctx context.Context, t Todo, now time.Time) {
	if t.Supervision == nil || t.DoneAt != nil || !s.memberRunning(t) {
		return
	}
	for _, m := range t.workingMembers() {
		if due := m.DueAt(); due.IsZero() || now.Before(due) {
			continue
		}
		expect := m.Assignment.ExpectMinutes
		since := now.Add(-signalrule.ProgressWindow(expect))
		if next, ok := signalrule.Extension(expect, m.Assignment.Extended, s.progressAfter(m.Machine, m.Session, since)); ok {
			a := m.Assignment.Clone()
			a.ExpectMinutes, a.Extended = next, a.Extended+1
			if err := s.setAssignment(ctx, t.ID, m.Machine, m.Session, a); err != nil {
				s.logf("task %s: member %s: extended estimate: %v", t.ID, short(m.Session), err)
			}
			continue
		}
		s.signal(superviseSignal{Todo: t.ID, Kind: sigEstimateDue, Machine: m.Machine, Session: m.Session, Key: memberDueKey(m), At: now,
			Minutes: int(now.Sub(m.LinkedAt).Minutes())})
	}
}

// memberRunning: t goes on (neither waiting for review nor with the owner nor parked): its members' clocks run.
func (s *Service) memberRunning(t Todo) bool {
	switch t.Supervision.state(t) {
	case reviewPending, reviewNeedsUser, reviewParked, reviewTimedOut, reviewClosing:
		return false
	}
	return true
}

// memberDue is the minutes expected of the member signal g (an estimate_due naming a session) is about, while its
// estimate is still the one that fell due and t goes on.
func (s *Service) memberDue(t Todo, g superviseSignal) (int, bool) {
	for _, m := range t.workingMembers() {
		if m.Session == g.Session && !m.DueAt().IsZero() {
			return m.Assignment.ExpectMinutes, g.Key == memberDueKey(m) && s.memberRunning(t)
		}
	}
	return 0, false
}
