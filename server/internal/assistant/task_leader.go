package assistant

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sunfayao/persona/server/internal/assistant/acceptance"
	"github.com/sunfayao/persona/server/internal/assistant/autosupervise"
	"github.com/sunfayao/persona/server/internal/assistant/criteria"
	"github.com/sunfayao/persona/server/internal/assistant/digestfmt"
	"github.com/sunfayao/persona/server/internal/assistant/ownerfence"
	"slices"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/membership"
	"github.com/sunfayao/persona/server/internal/assistant/taskleader"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Task leaders (#3656). A task the main conversation or a leader
// conversation hands out with task_plan role "leader" has a coding agent that leads it: it gets a restricted task_plan
// of its own and hands workers to its task, and what the workers report goes to it. Depth stops there: dot (a leader
// conversation) → task leader → worker; a worker hands out nothing (workerRefusal).
//
// What is kept, with no new column or table: the leader's todo_agents row has source "leader" (set when its agent is
// linked from the plan task's Role, which the saved plan keeps), and each worker's row has source "worker" and via the
// leader's task id. A worker's todo is a supervised task like any other, with the leader's agent session as the
// ledger's supervisor (so the API shows it, and a restart keeps it) and the task's done_when. Where a supervisor is a
// conversation everything goes to that conversation; here the same signals, the worker's reports included, are one
// message into the leader agent's session instead (deliverToLeaderAgent, called from sendDigest), and the leader closes
// its workers with a task_review of its own that reaches its direct workers only (leaderTaskReview). None of it goes
// through digestTo, which would send a session that is no conversation to the main chat. What a leader needs the user
// for goes up to the conversation that handed it its task (sigDecide).

const (
	roleLeader = "leader"
	// sourceLeader and sourceWorker are todo_agents.source of a task leader's agent and of its workers' (via: the
	// leader's task id), as sourceReview marks a reviewer.
	sourceLeader = "leader"
	sourceWorker = "worker"
	// leaderWorkersMost bounds the workers of one leader that are open at a time.
	leaderWorkersMost = 8
)

// leaderListWait bounds only legacy one-shot/direct discovery while a leader start reaches its link. Revision-aware
// watches use their cursor and heartbeat instead.
var leaderListWait = 8 * time.Second

func (a TodoAgent) active() bool { return a.ReleasedAt == nil && a.DismissedAt == nil }

// supervisorAgent is the to-do's supervisor Agent: its task leader's active row. A to-do has at most one
// (linkPlanAgent); false for every other to-do. The rules of a supervised to-do are taskleader's.
func (t Todo) supervisorAgent() (TodoAgent, bool) {
	for _, a := range t.Agents {
		if a.active() && a.Source == sourceLeader {
			return a, true
		}
	}
	return TodoAgent{}, false
}

// memberSupervised: t's own Agents do its work while its supervisor Agent supervises them directly (the hub attached it
// to a request under way): the ledger's supervisor is that Agent. The Agent is no worker of t.
func (t Todo) memberSupervised() bool {
	lead, ok := t.supervisorAgent()
	return ok && t.Supervision != nil && t.Supervision.Supervisor == leaderRef(lead.MachineID, lead.SessionID)
}

// members are t's Agent rows as the rules of membership read them; workingMembers those that work on t.
func (t Todo) members() []membership.Member {
	out := make([]membership.Member, len(t.Agents))
	for i, a := range t.Agents {
		out[i] = membership.Member{Machine: a.MachineID, Session: a.SessionID, Source: a.Source, LinkedAt: a.LinkedAt, Active: a.active(), Assignment: a.Assignment}
	}
	return out
}

func (t Todo) workingMembers() []membership.Member { return membership.Working(t.members()) }

// overMembers: t has a supervisor Agent and Agents of its own that do its work, whether or not its ledger was moved to the
// supervisor yet (the Agent is linked a moment before). Its turns are then not the work of t, from its first one.
func (t Todo) overMembers() bool {
	_, ok := t.supervisorAgent()
	return ok && (t.memberSupervised() || len(t.workingMembers()) > 0)
}

// supervisorRow: a is the supervisor Agent of t, whose own Agents work on it, not an Agent that works on it.
func (t Todo) supervisorRow(a TodoAgent) bool {
	return a.Source == sourceLeader && t.overMembers()
}

// supervisorTurn: machine/sid is the supervisor Agent of such a t: its turns are not the work of t.
func (t Todo) supervisorTurn(machine, sid string) bool {
	lead, ok := t.supervisorAgent()
	return ok && lead.MachineID == machine && lead.SessionID == sid && t.overMembers()
}

// conversationAbove is the conversation a decision of supervisor Agent's to-do t goes to: the one that supervised t
// before the Agent took it over, else t's ledger supervisor, else (a to-do with no ledger) its origin, else the main
// conversation.
func (s *Service) conversationAbove(ctx context.Context, t Todo) string {
	above, supervisor, origin := "", "", t.Origin // a ledger names its own supervisor: the origin stands in for a to-do with none
	if v := t.Supervision; v != nil {
		above, supervisor, origin = v.Above, v.Supervisor, ""
	}
	return autosupervise.Above(above, supervisor, origin, s.settings(ctx).Main, s.isLeaderRef)
}

// linkSource is the todo_agents source and via the agent of plan task t is linked with.
func (t PlanTask) linkSource() (source, via string) {
	switch {
	case t.Role == roleLeader:
		return sourceLeader, ""
	case t.Parent != "":
		return sourceWorker, t.Parent
	}
	return "manual", ""
}

// linkPlanAgent links the agent just started for plan task t to its todo.
func (s *Service) linkPlanAgent(ctx context.Context, t PlanTask, mid, sid string, allowDone bool) error {
	source, via := t.linkSource()
	err := s.linkTodoAgentWithSourceAllowingDone(ctx, t.TodoID, mid, sid, t.Title, t.Cwd, t.startedAgent, "", source, via, allowDone)
	if err == nil && !allowDone {
		s.estimateAdmitted(ctx, t, mid, sid)
	}
	return err
}

// leaderTier is the Agent preset a to-do's supervisor starts on when nothing else decides it (leaderDefaults): the
// light everyday preset, where the owner's model profile has one.
const leaderTier = "simple"

// leaderDefaults fills in the start of a to-do's supervisor where the main conversation's task_plan left it open
// (role "leader"): the assistant's own computer and its home directory, as it works on no project; the light preset
// when the active model profile has one and neither the call nor a start-defaults rule decides a leader's start; and
// always bypassPermissions without the read-only constraint. A read-only (plan) mode asks the owner before tool calls:
// on agy every Persona tool call would wait on a card (integrations/antigravity handlePermissionRequest), so a
// supervisor there could not supervise. It changes no files by its own rules (prompts/agents/task_leader.md).
func (s *Service) leaderDefaults(ctx context.Context, t *PlanTask) {
	cfg := s.settings(ctx)
	if strings.TrimSpace(t.Machine) == "" && cfg.MachineID != "" {
		t.Machine = cfg.MachineID
	}
	if strings.TrimSpace(t.Cwd) == "" {
		t.Cwd = "~"
		if m, ok := s.machineByName(ctx, t.Machine); ok && m.Home != "" {
			t.Cwd = m.Home
		}
	}
	t.Readonly, t.Permission = false, "bypassPermissions"
	if t.Tier != "" || t.Launcher != "" || t.Model != "" {
		return
	}
	if d, ruled, err := s.startDefaults(ctx, StartFacts{Machine: t.Machine, Role: roleLeader, Supervisor: "main",
		DoneWhen: len(t.DoneWhen)}); err != nil || ruled && len(d.Presets) > 0 {
		return // the owner's rule decides it at the start
	}
	s.models.mu.Lock()
	p, _, err := s.activeModelProfile(ctx)
	s.models.mu.Unlock()
	if err == nil && p != nil {
		if _, ok := p.Tiers[leaderTier]; ok {
			t.Tier = leaderTier
		}
	}
}

// supervisedWorkRefusal refuses a task of the main conversation's plan that would run work of a to-do its
// supervisor Agent runs: on that to-do itself (but a new supervisor for it, when its own has stopped), or on a piece of
// it. That work goes to the supervisor, which hands it out itself. With the setting auto a new piece of such a request
// is allowed: it is the supervisor's worker from its creation.
func (s *Service) supervisedWorkRefusal(ctx context.Context, t PlanTask) error {
	check := func(id string, replace bool) error {
		todo, ok := s.todoNow(id)
		if id == "" || !ok || todo.DoneAt != nil {
			return nil
		}
		lead, has := todo.supervisorAgent()
		if !has || replace && t.Role == roleLeader {
			return nil
		}
		return fmt.Errorf("to-do %s (%q) is run by its supervisor Agent %s on %s: send it new work or a new direction with "+
			"agent_send (no task; a closed supervisor is resumed by the message); it hands out the to-do's pieces itself",
			todo.ID, todo.Title, short(lead.SessionID), lead.MachineID)
	}
	if err := check(t.ReuseTodo, true); err != nil {
		return err
	}
	if p, ok := s.todoNow(t.ReuseTodo); ok && p.PartOf != "" {
		if err := check(p.PartOf, false); err != nil {
			return err
		}
	}
	if autosupervise.On(s.settings(ctx).TodoSupervisor) && t.ReuseTodo == "" {
		return nil // a piece of a request its supervisor Agent runs is that supervisor's worker (planTaskSupervisor)
	}
	return check(t.PartOf, false)
}

// attachWorker puts agent c of machine am, which works on no open task, under the supervisor Agent of to-do
// task.TodoID as a new worker (agent_send with task.todo_id): a to-do of its own beneath that to-do, supervised by the
// supervisor, with task's criteria; text is what it is told, its first instruction. Only the conversation that
// supervises that to-do, or the main conversation, attaches; the supervisor's bound on open workers holds.
func (s *Service) attachWorker(ctx context.Context, self string, am Device, c proto.Session, task sendTask, text string) (*handedTask, error) {
	root, ok := s.todoNow(strings.TrimSpace(task.TodoID))
	if !ok || root.DoneAt != nil {
		return nil, fmt.Errorf("no open to-do %q; nothing was sent", task.TodoID)
	}
	lead, has := root.supervisorAgent()
	if !has { // the to-do's Agents work on it themselves: this one joins them as a member (membership)
		return s.attachMember(ctx, self, am, c, task)
	}
	if root.Supervision == nil || root.Supervision.Supervisor != self && !s.isMain(ctx, self) {
		return nil, fmt.Errorf("to-do %s is not supervised from this conversation; nothing was sent", root.ID)
	}
	if t, busy := s.openTodoOfAgent(am.ID, c.SessionID); busy {
		return nil, fmt.Errorf("%s works for task %s (%q) already: only an Agent on no open task joins a to-do as a worker; nothing was sent",
			agentLabel(c), t.ID, t.Title)
	}
	if len(cleanDoneWhen(task.DoneWhen)) == 0 {
		return nil, errors.New("a worker needs done_when (results its supervisor can check); nothing was sent")
	}
	if err := s.checkLeaderRoom(ctx, &leaderScope{task: root, agent: lead}, 1); err != nil {
		return nil, fmt.Errorf("to-do %s: %w", root.ID, err)
	}
	ref := leaderRef(lead.MachineID, lead.SessionID)
	w, _, err := s.adoptAgentFor(ctx, ref, ref, am, c, task.Title, text, text, task.DoneWhen, task.ExpectMinutes, adoptionOptions{via: root.ID})
	if err != nil {
		return nil, fmt.Errorf("%w; nothing was sent", err)
	}
	return &handedTask{todo: w, made: true, self: self, supervisor: ref, attached: root,
		run: SessionRef{MachineID: am.ID, SessionID: c.SessionID}}, nil
}

// leadersPiece: conversation is a task leader's ref and to-do t a piece of that leader's own to-do that nobody
// supervises yet, which the leader may hand out with task_plan's todo_id (taskTodoForConversation).
func (s *Service) leadersPiece(conversation string, t Todo) bool {
	machine, session, ok := parseLeaderRef(conversation)
	if !ok || t.PartOf == "" || t.Supervision.assigned() {
		return false
	}
	root, found := s.todoNow(t.PartOf)
	if !found || root.DoneAt != nil {
		return false
	}
	lead, has := root.supervisorAgent()
	return has && s.canonical(lead.MachineID) == s.canonical(machine) && lead.SessionID == session
}

// leaderTask is the open task agent machine/session leads, with the agent's own row; false for any other agent.
func (s *Service) leaderTask(ctx context.Context, machine, session string) (Todo, TodoAgent, bool) {
	if session == "" {
		return Todo{}, TodoAgent{}, false
	}
	machine = s.canonical(machine)
	t, ok := s.taskOf(ctx, machine, session)
	if !ok || t.Cancelled() {
		return Todo{}, TodoAgent{}, false
	}
	for _, a := range t.Agents {
		if a.MachineID == machine && a.SessionID == session && a.active() && a.Source == sourceLeader {
			return t, a, true
		}
	}
	return Todo{}, TodoAgent{}, false
}

// taskOfAny is taskOf plus the tasks whose agent is released or dismissed: a leader whose agent was replaced still has
// its own task and its supervisor above it, so what could not be delivered can be escalated there.
func (s *Service) taskOfAny(ctx context.Context, mid, sid string) (Todo, bool) {
	if t, ok := s.taskOf(ctx, mid, sid); ok {
		return t, true
	}
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return Todo{}, false
	}
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	for _, t := range s.todos.todos {
		if t.Origin == "" || t.DoneAt != nil {
			continue
		}
		for _, a := range t.Agents {
			if a.MachineID == mid && a.SessionID == sid {
				return copyTodoLocked(t), true
			}
		}
	}
	return Todo{}, false
}

