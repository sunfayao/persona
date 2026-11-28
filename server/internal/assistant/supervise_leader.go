package assistant

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/autosupervise"
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