// isLeaderSession: machine/session is a task leader's agent, whatever the state of its row. Both are needed: a session id
// alone is not unique across computers.
func (s *Service) isLeaderSession(machine, sid string) bool {
	if sid == "" {
		return false
	}
	machine = s.canonical(machine)
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	for _, t := range s.todos.todos {
		for _, a := range t.Agents {
			if a.MachineID == machine && a.SessionID == sid && a.Source == sourceLeader {
				return true
			}
		}
	}
	return false
}

// agentSupervised: task t's supervisor is a task leader's agent, not a conversation.
func (s *Service) agentSupervised(t Todo) bool {
	if t.Supervision == nil || t.Supervision.Supervisor == "" {
		return false
	}
	return s.isLeaderRef(t.Supervision.Supervisor)
}

// leaderRef names a task leader's agent as a worker's ledger supervisor and to-do origin: machine and session together,
// since a session id alone is not unique across computers. It is no conversation id (conversations have none with a "/").
func leaderRef(machine, session string) string { return machine + "/" + session }

// parseLeaderRef is the machine and session a leaderRef names.
func parseLeaderRef(ref string) (machine, session string, ok bool) {
	machine, session, ok = strings.Cut(ref, "/")
	return machine, session, ok && machine != "" && session != ""
}

// isLeaderRef: ref names a task leader's agent.
func (s *Service) isLeaderRef(ref string) bool {
	m, sid, ok := parseLeaderRef(ref)
	return ok && s.isLeaderSession(m, sid)
}

// leaderSupervisor is the leader agent that supervises task t now: the session its ledger names while that is the
// agent of an open leader task, else the current agent of the leader task its worker's row says it belongs to (a leader
// started again has a new session). supervised says t's supervisor is a leader agent at all; ok that it can be reached.
func (s *Service) leaderSupervisor(t Todo) (lead TodoAgent, supervised, ok bool) {
	if !s.agentSupervised(t) {
		return TodoAgent{}, false, false
	}
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	open := func(l *Todo) bool { return l != nil && l.DoneAt == nil && !l.Cancelled() }
	machine, supSession, _ := parseLeaderRef(t.Supervision.Supervisor)
	machine = s.canonical(machine)
	// anyMachine: the fallback by the leader task's link takes its current agent wherever it runs now (a flexible leader
	// started again on another computer); the ledger's own session is matched on its computer.
	pick := func(l *Todo, sid string, anyMachine bool) {
		for _, a := range l.Agents {
			if (anyMachine || a.MachineID == machine) && a.active() && a.Source == sourceLeader && (sid == "" || a.SessionID == sid) && (!ok || a.LinkedAt.After(lead.LinkedAt)) {
				lead, ok = a, true
			}
		}
	}
	for _, l := range s.todos.todos {
		if open(l) {
			pick(l, supSession, false)
		}
	}
	if ok {
		return lead, true, true
	}
	for _, a := range t.Agents {
		if a.Source == sourceWorker && a.Via != "" && open(s.todos.todos[a.Via]) {
			pick(s.todos.todos[a.Via], "", true)
		}
	}
	return lead, true, ok
}

// leaderToolFiles are a task leader's own tools, the to-do supervisor's, under the names the model calls, each with
// its description file (prompts/mcp/leader): task_plan and task_review reach the workers it hands out beneath its
// to-do; agent_read, agent_send and task_scope reach those workers only (and task_scope its own to-do's time). With
// the Agents' own tools (report_to_supervisor up to whoever handed it its to-do) they are all it lists: no A2A, no
// task_spawn (sessionMCPToolsChecked), no machine_exec (an Agent never has it).
var leaderToolFiles = [][2]string{
	{"task_plan", "leader_task_plan"}, {"task_review", "leader_task_review"}, {"agent_read", "leader_agent_read"},
	{"agent_send", "leader_agent_send"}, {"task_scope", "leader_task_scope"},
}

// leaderToolList are a task leader's own tools (leaderToolFiles).
func leaderToolList() []proto.Tool {
	out := make([]proto.Tool, 0, len(leaderToolFiles))
	for _, n := range leaderToolFiles {
		t := prompts.Tool(n[1])
		out = append(out, proto.Tool{Name: n[0], Description: t.Description, InputSchema: t.InputSchema})
	}
	return out
}

// isLeaderTool: name is one of a task leader's own tools.
func isLeaderTool(name string) bool {
	return slices.ContainsFunc(leaderToolFiles, func(n [2]string) bool { return n[0] == name })
}

// runtimeLeaderTools supplies plugin registration schemas and checks individual sessions at tool activation.
func (s *Service) runtimeLeaderTools(ctx context.Context, machine, session string, guestHint bool) []proto.Tool {
	if s.isWorkMachine(machine) || guestHint {
		return nil
	}
	if session != "" {
		if _, guest, err := s.guestOf(ctx, machine, session); err != nil || guest {
			return nil
		}
		if _, _, ok := s.leaderTask(ctx, machine, session); !ok {
			return nil
		}
	}
	return localizedMCPTools(leaderToolList(), s.toolLanguage(ctx))
}

// leaderTools are the tools only a task leader's agent gets, on top of any agent's: none for any other agent.
func (s *Service) leaderTools(ctx context.Context, machine, session string) []proto.Tool {
	if session == "" {
		return nil
	}
	if _, _, ok := s.leaderTask(ctx, machine, session); ok {
		return localizedMCPTools(leaderToolList(), s.toolLanguage(ctx))
	}
	if revision, _ := ctx.Value(revisionDiscoveryKey{}).(bool); revision {
		return nil // a revision-aware peer recovers through its cursor, never through the legacy startup wait
	}
	// A legacy one-shot peer asks before the start returns and has no cursor with which to recover later. Wait only
	// while a leader is actually starting, and only until that leader's real link grants the tools.
	if _, ok := s.taskOf(ctx, s.canonical(machine), session); ok {
		return nil
	}
	for deadline := time.Now().Add(leaderListWait); s.leaderStarts.Load() > 0 && time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(100 * time.Millisecond):
		}
		if _, _, ok := s.leaderTask(ctx, machine, session); ok {
			return localizedMCPTools(leaderToolList(), s.toolLanguage(ctx))
		}
	}
	return nil
}

// leaderTaskPlan is task_plan called by agent machine/session through its MCP server: it runs only for the leader of
// an open task, and only for workers beneath that task. Any other agent is refused as if the tool did not exist; a
// task's other agents are told why.
func (s *Service) leaderTaskPlan(ctx context.Context, machine, session string, args json.RawMessage) proto.ToolResult {
	lead, agent, ok := s.leaderTask(ctx, machine, session)
	if !ok {
		if err := s.workerRefusal(ctx, machine, session); err != nil {
			return proto.TextResult(err.Error(), true)
		}
		return proto.TextResult("unknown tool task_plan", true)
	}
	res, err := s.taskPlanFrom(ctx, agent.MachineID, leaderRef(agent.MachineID, session), args, &leaderScope{task: lead, agent: agent})
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return proto.TextResult(res, false)
}

// leaderScope is what a task leader's task_plan is bound to: its open task and its own agent (the default place).
type leaderScope struct {
	task  Todo
	agent TodoAgent
}

// leaderTaskIn is a worker task as a task leader may plan it: nothing else (attachments, project, user_requested…) is
// read, an unknown field is refused. Every new worker declares role "worker". todo_id names a piece of the leader's own
// to-do (part_of it) that nobody supervises yet, to run on that piece's to-do (leadersPiece); any other is refused.
type leaderTaskIn struct {
	TodoID        string                `json:"todo_id"`
	Join          bool                  `json:"join"`
	Title         string                `json:"title"`
	Prompt        string                `json:"prompt"`
	Role          string                `json:"role"`
	Machine       string                `json:"machine"`
	MachinePolicy string                `json:"machine_policy"`
	Cwd           string                `json:"cwd"`
	Launcher      string                `json:"launcher"`
	Model         string                `json:"model"`
	Tier          string                `json:"tier"`
	Effort        string                `json:"effort"`
	Speed         string                `json:"speed"`
	Permission    string                `json:"permission"`
	Readonly      bool                  `json:"readonly"`
	DoneWhen      []string              `json:"done_when"`
	Checks        []acceptance.Declared `json:"checks"`
	Goal          string                `json:"goal"`
	ExpectMinutes int                   `json:"expect_minutes"`
	Why           string                `json:"why"`
}

// workerTasks reads a leader's task_plan arguments as worker tasks beneath its task.
func (l *leaderScope) workerTasks(args json.RawMessage) ([]PlanTask, error) {
	var in struct {
		Tasks []leaderTaskIn `json:"tasks"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("a task leader's task_plan takes only the fields its schema lists; attachments and any other "+
			"are refused, and workers can only be new tasks beneath your own task or its pieces (%v)", err)
	}
	out := make([]PlanTask, 0, len(in.Tasks))
	for _, t := range in.Tasks {
		pt := PlanTask{Title: t.Title, Prompt: t.Prompt, Role: t.Role, Machine: t.Machine, MachinePolicy: t.MachinePolicy, Cwd: t.Cwd,
			Launcher: t.Launcher, Model: t.Model, Tier: t.Tier, Effort: t.Effort, Speed: t.Speed, Permission: t.Permission, Readonly: t.Readonly,
			DoneWhen: t.DoneWhen, Checks: t.Checks, Goal: t.Goal, ExpectMinutes: t.ExpectMinutes, Why: t.Why, Parent: l.task.ID, Project: l.task.Project,
			ReuseTodo: strings.TrimSpace(t.TodoID), join: t.Join}
		if strings.TrimSpace(pt.Machine) == "" {
			pt.Machine = l.agent.MachineID
			if strings.TrimSpace(pt.Cwd) == "" {
				pt.Cwd = l.agent.Cwd
			}
		}
		out = append(out, pt)
	}
	return out, nil
}

// checkLeaderRoom refuses a plan that would leave the leader's task with more than leaderWorkersMost open workers. The
// workers are counted by the task they belong to (the supervisor ref or a worker row via the leader's task), so a leader
// started again does not get a new allowance.
func (s *Service) checkLeaderRoom(ctx context.Context, scope *leaderScope, adding int) error {
	ref := leaderRef(scope.agent.MachineID, scope.agent.SessionID)
	_ = s.ensureTodosLoaded(ctx)
	var pieces []int // the working members of each open worker's to-do
	s.todos.mu.RLock()
	for _, t := range s.todos.todos {
		if t.DoneAt != nil || t.ID == scope.task.ID || t.Supervision == nil && !slices.ContainsFunc(t.Agents, func(a TodoAgent) bool { return a.Source == sourceWorker }) {
			continue
		}
		belongs := t.Supervision != nil && t.Supervision.Supervisor == ref
		for _, a := range t.Agents {
			belongs = belongs || a.Source == sourceWorker && a.Via == scope.task.ID && a.active()
		}
		if belongs {
			pieces = append(pieces, len(t.workingMembers()))
		}
	}
	s.todos.mu.RUnlock()
	own := 0
	if scope.task.memberSupervised() { // its own Agents are members the supervisor supervises too
		own = len(scope.task.workingMembers())
	}
	open := membership.Load(pieces, own) // a member counts as a worker did: at most leaderWorkersMost at a time
	if open+adding > leaderWorkersMost {
		return fmt.Errorf("at most %d workers of your task are open at a time (%d now): wait for some to finish; nothing was planned",
			leaderWorkersMost, open)
	}
	return nil
}

// ---- supervision messages ----

// leaderAbove is the task whose agent supervises worker task t, with that agent's session (the ledger's), ok when t is
// leader-supervised at all. It works for a released or dismissed leader too: what could not be delivered still has a
// conversation above it to go to.
func (s *Service) leaderAbove(t Todo) (lead Todo, session string, ok bool) {
	if !s.agentSupervised(t) {
		return Todo{}, "", false
	}
	machine, sid, _ := parseLeaderRef(t.Supervision.Supervisor)
	machine = s.canonical(machine)
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	for _, l := range s.todos.todos {
		for _, a := range l.Agents {
			if a.MachineID == machine && a.SessionID == sid && a.Source == sourceLeader {
				return copyTodoLocked(l), sid, true
			}
		}
	}
	return Todo{}, sid, true
}

// supervisorAboveTask is the conversation above worker task t's leader: the conversation that supervises the leader's own
// task, else the main conversation; "" with neither. It does not need the leader's task to be open: a closed leader's
// workers go to this conversation (rehomeLeaderlessWorkers).
func (s *Service) supervisorAboveTask(ctx context.Context, t Todo) (up, leaderSession string) {
	lead, sid, ok := s.leaderAbove(t)
	if !ok {
		return "", ""
	}
	return s.conversationAbove(ctx, lead), sid
}

// escalateLeaderSignal is signal g about a worker whose leader agent is unreachable, as a decision for the conversation
// above that leader: it names the worker (so that conversation can review it directly) and says the leader could not be
// reached, with what the signal said. Todo "" when there is no conversation above it, in which case the caller leaves
// the signal for another try.
func (s *Service) escalateLeaderSignal(ctx context.Context, g superviseSignal) superviseSignal {
	return s.escalateLeaderSignalAs(ctx, g, false)
}

// escalateLeaderSignalAs is escalateLeaderSignal; uncertain says the leader's computer took the message but its session
// was not seen taking it (held for approval there, dropped, typed part way), rather than that the leader was not reached.
func (s *Service) escalateLeaderSignalAs(ctx context.Context, g superviseSignal, uncertain bool) superviseSignal {
	t, ok := s.todoNow(g.Todo)
	if !ok {
		return superviseSignal{}
	}
	up, sid := s.supervisorAboveTask(ctx, t)
	if lead, own := t.supervisorAgent(); up == "" && own && t.Supervision != nil && !s.isLeaderRef(t.Supervision.Supervisor) {
		up, sid = t.Supervision.Supervisor, lead.SessionID // the supervisor's own to-do: its own supervisor decides
	}
	if up == "" {
		s.logf("supervise: %s of task %s: its task leader %s is unreachable and nothing is above it; trying again later", g.Kind, g.Todo, short(sid))
		return superviseSignal{}
	}
	options := "Resume it with agent_send (a message resumes a closed Agent), take its workers over with task_review on them, " +
		"or cancel the to-do with task_review."
	why, key := fmt.Sprintf("Its supervisor Agent %s cannot be reached, so what it was to handle could not be delivered (%s: %s). %s",
		short(sid), g.Kind, clip(oneLine(g.Detail), 300), options), "leader-unreachable@"
	if uncertain {
		why, key = fmt.Sprintf("Its supervisor Agent %s may not have received what it was to handle: its computer took the message, but "+
			"the session was not seen taking it (it may wait for approval there, or have been dropped) (%s: %s). %s",
			short(sid), g.Kind, clip(oneLine(g.Detail), 300), options), "leader-unconfirmed@"
	}
	return superviseSignal{Todo: g.Todo, Kind: sigDecide, To: up, Key: key + g.Todo + "@" + g.Kind + "@" + g.Key,
		Detail: why, At: g.At, Unreachable: true}
}

// supervisorRoute is where signal g about a to-do run by a supervisor Agent goes (taskleader.Route: the to-do's own
// signals; its workers' go to the supervisor through leaderRoute), with that supervisor. ok false for a to-do with no
// supervisor. A piece of such a to-do that no Agent works on yet is its supervisor's to start: the piece's readiness
// goes to the supervisor too. A signal's explicit target (g.To) is decided by Route as well: a card's rung and a
// decision sent up keep theirs (AsBefore, ToMain); readiness and closeout wakes go to the supervisor.
func (s *Service) supervisorRoute(g superviseSignal) (lead TodoAgent, dest taskleader.Destination, ok bool) {
	t, found := s.todoNow(g.Todo)
	if !found || t.DoneAt != nil {
		return TodoAgent{}, "", false
	}
	if lead, ok = t.supervisorAgent(); ok {
		subject := taskleader.SubjectRoot
		if t.memberSupervised() && !(g.Kind == sigReport && g.Session == lead.SessionID) { // its own report goes up as a root's does
			subject = taskleader.SubjectMember
		}
		return lead, taskleader.Route(g.Kind, g.Report, subject), true
	}
	if g.Kind == sigTodoReady && t.PartOf != "" && (t.Supervision == nil || !s.agentSupervised(t)) {
		if root, found := s.todoNow(t.PartOf); found && root.DoneAt == nil {
			if lead, ok = root.supervisorAgent(); ok {
				return lead, taskleader.ToSupervisor, true
			}
		}
	}
	return TodoAgent{}, "", false
}

// leaderRoute says where signal g of a task goes when its supervisor is a leader agent: handled false for any other
// task (digestTo decides), else the leader agent it goes to, reachable false when there is none now (the signal is
// dropped: it never falls to the main conversation). A signal that names its conversation itself (g.To: a card's rung,
// a decision sent up) is not one of these.
func (s *Service) leaderRoute(g superviseSignal) (lead TodoAgent, handled, reachable bool) {
	if g.To != "" && (g.Kind != sigAsk || !s.isLeaderRef(g.To)) {
		return TodoAgent{}, false, false
	}
	t, ok := s.todoNow(g.Todo)
	if !ok || t.memberSupervised() { // what the supervisor does not judge goes to the conversation above (digestTo)
		return TodoAgent{}, false, false
	}
	lead, handled, reachable = s.leaderSupervisor(t)
	if g.To != "" && reachable && g.To != leaderRef(lead.MachineID, lead.SessionID) {
		return TodoAgent{}, false, false
	}
	return lead, handled, reachable
}

// deliverToLeaderAgent puts the signals list of its workers into leader agent lead's session as one supervision message
// (agents/worker_supervise), deduplicated as a conversation's are; now is when sendDigest marked it as the last message
// and prev the last before. True when it went in. session.send queues behind a running turn and resumes a closed agent,
// so nothing is queued here. An offline resume completes in the background.
func (s *Service) deliverToLeaderAgent(ctx context.Context, lead TodoAgent, list []superviseSignal, web string, now, prev time.Time) bool {
	var entries []prompts.SuperviseEntry
	var kept, moot, retry []superviseSignal
	own := "" // the leader's own to-do: its signals come with its workers' (supervisorRoute)
	if lt, ok := s.leaderTaskOf(ctx, lead); ok && !lt.memberSupervised() {
		own = lt.ID
	}
	// An earlier offline resume owns its in-flight signals until its real delivery callback. Keep them out of this
	// batch's acknowledgements as well as its text: a second digest may carry new signals, but must not ack the first
	// batch before that first batch reaches the leader (#5732, #9701). dedupMu is held throughout this selection.
	batch := make([]superviseSignal, 0, len(list))
	for _, g := range list {
		e, ok, deferRetry := s.digestEntryChecked(ctx, g, web)
		e.Own = ok && own != "" && g.Todo == own
		if deferRetry {
			retry = append(retry, g)
			continue
		}
		if !ok {
			moot = append(moot, g)
			batch = append(batch, g)
			continue
		}
		key, hash := digestFingerprint(e, g)
		if _, resuming := s.supervise.resuming[[2]string{key, hash}]; resuming {
			continue
		}
		entries, kept = append(entries, e), append(kept, g)
		batch = append(batch, g)
	}
	if len(retry) > 0 {
		at := now
		if len(entries) > 0 {
			at = time.Time{}
		}
		s.restoreDigest(retry, at, prev)
	}
	s.clearSentTodoReady(moot)
	s.clearSentPRMerged(moot)
	s.clearSentParentCloseout(moot)
	s.turnsTold(ctx, moot) // what no longer holds is not told after a restart either
	if len(entries) == 0 {
		s.ackSupervisePending(batch)
		return false
	}
	holds := kept
	session, listed := s.session(ctx, lead.MachineID, lead.SessionID)
	if !listed {
		s.logf("supervise: task leader %s is not listed; trying again", short(lead.SessionID))
		s.restoreDigest(kept, now, prev)
		return false
	}
	delivery := leaderDelivery
	if session.Status == proto.StatusOffline {
		delivery = leaderResumeDelivery
	}
	entries, kept, rollback, err := s.reserveSupervisionDelivery(ctx, entries, kept, now, delivery)
	if err != nil {
		s.logf("supervise: deduplication unavailable; retrying: %v", err)
		s.restoreDigest(kept, now, prev)
		return false
	}
	if len(entries) == 0 {
		s.ackSupervisePending(batch)
		s.todoReadyTold(ctx, holds)
		s.prMergeWakeTold(ctx, holds)
		s.parentCloseoutTold(ctx, holds)
		s.turnsTold(ctx, holds) // already covered by the deduplication history
		return false
	}
	delivered := func(ctx context.Context) {
		s.supervise.dedupMu.Lock()
		defer s.supervise.dedupMu.Unlock()
		s.leaderDelivered(ctx, lead, entries, batch, kept, holds)
	}
	// Sort a copy for rendering so the retry still pairs each fingerprint with its original signal.
	text := digestfmt.Render(digestfmt.Input{Role: prompts.SuperviseForLeader, Entries: entries}).Text // whole: a supervisor Agent's compaction is not seen here
	if delivery == leaderResumeDelivery {
		s.resumeLeaderForSupervision(lead, text, batch, kept, holds, func(ctx context.Context, err error) {
			s.supervise.dedupMu.Lock()
			defer s.supervise.dedupMu.Unlock()
			rollback()
			if err == nil {
				s.leaderDelivered(ctx, lead, entries, batch, kept, holds)
			}
		}, delivered)
		return false
	}
	if err := s.sendToLeader(ctx, lead, text, batch, kept, holds, delivered); err != nil {
		var escalated leaderEscalated
		if errors.As(err, &escalated) {
			// The current process's escalation/retry owns this message. Do not restore its pending entry and escalate it
			// twice, but release dedup and leave the turn untold so a manager restarted without either can deliver it.
			rollback()
			s.logf("supervise: %d signal(s) for task leader %s escalated (stopped, or not seen taking them): %v; keeping them recoverable", len(entries), short(lead.SessionID), err)
			return false
		}
		s.logf("supervise: %d signal(s) for task leader %s: %v; trying again", len(entries), short(lead.SessionID), err)
		rollback()
		s.restoreDigest(kept, now, prev)
		return false
	}
	s.logf("supervise: %d signal(s) to task leader %s", len(entries), short(lead.SessionID))
	s.ackSupervisePending(batch)
	s.parkedDueTold(ctx, kept)
	s.estimateDueTold(ctx, kept)
	s.todoReadyTold(ctx, holds)
	s.prMergeWakeTold(ctx, holds)
	s.parentCloseoutTold(ctx, holds)
	s.turnsTold(ctx, holds) // kept went, the rest was said the same within the deduplication window
	return true
}

// leaderUnreachable is a send to the leader's session that failed: the message did not reach it, so it must be retried
// or escalated, never lost.
type leaderUnreachable struct{ err error }

func (e leaderUnreachable) Error() string { return e.err.Error() }
func (e leaderUnreachable) Unwrap() error { return e.err }

// leaderEscalated is a refused or unconfirmed send (#9607) whose escalation and process-local retry are already running. It is not delivery:
// the original turn stays untold and its dedup reservation is released so a restarted manager can recover it.
type leaderEscalated struct{ err error }

func (e leaderEscalated) Error() string { return e.err.Error() }
func (e leaderEscalated) Unwrap() error { return e.err }

// sendToLeader sends text, the supervision message for signals sent, into the leader agent's session. It states no
// permission-mode class without evidence captured by the source node when the words were written; a digest with missing
// or mixed classes states none, and queued retries reuse that immutable evidence. A stopped or unconfirmed send escalates
// and retries (escalateLeaderMessage); nodeMayHaveTaken marks that escalation unconfirmed rather than unreachable (#9607).
// Other failures are retried by the next digest. After real delivery, delivered re-records dedup and acknowledges this
// batch, including members suppressed by delivered dedup history (#5732). sent determines the text actually carried;
// receipts includes the filtered members whose turn receipts close on delivery.
func (s *Service) sendToLeader(ctx context.Context, lead TodoAgent, text string, all, sent, receipts []superviseSignal, delivered func(context.Context)) error {
	ctx = supervisionSourceContext(ctx, sent)
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()
	if err := s.call(sctx, lead.MachineID, DeviceSessionSend, sendParams(lead.SessionID, text, false), nil); err != nil {
		if refusedWith(err, proto.CodeAgentStopped) {
			s.logf("task leader %s: it has stopped: %v", short(lead.SessionID), err)
			s.escalateLeaderMessage(lead, all, sent, receipts, text, false, delivered)
			return leaderEscalated{err}
		}
		if nodeMayHaveTaken(err) {
			s.logf("task leader %s: the message may have reached it, not confirmed: %v", short(lead.SessionID), err)
			s.escalateLeaderMessage(lead, all, sent, receipts, text, true, delivered)
			return leaderEscalated{err}
		}
		return leaderUnreachable{err}
	}
	return nil
}

// resumeLeaderForSupervision lets the digest continue while the offline session resumes. Complete the first attempt
// before handing a failure to the retry loop. Only confirmed delivery acknowledges the batch and its filtered receipts.
func (s *Service) resumeLeaderForSupervision(lead TodoAgent, text string, all, sent, receipts []superviseSignal, completed func(context.Context, error), delivered func(context.Context)) {
	go func() {
		ctx := context.Background()
		err := s.sendToLeaderNow(ctx, lead, text, sent)
		completed(ctx, err)
		if err != nil {
			s.logf("task leader %s: resume: %v", short(lead.SessionID), err)
			s.escalateLeaderMessage(lead, all, sent, receipts, text, nodeMayHaveTaken(err), delivered)
		}
	}()
}

// leaderDelivered records a confirmed background send or retry under dedupMu. It re-records the actual entries/signals
// pairing, then acknowledges only this batch; an older in-flight resume still owns its own excluded signals.
func (s *Service) leaderDelivered(ctx context.Context, lead TodoAgent, entries []prompts.SuperviseEntry, all, sent, receipts []superviseSignal) {
	if _, _, _, err := s.reserveSupervision(ctx, entries, sent, time.Now()); err != nil {
		s.logf("supervise: task leader %s took the message; deduplication not recorded: %v", short(lead.SessionID), err)
	}
	s.ackSupervisePending(all)
	s.parkedDueTold(ctx, sent)
	s.estimateDueTold(ctx, sent)
	s.todoReadyTold(ctx, receipts)
	s.prMergeWakeTold(ctx, receipts)
	s.parentCloseoutTold(ctx, receipts)
	s.turnsTold(ctx, receipts)
	s.logf("supervise: %d signal(s) to task leader %s after confirmation", len(sent), short(lead.SessionID))
}

// sendToLeaderNow is one attempt to put text into the leader's session, resuming it if closed; it waits up to
// leaderResumeTime. Each attempt has its own time, so it can be called from a goroutine that outlives any request.
func (s *Service) sendToLeaderNow(ctx context.Context, lead TodoAgent, text string, kept []superviseSignal) error {
	ctx = supervisionSourceContext(ctx, kept)
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.leaderResumeTime())
	defer cancel()
	return s.call(sctx, lead.MachineID, DeviceSessionSend, sendParams(lead.SessionID, text, false), nil)
}

// escalateLeaderMessage handles supervision message text for signals sent that could not be put into the leader's
// session (a closed agent's resume failed, or the node refused it because the agent has stopped), or that its session
// was not seen taking (uncertain). Only those actually rendered into the text become decisions for the conversation
// above the leader, naming the worker and saying whether the leader was unreachable or unconfirmed. In any case the
// message is retried on its own context until taken or the leader's task closes. Real delivery re-records dedup and
// acknowledges the complete original batch, including filtered members (#5732, #9607).
func (s *Service) escalateLeaderMessage(lead TodoAgent, all, sent, receipts []superviseSignal, text string, uncertain bool, delivered func(context.Context)) {
	ctx := context.Background()
	for _, g := range sent {
		if w, ok := s.todoNow(g.Todo); ok {
			if esc := s.escalateLeaderSignalAs(ctx, g, uncertain); esc.Todo != "" {
				s.signalReplacing(&g, esc)
			} else if up, _ := s.supervisorAboveTask(ctx, w); up == "" {
				s.logf("task leader %s: nobody above it to tell about %s; retrying the leader", short(lead.SessionID), g.Todo)
			}
		}
	}
	go func() {
		wait, deadline := s.leaderResumeTime(), time.Now().Add(24*time.Hour)
		for time.Now().Before(deadline) {
			time.Sleep(wait)
			if _, ok := s.taskOfAny(ctx, lead.MachineID, lead.SessionID); !ok {
				return // the leader's task is closed: its workers were rehomed, nobody is left to tell
			}
			s.logf("task leader %s: trying the undelivered supervision message again", short(lead.SessionID))
			if err := s.sendToLeaderNow(ctx, lead, text, sent); err == nil {
				if delivered != nil {
					delivered(ctx)
				} else {
					s.ackSupervisePending(all)
					s.parkedDueTold(ctx, sent)
					s.turnsTold(ctx, receipts)
				}
				return
			}
			wait = leaderRetryBackoff(wait)
		}
	}()
}

// ---- the leader's task_review ----

// leaderTaskReview is task_review called by agent machine/session through its MCP server: only for a task leader.
func (s *Service) leaderTaskReview(ctx context.Context, machine, session string, args json.RawMessage) proto.ToolResult {
	_, agent, ok := s.leaderTask(ctx, machine, session)
	if !ok {
		return proto.TextResult("unknown tool task_review", true)
	}
	res, err := s.leaderReview(ctx, agent, args)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return proto.TextResult(res, false)
}

// ---- the supervisor's agent_read, agent_send and task_scope ----

// leaderScopedTool is agent_read, agent_send or task_scope called by agent machine/session through its MCP server: a
// task leader's own versions (prompts/mcp/leader), which reach the workers it handed out and nothing else (and, for
// task_scope, the time its own to-do should take). Any other agent is refused as if the tool did not exist.
func (s *Service) leaderScopedTool(ctx context.Context, machine, session, name string, args json.RawMessage) proto.ToolResult {
	root, lead, ok := s.leaderTask(ctx, machine, session)
	if !ok {
		return proto.TextResult("unknown tool "+name, true)
	}
	var res string
	var err error
	switch name {
	case "agent_read":
		res, err = s.leaderAgentRead(ctx, lead, args)
	case "agent_send":
		res, err = s.leaderAgentSend(ctx, lead, args)
	case "task_scope":
		res, err = s.leaderTaskScope(ctx, root, lead, args)
	default:
		return proto.TextResult("unknown tool "+name, true)
	}
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return proto.TextResult(res, false)
}

// leaderWorker is to-do id when it is a worker of task leader lead (its ledger names lead's session now), else an
// error the model can act on.
func (s *Service) leaderWorker(lead TodoAgent, id string) (Todo, error) {
	t, ok := s.todoNow(id)
	if !ok {
		return Todo{}, fmt.Errorf("no task %q", id)
	}
	if cur, supervised, reachable := s.leaderSupervisor(t); !supervised || !reachable || cur.MachineID != lead.MachineID || cur.SessionID != lead.SessionID {
		return Todo{}, fmt.Errorf("task %s is not one of your workers: you reach the workers you handed out with task_plan, nothing else", t.ID)
	}
	return t, nil
}

// workerAgent is the agent of worker task t to read: its newest active one, else its newest.
func workerAgent(t Todo) (TodoAgent, bool) {
	var pick TodoAgent
	found := false
	for _, a := range t.Agents {
		if a.SessionID == "" || a.Source == sourceLeader { // never the supervisor's own session
			continue
		}
		better := !found || a.active() && !pick.active() || a.active() == pick.active() && a.LinkedAt.After(pick.LinkedAt)
		if better {
			pick, found = a, true
		}
	}
	return pick, found
}

// leaderAgentRead is the supervisor's agent_read: the recent exchanges of one of its workers' Agent.
func (s *Service) leaderAgentRead(ctx context.Context, lead TodoAgent, args json.RawMessage) (string, error) {
	a, err := taskleader.DecodeRead(args)
	if err != nil {
		return "", err
	}
	w, err := s.leaderWorker(lead, a.TodoID)
	if err != nil {
		return "", err
	}
	member, err := s.memberFor(w, a.SessionID)
	if err != nil {
		return "", err
	}
	agent, ok := workerAgent(onlyMember(w, member))
	if !ok {
		return "", fmt.Errorf("worker %s has no Agent yet: its start is told to you when it fails", w.ID)
	}
	m, ok := s.device(ctx, agent.MachineID)
	if !ok {
		return "", fmt.Errorf("worker %s's computer %s is not connected now", w.ID, agent.MachineID)
	}
	c, ok := s.session(ctx, m.ID, agent.SessionID)
	if !ok {
		return "", fmt.Errorf("worker %s's Agent %s is not listed on %s now", w.ID, short(agent.SessionID), m.Name)
	}
	return s.agentTranscript(ownerfence.WithObserver(ctx, s.ownerInputRef(lead.MachineID, lead.SessionID)), m, c, a.Turns)
}

// leaderAgentSend is the supervisor's agent_send: a message to one of its open workers' Agent (a nudge, an answer, a
// resume after a restart, the order to merge in), never new work. A closed Agent is resumed by it (sendToTask), and
// the message is kept on the worker's page.
func (s *Service) leaderAgentSend(ctx context.Context, lead TodoAgent, args json.RawMessage) (string, error) {
	a, err := taskleader.DecodeSend(args)
	if err != nil {
		return "", err
	}
	w, err := s.leaderWorker(lead, a.TodoID)
	if err != nil {
		return "", err
	}
	if w.DoneAt != nil {
		return "", fmt.Errorf("task %s is closed (%s): plan a new worker for what is left", w.ID, cmp.Or(w.DoneReason, "done"))
	}
	member, err := s.memberFor(w, a.SessionID)
	if err != nil {
		return "", err
	}
	if res, handled, err := s.leaderAnswerWorkerQuestion(ctx, lead, w, a.Text); handled {
		return res, err
	}
	sent, err := s.sendToMember(ownerfence.WithObserver(ctx, s.ownerInputRef(lead.MachineID, lead.SessionID)), w, member, a.Text)
	if errors.Is(err, errTaskAgentsStopped) {
		return "", fmt.Errorf("worker %s's Agent was stopped by the owner and takes no message: plan a new worker with task_plan "+
			"(put what it needs in the prompt) and cancel this one with task_review", w.ID)
	}
	if err != nil {
		return "", fmt.Errorf("the message to worker %s did not go: %v", w.ID, err)
	}
	return fmt.Sprintf("Worker %s (%q): %s.", w.ID, w.Title, sent), nil
}

// leaderTaskScope is the supervisor's task_scope: a worker's done_when or expect_minutes, or the expect_minutes of its
// own to-do (root), whose done_when is the main conversation's to change. Logged on the brief as task_scope is.
func (s *Service) leaderTaskScope(ctx context.Context, root Todo, lead TodoAgent, args json.RawMessage) (string, error) {
	a, err := taskleader.DecodeScope(args)
	if err != nil {
		return "", err
	}
	self := leaderRef(lead.MachineID, lead.SessionID)
	if a.TodoID == root.ID && !root.memberSupervised() {
		if a.DoneWhen != nil || len(a.Add)+len(a.Drop)+len(a.Edit)+len(a.Checks) > 0 {
			return "", errors.New("your own to-do's done_when is the main conversation's to change: ask it with report_to_supervisor " +
				"(kind question); on your own to-do you change only expect_minutes")
		}
		if a.ExpectMinutes == 0 {
			return "", errors.New("give expect_minutes: on your own to-do you change only the time it should take")
		}
		return s.scopeTask(ctx, self, root, a.Change, a.Why, nil, criteria.Ops{}, declaredChecks{}, a.ExpectMinutes)
	}
	w, err := s.leaderWorker(lead, a.TodoID)
	if err != nil {
		return "", err
	}
	ops := criteria.Ops{Add: a.Add, Drop: a.Drop}
	for _, e := range a.Edit {
		ops.Edit = append(ops.Edit, criteria.Edit{Number: e.Number, Text: e.Text})
	}
	var declared []acceptance.Declared
	if len(a.Checks) > 0 {
		if err := json.Unmarshal(a.Checks, &declared); err != nil {
			return "", fmt.Errorf("checks: %v", err)
		}
	}
	return s.scopeTask(ctx, self, w, a.Change, a.Why, a.DoneWhen, ops, declaredChecks{declared, acceptance.Supervisor}, a.ExpectMinutes)
}

// leaderWorkers are the worker to-dos of to-do root's task leaders (open and closed), oldest first: each worker's row
// names root (via), or its ledger names one of root's leaders.
func (s *Service) leaderWorkers(root Todo) []Todo {
	refs := map[string]bool{}
	for _, a := range root.Agents {
		if a.Source == sourceLeader {
			refs[leaderRef(s.canonical(a.MachineID), a.SessionID)] = true
		}
	}
	s.todos.mu.RLock()
	var out []Todo
	for _, t := range s.todos.todos {
		if t.ID == root.ID {
			continue
		}
		belongs := t.Supervision != nil && refs[t.Supervision.Supervisor]
		for _, a := range t.Agents {
			belongs = belongs || a.Source == sourceWorker && a.Via == root.ID
		}
		if belongs {
			out = append(out, copyTodoLocked(t))
		}
	}
	s.todos.mu.RUnlock()
	slices.SortFunc(out, func(a, b Todo) int { return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID)) })
	return out
}

// openLeaderWorkers are root's workers still open.
func (s *Service) openLeaderWorkers(root Todo) []Todo {
	return slices.DeleteFunc(s.leaderWorkers(root), func(t Todo) bool { return t.DoneAt != nil })
}

// supervisorCompletion is a supervisor Agent's done report on its own to-do root (report_to_supervisor kind done):
// the completion, once the hub's checks pass (taskleader.CompletionRefusal: every worker closed, every done_when met
// with evidence). The report then goes as any report does, though it wakes nobody itself (taskleader.Route); the
// to-do waits for review, and its supervisor (the main conversation) gets one review entry with the supervisor's
// checks and its workers as the hub has them. Refused, nothing is told and the to-do stays as it is.
func (s *Service) supervisorCompletion(ctx context.Context, root Todo, machine, session, text string, evidence []string, step, steps int, now time.Time) (string, error) {
	cur, ok := s.todoNow(root.ID)
	if !ok || cur.DoneAt != nil || cur.Supervision == nil {
		return "", fmt.Errorf("task %s is closed already", root.ID)
	}
	var criteria []taskleader.Criterion
	for i, c := range cur.Supervision.DoneWhen {
		cr := taskleader.Criterion{Text: c}
		if i < len(cur.Supervision.Criteria) && cur.Supervision.Criteria[i].Criterion == c {
			cr.State, cr.Evidence = cur.Supervision.Criteria[i].State, cur.Supervision.Criteria[i].Evidence
		}
		criteria = append(criteria, cr)
	}
	var workers []taskleader.WorkerState
	for _, w := range s.leaderWorkers(cur) {
		workers = append(workers, taskleader.WorkerState{ID: w.ID, Title: w.Title, Open: w.DoneAt == nil})
	}
	if why := taskleader.CompletionRefusal(criteria, workers); why != "" {
		return "", fmt.Errorf("not reported, and nothing went to the main conversation: %s", why)
	}
	if _, err := s.reportNow(ctx, cur, machine, session, upReportDone, text, evidence, step, steps, now); err != nil {
		return "", err
	}
	v, _, err := s.updateSupervision(ctx, cur.ID, func(v *Supervision) bool {
		if v.State == reviewPending && v.Waits == nil {
			return false
		}
		v.State, v.Waits = reviewPending, nil
		return true
	})
	if err != nil {
		return "", err
	}
	s.signal(superviseSignal{Todo: cur.ID, Kind: sigReview, Report: taskleader.ReportCompletion, Key: reviewKey(&v),
		Detail: clip(oneLine(text), 300)})
	s.logf("task %s: its supervisor %s reports it complete; it waits for review", cur.ID, short(session))
	return "Reported as complete: the main conversation reviews your to-do now, and its acceptance closes it. Do nothing more " +
		"unless it writes to you.", nil
}

// completionWorkers are the workers of to-do root as a completion entry lists them (prompts.SuperviseWorker).
func (s *Service) completionWorkers(ctx context.Context, root Todo) []prompts.SuperviseWorker {
	var out []prompts.SuperviseWorker
	for _, w := range s.leaderWorkers(root) {
		e := prompts.SuperviseWorker{ID: w.ID, Title: w.Title, Verdict: cmp.Or(w.Supervision.state(w), "open"), PR: w.PRURL}
		if w.Cancelled() {
			e.Verdict = reviewCancelled
		} else if w.DoneAt != nil && w.Supervision.state(w) != reviewCancelled {
			e.Verdict = reviewAccepted
		}
		if a, ok := workerAgent(w); ok {
			e.Machine = a.MachineID
			if d, ok := s.device(ctx, a.MachineID); ok {
				e.Machine = d.Name
			}
		}
		e.Merged = w.PRStatus != nil && w.PRStatus.URL == w.PRURL && w.PRStatus.State == "merged"
		out = append(out, e)
	}
	return out
}

// leaderReview is the leader agent's task_review on one of its direct workers: accepted closes it, cancelled closes and
// stops it, rework sends what is missing back to the worker (two rounds, then it goes up), needs_user sends the
// question to whoever handed the leader its task. No conclusion card, "Please verify" or other card goes to the user,
// and no other task is reachable: it is the leader's direct worker (its ledger names this leader) or nothing.
func (s *Service) leaderReview(ctx context.Context, lead TodoAgent, args json.RawMessage) (string, error) {
	var a struct {
		TodoID     string        `json:"todo_id"`
		SessionID  string        `json:"session_id"` // rework: the member of a to-do with several
		Verdict    string        `json:"verdict"`
		Verify     bool          `json:"verify"` // the hub runs the declared checks again; no verdict is written
		Outcome    string        `json:"outcome"`
		ReworkKind string        `json:"rework_kind"`
		Checks     []ReviewCheck `json:"checks"`
		Note       string        `json:"note"`
		Reason     string        `json:"reason"`
		Recommend  string        `json:"recommend"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return "", fmt.Errorf("a task leader's task_review takes only the fields its schema lists (%v)", err)
	}
	t, ok := s.todoNow(strings.TrimSpace(a.TodoID))
	if !ok {
		return "", fmt.Errorf("no task %q", a.TodoID)
	}
	if cur, supervised, reachable := s.leaderSupervisor(t); !supervised || !reachable || cur.MachineID != lead.MachineID || cur.SessionID != lead.SessionID {
		return "", fmt.Errorf("task %s is not one of your workers: you review the workers you handed out with task_plan, nothing else", t.ID)
	}
	if a.Verify {
		return s.verifyChecks(ctx, t)
	}
	a.Note = strings.TrimSpace(a.Note)
	a.ReworkKind = strings.TrimSpace(a.ReworkKind)
	switch a.Verdict {
	case reviewAccepted, reviewRework, reviewNeedsUser, reviewCancelled:
	default:
		return "", errors.New("verdict must be accepted, rework, needs_user or cancelled")
	}
	if a.Outcome != "" && (a.Verdict != reviewAccepted || a.Outcome != outcomeDone) {
		return "", errors.New("outcome done applies to accepted only")
	}
	if a.ReworkKind != "" && a.Verdict != reviewRework {
		return "", errors.New("rework_kind applies to rework only")
	}
	if !validReworkKind(a.ReworkKind) {
		return "", errors.New("rework_kind must be external_input, substantive_new, repeated_error or low_roi_edge")
	}
	if a.Verdict == reviewRework && a.Note == "" && !slices.ContainsFunc(a.Checks, func(c ReviewCheck) bool { return !c.Met }) {
		return "", errors.New("rework: say what is missing (a check that is not met, or note)")
	}
	if a.Verdict == reviewAccepted {
		if err := alreadyAccepted(ctx, t); err != nil {
			return "", err
		}
	}
	if a.Verdict == reviewAccepted && t.DoneAt == nil && t.memberSupervised() && len(s.openLeaderWorkers(t)) > 0 {
		return "", fmt.Errorf("task %s has open pieces: close them with task_review first", t.ID)
	}
	if t.DoneAt != nil && (a.Verdict == reviewRework || a.Verdict == reviewNeedsUser) {
		return "", fmt.Errorf("task %s is done already (%s): hand out a new worker for what is left", t.ID, cmp.Or(t.DoneReason, "closed"))
	}
	member := ""
	if a.Verdict == reviewRework {
		var err error
		if member, err = s.memberFor(t, a.SessionID); err != nil {
			return "", err
		}
	}
	var up string // the conversation a needs_user goes to: whoever handed the leader its task
	if lt, ok := s.leaderTaskOf(ctx, lead); ok {
		up = s.conversationAbove(ctx, lt)
	}
	if a.Verdict == reviewNeedsUser && up == "" {
		return "", errors.New("your own task has nobody to hand a decision to: decide it yourself, or review the worker with the other verdicts")
	}
	var current []string
	if t.Supervision != nil {
		current = t.Supervision.DoneWhen
	}
	resolved, byNumber, err := criteria.ResolveChecks(current, a.Checks)
	if err != nil {
		return "", err
	}
	a.Checks = resolved
	checks := make([]ReviewCheck, 0, len(a.Checks))
	for _, c := range a.Checks {
		c.Criterion, c.Evidence = criteria.Fit(c.Criterion), clip(strings.TrimSpace(c.Evidence), 600)
		if c.Criterion != "" {
			checks = append(checks, c)
		}
	}
	verdict, note := a.Verdict, clip(a.Note, 2000)
	over, overSafetyCap, overCount := false, false, 0
	var criteriaErr error
	gateNote := "" // the gates the main conversation's acceptance passes: owner wait, criteria, delivery (acceptanceGates)
	if verdict == reviewAccepted {
		if gateNote, err = s.acceptanceGates(ctx, t, a.Checks, a.Outcome, ""); err != nil {
			return "", err
		}
	}
	reviewBy := ""
	if hubActing(ctx) {
		reviewBy = reviewerHub
	}
	v, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
		if byNumber && !slices.Equal(v.DoneWhen, current) {
			criteriaErr = criteria.ErrChanged
			return false
		}
		if verdict == reviewAccepted {
			if t.DoneAt == nil && v.Waits != nil {
				criteriaErr = acceptance.OwnerWait(t.ID, v.Waits.Reason)
				return false
			}
			if criteriaErr = criteria.Complete(v.DoneWhen, a.Checks); criteriaErr != nil {
				return false
			}
		}
		if verdict == reviewRework {
			if blocked, safetyCap, count := v.reworkBlocked(a.ReworkKind); blocked {
				verdict, over, overSafetyCap, overCount = reviewNeedsUser, true, safetyCap, count
			}
		}
		r := TaskReview{At: time.Now().UTC(), Verdict: verdict, Checks: checks, Note: note, By: reviewBy}
		if verdict == reviewNeedsUser {
			r.Note = cmp.Or(r.Note, clip(oneLine(a.Reason), 200))
		}
		if verdict == reviewRework {
			round, stalled := v.applyReworkAccounting(a.ReworkKind)
			r.Round, r.ReworkKind, r.StalledReworkRounds = round, a.ReworkKind, stalled
		}
		v.State = verdict
		v.Reviews = append(v.Reviews, r)
		if len(v.Reviews) > 20 {
			v.Reviews = v.Reviews[len(v.Reviews)-20:]
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if criteriaErr != nil {
		return "", criteriaErr
	}
	if verdict == reviewNeedsUser && up == "" {
		up = s.settings(ctx).Main // reworked too often with nobody above the leader: the main conversation
	}
	if verdict != reviewAccepted { // by the leader's model and effort; an acceptance once it closed the worker (below)
		s.leaderTaskReviewed(lead, t.ID, verdict, v)
	}
	if verdict == reviewRework || verdict == reviewNeedsUser {
		s.setTaskStageKind(ctx, t.ID, "unverified", "", "", false)
	}
	b := &s.supervise
	b.mu.Lock()
	b.init()
	keys := b.kindKeys(t.ID, sigReview, sigIdle, sigUnjudged, sigNeedsYou, sigStalled, sigNoProgress, sigTimedOut, sigOrphaned, sigEstimateDue)
	for _, key := range keys {
		delete(b.pending, key) // reviewed: what waited is answered
	}
	s.forgetSupervisePending(keys...)
	b.mu.Unlock()
	switch verdict {
	case reviewAccepted, reviewCancelled:
		reason := cmp.Or(clip(oneLine(note), 80), "验收通过") // i18n-ignore: done_reason, as a conversation's review writes it
		if verdict == reviewCancelled {
			reason = "取消：" + cmp.Or(clip(oneLine(note), 80), "不做了") // i18n-ignore: done_reason, as a conversation's review writes it
		}
		if t.DoneAt == nil || t.StartError != "" {
			wasOpen := t.DoneAt == nil
			closed, err := s.reviewDoneTodo(ctx, t.ID, reason, verdict == reviewAccepted)
			if err != nil {
				return "", err
			}
			if wasOpen && closed && verdict == reviewAccepted { // a failed start was closed already: no acceptance to count
				s.leaderTaskReviewed(lead, t.ID, verdict, v)
			}
		}
		if verdict == reviewAccepted {
			s.sendsAccepted(ctx, t.ID, time.Now()) // what was sent its agent before is answered: closed already, too (#7539)
		}
		if verdict == reviewCancelled {
			s.stopTaskAgents(ctx, t)
			return fmt.Sprintf("Worker task %s is cancelled and closed; its agent is being stopped (its terminal stays) and archived.", t.ID), nil
		}
		return fmt.Sprintf("Worker task %s is accepted and closed; its agent is stopped once it stays still (its terminal stays).", t.ID) + gateNote, nil
	case reviewNeedsUser:
		why := cmp.Or(clip(oneLine(a.Reason), 200), clip(oneLine(note), 200))
		n := cmp.Or(overCount, maxReworkRounds)
		if over && a.Reason == "" {
			why = fmt.Sprintf("sent back %d times already, still short: %s", n, clip(oneLine(note), 160))
		}
		if cfg := s.settings(ctx); cfg.Main != "" {
			up = cfg.Main
		}
		ids := slices.DeleteFunc(s.waitingCards(t, true), func(id string) bool {
			ix, ok := s.pendingIx(id)
			return !ok || ix.Kind != proto.IxQuestion
		})
		s.ringAsks(t.ID, ids, &proto.Delegate{Reason: why, Recommend: clip(oneLine(a.Recommend), 200)}, escalation{})
		s.signal(superviseSignal{Todo: t.ID, Kind: sigDecide, To: up, Key: fmt.Sprintf("decide@%d@%s", v.Round, time.Now().UTC().Format(time.RFC3339Nano)),
			Detail: why, Ask: clip(oneLine(a.Recommend), 200)})
		if over {
			if overSafetyCap {
				return fmt.Sprintf("Worker task %s was sent back %d times already (overall rework safety cap %d): it now waits for a decision, which went up to whoever handed "+
					"you your task. Review it again once the answer reaches you.", t.ID, n, maxTotalReworkRounds), nil
			}
			return fmt.Sprintf("Worker task %s was sent back %d times already: it now waits for a decision, which went up to whoever handed "+
				"you your task. Review it again once the answer reaches you.", t.ID, n), nil
		}
		return fmt.Sprintf("Worker task %s waits for a decision, which went up to whoever handed you your task. You are told what "+
			"they decide; no card went to the user.", t.ID), nil
	}
	displayMax := maxTotalReworkRounds
	if isStalledReworkKind(a.ReworkKind) && v.Round <= maxReworkRounds {
		displayMax = maxReworkRounds
	}
	text := prompts.RenderSuperviseRework(prompts.SuperviseRework{Round: v.Round, Max: displayMax, Checks: reworkChecks(checks), Note: note})
	sent, err := s.sendToMember(ownerfence.WithObserver(ctx, s.ownerInputRef(lead.MachineID, lead.SessionID)), t, member, text)
	if err == nil {
		s.reworkMember(ctx, t, member)
	}
	if err != nil {
		return "", fmt.Errorf("recorded rework round %d, but sending it to the worker failed: %v", v.Round, err)
	}
	stalled := v.stalledReworkRounds()
	if isStalledReworkKind(a.ReworkKind) && v.Round == stalled {
		return fmt.Sprintf("Rework round %d of %d: %s. Its next stop comes back to you as a supervision message.", v.Round, maxReworkRounds, sent), nil
	}
	if a.ReworkKind != "" {
		return fmt.Sprintf("Rework round %d of %d (%s; stalled/low-ROI %d of %d): %s. Its next stop comes back to you as a supervision message.",
			v.Round, maxTotalReworkRounds, a.ReworkKind, stalled, maxReworkRounds, sent), nil
	}
	return fmt.Sprintf("Rework round %d of %d (stalled/low-ROI %d of %d): %s. Its next stop comes back to you as a supervision message.",
		v.Round, maxTotalReworkRounds, stalled, maxReworkRounds, sent), nil
}

// rehomeLeaderlessWorkers gives the open workers of a leader whose own task is closed a supervisor that can act: the
// conversation above the leader (its own supervisor, else the main one), and with no conversation at all the worker is
// cancelled with the reason. It runs from the watch loop and every digest, and is idempotent: a rehomed worker is no
// longer leader-supervised.
func (s *Service) rehomeLeaderlessWorkers(ctx context.Context) {
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return
	}
	s.todos.mu.RLock()
	var workers []Todo
	for _, t := range s.todos.todos {
		if t.DoneAt == nil && t.Supervision != nil {
			workers = append(workers, copyTodoLocked(t))
		}
	}
	s.todos.mu.RUnlock()
	for _, t := range workers {
		s.rehomeWorker(ctx, t)
	}
}

// rehomeWorker rehomes worker task t if its leader's task is closed (see rehomeLeaderlessWorkers); it gives the task as
// it is after, and cancelled true when there was nobody to hand it to. The supervisor and the origin move together:
// the origin is where the task's notices and receipts go.
func (s *Service) rehomeWorker(ctx context.Context, t Todo) (cur Todo, cancelled bool) {
	lead, _, ok := s.leaderAbove(t)
	if !ok || lead.ID != "" && s.leaderOpen(lead) { // not leader-supervised, or its leader's task is open
		return t, false
	}
	up, _ := s.supervisorAboveTask(ctx, t)
	cancelledRoot := lead.ID != "" && lead.Cancelled() // its whole to-do was called off: its workers go with it
	if up == "" || s.isLeaderRef(up) || cancelledRoot {
		reason := "its task leader is gone, and there is no conversation above it to take it over" // i18n-ignore: done_reason
		if cancelledRoot {
			reason = "its to-do was cancelled" // i18n-ignore: done_reason
		}
		s.logf("task %s: %s; cancelled", t.ID, reason)
		if _, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool { v.State = reviewCancelled; return true }); err != nil {
			s.logf("task %s: rehome: %v", t.ID, err)
		}
		if _, err := s.reviewDoneTodo(ctx, t.ID, reason, false); err != nil {
			s.logf("task %s: rehome: %v", t.ID, err)
		}
		cur, _ = s.todoNow(t.ID)
		s.stopTaskAgents(ctx, cur)
		return cur, true
	}
	s.logf("task %s: its task leader is gone; %s takes it over", t.ID, up)
	if _, err := s.editTodo(ctx, t.ID, db.TodoEdit{Origin: &up}); err != nil {
		s.logf("task %s: rehome: origin: %v", t.ID, err)
		return t, false // nothing changed yet: tried again at the next pass
	}
	s.todos.mu.Lock() // the stored row moved; memory follows (setTodoRow leaves the origin alone for every other writer)
	if m := s.todos.todos[t.ID]; m != nil {
		m.Origin = up
	}
	s.todos.mu.Unlock()
	s.todosChanged(ctx)
	var pending bool
	v, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
		v.Supervisor = up
		if v.SupervisorSource == autosupervise.Auto { // an adopted piece goes back as it was: the hub's own decision again
			v.SupervisorSource, v.Above = "", ""
		}
		if v.State == reviewRework {
			v.State = reviewPending // what the leader asked for is with the worker: the new supervisor reviews what comes back
		}
		pending = v.state(t) == reviewPending
		return true
	})
	if err != nil {
		s.logf("task %s: rehome: %v", t.ID, err)
		return t, false
	}
	if pending {
		s.signal(superviseSignal{Todo: t.ID, Kind: sigReview, Key: reviewKey(&v), Detail: "its task leader is gone; this task is handed to you for review"})
	}
	cur, _ = s.todoNow(t.ID)
	return cur, false
}

// leaderOpen: the leader's own task is open.
func (s *Service) leaderOpen(t Todo) bool { return t.ID != "" && t.DoneAt == nil && !t.Cancelled() }

// mayReviewForLeader: conversation self supervises the task leader that supervises worker task t, so it can review the
// worker (a decision the leader sent up). The worker's supervisor stays the leader's agent: this never takes it over.
func (s *Service) mayReviewForLeader(ctx context.Context, self string, t Todo) bool {
	up, _ := s.supervisorAboveTask(ctx, t)
	if up == "" {
		return false
	}
	// The background supervisor stands in for main only when this worker really had a legacy notice for it. Being
	// configured as background never grants acceptance over a new task-leader worker (#5732). An attested round fork
	// shares only its base's already recorded notice, never a fresh acceptance grant.
	self = s.supervisionHoldKey(ctx, self)
	return up == self || s.isBackground(ctx, self) && s.legacyBackgroundNotice(ctx, self, t.ID)
}

// leaderTaskOf is the open task leader agent lead leads.
func (s *Service) leaderTaskOf(ctx context.Context, lead TodoAgent) (Todo, bool) {
	t, ok := s.taskOfAny(ctx, lead.MachineID, lead.SessionID)
	return t, ok
}

// leaderResumeWait bounds how long resuming a closed leader agent may take before the message goes up to the conversation
// above it instead (Service.leaderResume shortens it in tests).
const leaderResumeWait = 3 * time.Minute

// leaderResumeTime is s's own leaderResumeWait. Tests set it on their Service, so a retry goroutine left over from one
// test (it may run for a day) never reads what another test's cleanup writes.
func (s *Service) leaderResumeTime() time.Duration {
	if s.leaderResume > 0 {
		return s.leaderResume
	}
	return leaderResumeWait
}

// leaderRetryBackoff caps how often an undelivered worker supervision message is tried again.
func leaderRetryBackoff(d time.Duration) time.Duration {
	if d < 10*time.Minute {
		return d * 2
	}
	return 30 * time.Minute
}

// A leader replies to its current option card through the same claim as interaction_answer. The scoped tool
// accepts a label for a single question or a JSON object keyed by question when the card asks several.
func (s *Service) leaderAnswerWorkerQuestion(ctx context.Context, lead TodoAgent, task Todo, text string) (string, bool, error) {
	cards := s.heldAsks(task.ID, leaderRef(lead.MachineID, lead.SessionID))
	for _, card := range cards {
		if card.Kind != proto.IxQuestion {
			continue
		}
		ix, ok := s.pendingIx(card.ID)
		if !ok {
			continue
		}
		ctx, err := s.supervisoryAction(ctx, lead.MachineID, lead.SessionID, ix.MachineID, ix.SessionID, ix.CreatedAt)
		if err != nil {
			return "", true, err
		}
		raw := json.RawMessage(strings.TrimSpace(text))
		if len(ix.Questions) == 1 && !bytes.HasPrefix(raw, []byte("{")) {
			raw, _ = json.Marshal(map[string]string{ix.Questions[0].Question: text})
		}
		answers, err := ownerfence.InteractionAnswers(ix, raw)
		if err != nil {
			return "", true, err
		}
		ok, err = s.answerQuestion(ctx, ix.MachineID, ix.SessionID, ix.ID, answers, "")
		if err != nil && !errors.Is(err, errNotPending) {
			return "", true, err
		}
		if !ok {
			return "", true, s.questionOutcome(ctx, ix.MachineID, ix.SessionID, ix.ID, err).ToolError(ix.ID)
		}
		s.askAnswered(ctx, task.ID, lead.SessionID, ix, answers, "")
		return fmt.Sprintf("Answered worker question %s.", ix.ID), true, nil
	}
	return "", false, nil
}
