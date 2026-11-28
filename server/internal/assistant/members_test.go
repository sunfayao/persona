package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/membership"
	"github.com/sunfayao/persona/server/internal/assistant/taskleader"
	"github.com/sunfayao/persona/server/internal/proto"
)

// listMembers lists every Agent of every to-do as a running session on its computer, plus the main conversation.
func listMembers(s *Service, f *fakeHub, state string) {
	byMachine := map[string][]proto.Session{"server": {{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle}}}
	status := proto.StatusWorking
	if state != proto.StateWorking {
		status = proto.StatusIdle
	}
	for _, t := range s.todosOf(context.Background(), "*") {
		for _, a := range t.Agents {
			byMachine[a.MachineID] = append(byMachine[a.MachineID], proto.Session{SessionID: a.SessionID, MachineID: a.MachineID, Cwd: a.Cwd,
				Agent: "claude", Status: status, State: state, UpdatedAt: time.Now()})
		}
	}
	for machine, list := range byMachine {
		f.setSessions(machine, list...)
	}
}

// firstAgent is the session of the Agent the first plan starts (memberRequest).
const firstAgent = "agent-000001"

// memberRequest is a to-do of the main conversation with its Agent on box at work (the setting is explicit: the main
// conversation supervises it itself), and a second computer mac.
func memberRequest(t *testing.T) (*Service, *fakeHub, Todo) {
	t.Helper()
	s, f := newQuietServiceOn(t, nil)
	f.judge = `{"verdict":"done","reason":"PR 已合并"}`
	f.startReply = func(_ map[string]any, sess *proto.Session) { sess.SessionID = fmt.Sprintf("agent-%06d", f.agents) } // ids a tool can name
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{
		"title": "修登录崩溃", "prompt": "iPad 横屏时登录页崩溃，修好并补测试", "role": "worker", "machine": "box", "cwd": "/code/app", "readonly": true,
		"why": "这个仓库最近在 box 上", "done_when": []string{"PR 合并到 master", "浅色 / 深色截图贴进 PR"}, "expect_minutes": 30,
	}}})
	if _, err := s.toolTaskPlan(context.Background(), "conv1", raw); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { return len(onlyTodo(s).Agents) == 1 })
	f.setSessions("mac", proto.Session{SessionID: "m0", MachineID: "mac", Status: proto.StatusIdle})
	listMembers(s, f, proto.StateWorking)
	return s, f, onlyTodo(s)
}

// joinMember plans one more Agent on the to-do from the main conversation (readonly: it starts at once) and waits for it.
func joinMember(t *testing.T, s *Service, f *fakeHub, todo Todo, task map[string]any) TodoAgent {
	t.Helper()
	base := map[string]any{"prompt": "do the web side", "role": "worker", "machine": "mac", "cwd": "/code/web", "readonly": true,
		"why": "the web checkout is on mac", "todo_id": todo.ID, "join": true}
	for k, v := range task {
		base[k] = v
	}
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{base}})
	before := len(todoNow(s, todo.ID).workingMembers())
	if res, err := s.toolTaskPlan(context.Background(), "conv1", raw); err != nil {
		t.Fatalf("join: %v", err)
	} else if !strings.Contains(res, "joins to-do "+todo.ID) {
		t.Fatalf("join answer: %s", res)
	}
	waitFor(t, "the second member", func() bool {
		return len(todoNow(s, todo.ID).workingMembers()) == before+1 && todoNow(s, todo.ID).workingMembers()[before].Assignment != nil
	})
	listMembers(s, f, proto.StateWorking)
	list := todoNow(s, todo.ID).Agents
	return list[len(list)-1]
}

// A second Agent on another computer joins the to-do, and the Agent already there is neither stopped, released nor told
// anything: the to-do keeps its one ledger.
func TestASecondAgentOnAnotherComputerJoinsAndTheFirstKeepsWorking(t *testing.T) {
	s, f, todo := memberRequest(t)
	ledger := *todo.Supervision
	m := joinMember(t, s, f, todo, map[string]any{"title": "web side", "done_when": []string{"2", "PR 合并到 master"}, "expect_minutes": 20})
	cur := todoNow(s, todo.ID)
	if len(cur.workingMembers()) != 2 || m.MachineID != "mac" || m.Source != "manual" || m.SessionID == "agent-000001" {
		t.Fatalf("members: %+v", cur.Agents)
	}
	a := m.Assignment
	if a == nil || a.Title != "web side" || a.ExpectMinutes != 20 || a.State != membership.StateWorking ||
		len(a.Criteria) != 2 || a.Criteria[0] != "浅色 / 深色截图贴进 PR" || a.Criteria[1] != "PR 合并到 master" {
		t.Fatalf("assignment: %+v", a)
	}
	first := cur.Agents[0]
	if first.SessionID != "agent-000001" || first.ReleasedAt != nil || first.Assignment != nil {
		t.Fatalf("the first Agent: %+v", first)
	}
	if got := toSession2(f, "box", "agent-000001"); got != "" {
		t.Fatalf("the first Agent was told: %q", got)
	}
	f.mu.Lock()
	stopped, retired := len(f.stopped), len(f.retired)
	start := f.starts[len(f.starts)-1]
	f.mu.Unlock()
	if stopped+retired != 0 {
		t.Fatalf("an Agent was stopped: %d %d", stopped, retired)
	}
	prompt, _ := start["prompt"].(string)
	for _, want := range []string{"Your part: web side", "  1. PR 合并到 master", "  2. 浅色 / 深色截图贴进 PR", "about 20 minutes", "do the web side", "do not stop, redo or undo"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the member's first message lacks %q:\n%s", want, prompt)
		}
	}
	if v := cur.Supervision; v.Supervisor != ledger.Supervisor || len(v.DoneWhen) != len(ledger.DoneWhen) || v.ExpectMinutes != ledger.ExpectMinutes || v.Round != ledger.Round {
		t.Fatalf("the ledger changed: %+v, was %+v", v, ledger)
	}
	if len(s.todosOf(context.Background(), "*")) != 1 {
		t.Fatal("a to-do was made for the member")
	}
}

// toSession2 are the texts sent into session of machine.
func toSession2(f *fakeHub, machine, session string) string {
	return strings.Join(sendsTo(f, machine, session), "\n---\n")
}

// waitingSignalIDs are the ids of the signals that wait to be told, sorted.
func waitingSignalIDs(s *Service) []string {
	b := &s.supervise
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.pending))
	for id := range b.pending {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// holdDigests makes supervision messages wait for an hour: the signals stay where a test can read them.
func holdDigests(s *Service) {
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
}

// The report, the estimate and the stop of two members live side by side, each under its own id; a to-do with one
// member has the ids it always had.
func TestTheSignalsOfTwoMembersDoNotReplaceEachOther(t *testing.T) {
	s, f, todo := memberRequest(t)
	holdDigests(s)
	raise := func(machine, session string) {
		for _, g := range []superviseSignal{
			{Kind: sigReport, Report: upReportQuestion, Key: "q@" + session},
			{Kind: sigEstimateDue, Key: "e@" + session},
			{Kind: sigIdle, Key: "i@" + session},
			{Kind: sigReview, Key: "r@" + session}, // the to-do's own: one for both
		} {
			g.Todo, g.Machine, g.Session = todo.ID, machine, session
			s.signal(g)
		}
	}
	raise("box", firstAgent)
	want := []string{todo.ID + "|estimate_due", todo.ID + "|idle", todo.ID + "|report:question", todo.ID + "|review"}
	if got := waitingSignalIDs(s); !slices.Equal(got, want) {
		t.Fatalf("one member: ids %v, want the ones it always had %v", got, want)
	}
	m := joinMember(t, s, f, todo, map[string]any{"title": "web side"})
	raise("box", firstAgent) // each tells again, as the watch does
	raise("mac", m.SessionID)
	got := waitingSignalIDs(s)
	for _, id := range []string{todo.ID + "|estimate_due#" + firstAgent, todo.ID + "|idle#" + firstAgent, todo.ID + "|report:question#" + firstAgent,
		todo.ID + "|estimate_due#" + m.SessionID, todo.ID + "|idle#" + m.SessionID, todo.ID + "|report:question#" + m.SessionID, todo.ID + "|review"} {
		if !slices.Contains(got, id) {
			t.Errorf("no signal %s among %v", id, got)
		}
	}
	// A signal an older build queued has no mark and keeps the id it had.
	if g := (superviseSignal{Todo: "t", Kind: sigStalled, Session: firstAgent}); g.id() != "t|stalled" {
		t.Fatalf("an old signal's id: %s", g.id())
	}
	// What a review clears is every member's.
	if _, _, err := s.updateSupervision(context.Background(), todo.ID, func(v *Supervision) bool { v.State = reviewPending; return true }); err != nil {
		t.Fatal(err)
	}
	b := &s.supervise
	b.mu.Lock()
	keys := b.kindKeys(todo.ID, sigIdle, sigEstimateDue)
	b.mu.Unlock()
	if len(keys) != 6 { // two kinds: the plain id, and one for each of the two sessions
		t.Fatalf("keys of a review: %v", keys)
	}
}

// memberStates puts each session of every to-do in the state states names ("" for the others: working).
func memberStates(s *Service, f *fakeHub, states map[string]string) {
	byMachine := map[string][]proto.Session{"server": {{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle}}}
	for _, t := range s.todosOf(context.Background(), "*") {
		for _, a := range t.Agents {
			c := proto.Session{SessionID: a.SessionID, MachineID: a.MachineID, Cwd: a.Cwd, Agent: "claude", Status: proto.StatusWorking, State: proto.StateWorking, UpdatedAt: time.Now()}
			if states[a.SessionID] == proto.StateIdle {
				c.Status, c.State, c.LastText = proto.StatusDone, proto.StateUnread, "done: "+a.SessionID
			}
			byMachine[a.MachineID] = append(byMachine[a.MachineID], c)
		}
	}
	for machine, list := range byMachine {
		f.setSessions(machine, list...)
	}
}

// memberReport is report_to_supervisor from the Agent machine/session.
func memberReport(t *testing.T, s *Service, machine, session, kind, text string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": kind, "text": text})
	if _, err := s.reportToSupervisor(context.Background(), machine, session, raw, time.Now()); err != nil {
		t.Fatalf("report: %v", err)
	}
}

// The to-do waits for review when its last working member has finished; a member that reports done while another works
// has its part marked done, its report is its own, and nothing of the to-do is asked for review yet.
func TestTheReviewIsRaisedOnlyWhenTheLastMemberFinished(t *testing.T) {
	s, f, todo := memberRequest(t)
	ctx := context.Background()
	m := joinMember(t, s, f, todo, map[string]any{"title": "web side"})
	holdDigests(s)
	s.taskWorking(ctx, "box", firstAgent)
	s.taskWorking(ctx, "mac", m.SessionID)
	// The first Agent finishes while the second still works.
	memberStates(s, f, map[string]string{firstAgent: proto.StateIdle})
	memberReport(t, s, "box", firstAgent, upReportDone, "the app side is done")
	s.taskTurnEnded(ctx, "box", firstAgent)
	cur := todoNow(s, todo.ID)
	if st := cur.Supervision.state(cur); st != "" {
		t.Fatalf("the to-do waits for review (%q) while a member works", st)
	}
	if a := cur.Agents[0].Assignment; a == nil || a.State != membership.StateDone || a.Title == "" {
		t.Fatalf("the first member's part: %+v", a)
	}
	if b, ok := s.briefOfTodo(todo.ID); ok && len(b.Log) > 0 {
		for _, e := range b.Log {
			if e.Kind == briefEventCompletion {
				t.Fatalf("a completion was recorded for the to-do: %+v", e)
			}
		}
	}
	if !slices.Contains(waitingSignalIDs(s), todo.ID+"|report:done#"+firstAgent) {
		t.Fatalf("the member's report is not its own: %v", waitingSignalIDs(s))
	}
	// The second finishes: now it waits for review.
	memberStates(s, f, map[string]string{firstAgent: proto.StateIdle, m.SessionID: proto.StateIdle})
	memberReport(t, s, "mac", m.SessionID, upReportDone, "the web side is done")
	s.taskTurnEnded(ctx, "mac", m.SessionID)
	waitFor(t, "the review", func() bool { return todoNow(s, todo.ID).Supervision.State == reviewPending })
	if a := todoNow(s, todo.ID).Agents[len(cur.Agents)-1].Assignment; a == nil || a.State != membership.StateDone {
		t.Fatalf("the second member's part: %+v", a)
	}
}

// A message about a to-do with several working members needs the member named: unnamed, it is refused with the members
// and nothing is done; named, it goes to that member alone and puts its part back to work.
func TestAnUnnamedMessageToATwoMemberTodoIsRefusedWithTheMemberList(t *testing.T) {
	s, f, todo := memberRequest(t)
	ctx := context.Background()
	m := joinMember(t, s, f, todo, map[string]any{"title": "web side"})
	memberStates(s, f, map[string]string{firstAgent: proto.StateIdle, m.SessionID: proto.StateIdle})
	memberReport(t, s, "mac", m.SessionID, upReportDone, "the web side is done")
	rework := map[string]any{"todo_id": todo.ID, "verdict": "rework", "note": "the screenshots are missing"}
	raw, _ := json.Marshal(rework)
	_, err := s.runTool(ctx, "server", "conv1", "task_review", raw)
	if err == nil || !strings.Contains(err.Error(), "name the one it is for") || !strings.Contains(err.Error(), "members: ") ||
		!strings.Contains(err.Error(), short(m.SessionID)+" (web side, done)") || !strings.Contains(err.Error(), short(firstAgent)+" (") {
		t.Fatalf("an unnamed rework: %v", err)
	}
	if v := todoNow(s, todo.ID).Supervision; v.Round != 0 || len(v.Reviews) != 0 || len(deliveredTo(f, firstAgent))+len(deliveredTo(f, m.SessionID)) != 0 {
		t.Fatalf("something was done: %+v", v)
	}
	rework["session_id"] = m.SessionID
	raw, _ = json.Marshal(rework)
	if res, err := s.runTool(ctx, "server", "conv1", "task_review", raw); err != nil || !strings.Contains(res, "Rework round 1") {
		t.Fatalf("a named rework: %q %v", res, err)
	}
	waitFor(t, "the message", func() bool { return len(deliveredTo(f, m.SessionID)) == 1 })
	if got := deliveredTo(f, firstAgent); len(got) != 0 {
		t.Fatalf("the other member was told: %q", got)
	}
	if a := todoNow(s, todo.ID).Agents[1].Assignment; a == nil || a.State != membership.StateWorking || a.Round != 1 {
		t.Fatalf("the member's part after its rework: %+v", a)
	}
	// One member keeps working as before: no name needed.
	s2, f2, todo2 := memberRequest(t)
	_ = f2
	memberStates(s2, f2, map[string]string{firstAgent: proto.StateIdle})
	rework["todo_id"] = todo2.ID
	delete(rework, "session_id")
	raw, _ = json.Marshal(rework)
	if res, err := s2.runTool(ctx, "server", "conv1", "task_review", raw); err != nil || !strings.Contains(res, "Rework round 1") {
		t.Fatalf("a single member: %q %v", res, err)
	}
}

// planTodo plans one more read-only task of the main conversation on box and waits for its Agent.
func planTodo(t *testing.T, s *Service, title string) Todo {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{"title": title, "prompt": "do " + title, "role": "worker", "machine": "box", "cwd": "/code/other",
		"readonly": true, "why": "the checkout is on box", "done_when": []string{"it is done"}, "expect_minutes": 30}}})
	before := len(s.todosOf(context.Background(), "*"))
	if _, err := s.toolTaskPlan(context.Background(), "conv1", raw); err != nil {
		t.Fatal(err)
	}
	var found Todo
	waitFor(t, "the second to-do", func() bool {
		for _, c := range s.todosOf(context.Background(), "*") {
			if c.Title == title && len(c.Agents) == 1 {
				found = c
			}
		}
		return len(s.todosOf(context.Background(), "*")) == before+1 && found.ID != ""
	})
	return found
}

// memberBriefsTo are the texts session.send_once put into machine's session (what agent_supervise tells an Agent).
func memberBriefsTo(f *fakeHub, machine, session string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		raw, _ := json.Marshal(c.Params)
		var p struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if c.Device == machine && c.Method == DeviceSessionSendOnce && json.Unmarshal(raw, &p) == nil && p.SessionID == session {
			out = append(out, p.Text)
		}
	}
	return out
}

func superviseArgs(m map[string]any) json.RawMessage {
	raw, _ := json.Marshal(m)
	return raw
}

// agent_supervise with todo_id and an action: attach links an Agent as a member, detach lets it go (it keeps running), move
// does both between two to-dos the caller supervises; the last Agent of a to-do in progress cannot leave it.
func TestDetachMoveAndTheLastMemberRefusal(t *testing.T) {
	s, f, a := memberRequest(t)
	ctx := context.Background()
	b := planTodo(t, s, "other work")
	m := joinMember(t, s, f, a, map[string]any{"title": "web side"})
	all := func() {
		f.setSessions("box", proto.Session{SessionID: firstAgent, MachineID: "box", Status: proto.StatusWorking, State: proto.StateWorking, Agent: "claude", Cwd: "/code/app"},
			proto.Session{SessionID: b.Agents[0].SessionID, MachineID: "box", Status: proto.StatusWorking, State: proto.StateWorking, Agent: "claude", Cwd: "/code/other"})
		f.setSessions("mac", proto.Session{SessionID: m.SessionID, MachineID: "mac", Status: proto.StatusWorking, State: proto.StateWorking, Agent: "claude", Cwd: "/code/web"})
	}
	all()
	call := func(machine, session, todo, action string) (string, error) {
		return s.toolAgentSupervise(ctx, "conv1", superviseArgs(map[string]any{"machine": machine, "session_id": session, "todo_id": todo, "action": action}))
	}
	members := func(id string) []string {
		var out []string
		for _, x := range todoNow(s, id).workingMembers() {
			out = append(out, x.Session)
		}
		return out
	}
	if res, err := call("box", firstAgent, a.ID, "detach"); err != nil || !strings.Contains(res, "keeps running") {
		t.Fatalf("detach: %q %v", res, err)
	}
	if got := members(a.ID); len(got) != 1 || got[0] != m.SessionID {
		t.Fatalf("members after detach: %v", got)
	}
	if got := todoNow(s, a.ID).Agents[0]; got.SessionID != firstAgent || got.ReleasedAt == nil {
		t.Fatalf("the detached row: %+v", got)
	}
	if _, err := call("mac", m.SessionID, a.ID, "detach"); err == nil || !strings.Contains(err.Error(), "last Agent working") || strings.Count(err.Error(), ";") > 1 {
		t.Fatalf("the last member left: %v", err)
	}
	if got := members(a.ID); len(got) != 1 {
		t.Fatalf("members after the refusal: %v", got)
	}
	if res, err := call("box", firstAgent, a.ID, "attach"); err != nil || !strings.Contains(res, "now a member") {
		t.Fatalf("attach: %q %v", res, err)
	}
	if got := memberBriefsTo(f, "box", firstAgent); len(got) == 0 || !strings.Contains(got[len(got)-1], "you join to-do "+a.ID) {
		t.Fatalf("the attached Agent was not told its part: %q", got)
	}
	if got := members(a.ID); len(got) != 2 {
		t.Fatalf("members after attach: %v", got)
	}
	if _, err := call("box", firstAgent, a.ID, "attach"); err == nil || !strings.Contains(err.Error(), "member of to-do") {
		t.Fatalf("attached twice: %v", err)
	}
	if res, err := call("mac", m.SessionID, b.ID, "move"); err != nil || !strings.Contains(res, "member of to-do "+b.ID) {
		t.Fatalf("move: %q %v", res, err)
	}
	if got := members(a.ID); len(got) != 1 || got[0] != firstAgent {
		t.Fatalf("the to-do it left: %v", got)
	}
	if got := members(b.ID); len(got) != 2 || got[1] != m.SessionID {
		t.Fatalf("the to-do it joined: %v", got)
	}
	if _, err := call("box", firstAgent, b.ID, "move"); err == nil || !strings.Contains(err.Error(), "last Agent working") {
		t.Fatalf("the last member moved: %v", err)
	}
	// A to-do the main conversation does not supervise is not touched.
	if _, _, err := s.updateSupervision(ctx, b.ID, func(v *Supervision) bool { v.Supervisor = "someone-else"; return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := call("box", firstAgent, b.ID, "move"); err == nil || !strings.Contains(err.Error(), "not supervised from here") {
		t.Fatalf("another's to-do: %v", err)
	}
}

// agent_send with task.todo_id gives an Agent that runs on no task work on a to-do the main conversation supervises: it
// becomes a member with that part, is told it first, and the Agents there are left alone.
func TestAgentSendWithATodoAttachesAMember(t *testing.T) {
	s, f, todo := memberRequest(t)
	f.setSessions("mac", proto.Session{SessionID: "helper-00001", MachineID: "mac", Cwd: "/code/web", Agent: "claude", Title: "helper",
		Status: proto.StatusIdle, State: proto.StateIdle, UpdatedAt: time.Now()})
	res, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "mac", "session_id": "helper-00001", "text": "take the web side",
		"task": map[string]any{"title": "web side", "todo_id": todo.ID, "done_when": []string{"#2"}, "expect_minutes": 15}})
	if err != nil || !strings.Contains(res, "now a member of to-do "+todo.ID) {
		t.Fatalf("%q %v", res, err)
	}
	cur := todoNow(s, todo.ID)
	if len(cur.workingMembers()) != 2 || len(s.todosOf(context.Background(), "*")) != 1 {
		t.Fatalf("members: %+v", cur.Agents)
	}
	a := cur.Agents[1].Assignment
	if cur.Agents[1].SessionID != "helper-00001" || a == nil || a.Title != "web side" || a.ExpectMinutes != 15 || len(a.Criteria) != 1 || a.Criteria[0] != "浅色 / 深色截图贴进 PR" {
		t.Fatalf("the new member: %+v", cur.Agents[1])
	}
	got := deliveredTo(f, "helper-00001")
	if len(got) != 1 || !strings.HasPrefix(got[0], "(Persona: you join to-do "+todo.ID) || !strings.HasSuffix(got[0], "take the web side") || !strings.Contains(got[0], "  2. 浅色 / 深色截图贴进 PR") {
		t.Fatalf("what the member was told: %q", got)
	}
	if got := deliveredTo(f, firstAgent); len(got) != 0 {
		t.Fatalf("the first Agent was told: %q", got)
	}
	// A criterion the to-do does not have is refused before anything is sent.
	f.setSessions("mac", proto.Session{SessionID: "helper-00002", MachineID: "mac", Cwd: "/code/web", Agent: "claude", Status: proto.StatusIdle, State: proto.StateIdle, UpdatedAt: time.Now()})
	if _, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "mac", "session_id": "helper-00002", "text": "x",
		"task": map[string]any{"title": "more", "todo_id": todo.ID, "done_when": []string{"7"}}}); err == nil || !strings.Contains(err.Error(), "none of the to-do's") || !strings.Contains(err.Error(), "1. PR 合并到 master") {
		t.Fatalf("an unknown criterion: %v", err)
	}
	if got := deliveredTo(f, "helper-00002"); len(got) != 0 || len(todoNow(s, todo.ID).workingMembers()) != 2 {
		t.Fatalf("something went: %q", got)
	}
}

// supervisedRequest is autoRequest after the hub attached the supervisor Agent s2 to the request: the Agent s1 works on it,
// supervised by s2, with mac a second computer.
func supervisedRequest(t *testing.T) (*Service, *fakeHub, Todo, TodoAgent) {
	t.Helper()
	s, f, todo := autoRequest(t)
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	f.setSessions("mac", proto.Session{SessionID: "m0", MachineID: "mac", Status: proto.StatusIdle})
	listAutoAgents(s, f)
	lead, _ := supervisorAgentOf(s, todo.ID)
	return s, f, todoNow(s, todo.ID), lead
}

// A supervisor Agent joins one more member to a to-do it supervises, through its own task_plan; a to-do it does not
// supervise is refused.
func TestASupervisorAgentJoinsAMemberToWhatItSupervisesAndNotToWhatItDoesNot(t *testing.T) {
	s, _, todo, lead := supervisedRequest(t)
	ctx := context.Background()
	join := func(id string) proto.ToolResult {
		raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{"title": "web side", "prompt": "do the web side", "role": "worker", "machine": "mac",
			"cwd": "/code/web", "readonly": true, "todo_id": id, "join": true, "done_when": []string{"2"}}}})
		return s.mcpCall(ctx, lead.MachineID, lead.SessionID, "task_plan", raw, false)
	}
	res := join(todo.ID)
	if res.IsError || !strings.Contains(res.Content[0].Text, "joins to-do "+todo.ID) {
		t.Fatalf("join: %+v", res)
	}
	waitFor(t, "the member", func() bool { return len(todoNow(s, todo.ID).workingMembers()) == 2 })
	cur := todoNow(s, todo.ID)
	if m := cur.workingMembers()[1]; m.Session == lead.SessionID || m.Machine != "mac" || m.Assignment == nil || len(m.Assignment.Criteria) != 1 {
		t.Fatalf("members: %+v", cur.Agents)
	}
	if !cur.memberSupervised() || cur.Supervision.Supervisor != leaderRef(lead.MachineID, lead.SessionID) {
		t.Fatalf("the ledger moved: %+v", cur.Supervision)
	}
	// Another to-do, the main conversation's own, is not the supervisor Agent's.
	other := planTodo(t, s, "other work")
	if res := join(other.ID); !res.IsError || !strings.Contains(res.Content[0].Text, "not supervised from here") {
		t.Fatalf("a to-do it does not supervise: %+v", res)
	}
	if got := len(todoNow(s, other.ID).workingMembers()); got != 1 {
		t.Fatalf("the other to-do has %d members", got)
	}
}

// The supervisor Agent is never a member: the code that raises all_stopped, stalled and cardless_wait counts the Agents
// that work on the to-do, not the one that supervises them.
func TestTheSupervisorRowIsNotCountedByAllStoppedStalledAndCardlessWait(t *testing.T) {
	now := time.Now()
	ref := leaderRef("server", "sup-00001")
	todo := Todo{ID: "t1", Origin: "conv1", Supervision: &Supervision{Supervisor: ref, ExpectMinutes: 30, UpdatedAt: now.Add(-6 * time.Hour)}, Agents: []TodoAgent{
		{MachineID: "box", SessionID: "work-0001", Source: "manual", LinkedAt: now.Add(-7 * time.Hour)},
		{MachineID: "server", SessionID: "sup-00001", Source: sourceLeader, LinkedAt: now.Add(-3 * time.Hour)}}}
	if !todo.memberSupervised() || todo.supervisorRow(todo.Agents[0]) || !todo.supervisorRow(todo.Agents[1]) {
		t.Fatal("the rows are not read as a supervisor and a member")
	}
	t.Run("all_stopped", func(t *testing.T) {
		stopped := proto.Session{MachineID: "box", SessionID: "work-0001", State: proto.StateIdle, Status: proto.StatusIdle, StateSince: now.Add(-time.Hour)}
		supervising := proto.Session{MachineID: "server", SessionID: "sup-00001", State: proto.StateWorking, Status: proto.StatusWorking, Assistant: true}
		g, ok := allStoppedSignal(todo, nil, []proto.Session{stopped, supervising}, now)
		if !ok || g.Kind != sigAllStopped || strings.Contains(g.Detail, "sup-00001") || strings.Contains(g.Key, "sup-00001") {
			t.Fatalf("the supervisor's work held the signal back, or was counted in it: %v %+v", ok, g)
		}
		both := todo
		both.Agents = slices.Clone(todo.Agents)
		both.Agents[1].Source = "manual" // a second member that works: not all stopped
		if _, ok := allStoppedSignal(both, nil, []proto.Session{stopped, supervising}, now); ok {
			t.Fatal("all stopped with a member working")
		}
	})
	t.Run("cardless_wait", func(t *testing.T) {
		lists := machineSessions{"box": {}, "server": {err: errors.New("the supervisor's computer did not answer")}}
		if !cardlessAgentsReachable(todo, lists) {
			t.Fatal("the supervisor's computer held the hand-back")
		}
		lists["box"] = machineList{err: errors.New("down")}
		if cardlessAgentsReachable(todo, lists) {
			t.Fatal("a member's computer that did not answer was ignored")
		}
	})
	t.Run("stalled", func(t *testing.T) {
		s, f := newQuietService(t)
		s.todos.mu.Lock()
		s.todos.todos = map[string]*Todo{todo.ID: &todo}
		s.todos.activeBySession = map[[2]string]string{{"box", "work-0001"}: todo.ID, {"server", "sup-00001"}: todo.ID}
		s.todos.loaded = true
		s.todos.mu.Unlock()
		member := proto.Session{MachineID: "box", SessionID: "work-0001", State: proto.StateWorking, Status: proto.StatusWorking, PaneID: "p1", StartedByPersona: true,
			UpdatedAt: now.Add(-5 * time.Hour)}
		f.setSessions("box", member)
		s.closeStalledTask(context.Background(), todo, []proto.Session{member}, now)
		if st := todoNow(s, todo.ID).Supervision.state(todo); st != reviewClosing && st != reviewTimedOut {
			t.Fatalf("a member stalled past the close time was not closed: the ledger is %q", st)
		}
	})
}

// The supervisor Agent is linked a moment before the ledger moves to it: in between, its first turn is no work of the
// to-do, and its end is no turn of it to settle, so a review waiting for the owner's Agents is not undone.
func TestTheSupervisorAgentsFirstTurnBeforeTheLedgerMovesIsNotTheWorkersTurn(t *testing.T) {
	s, f, todo := memberRequest(t)
	ctx := context.Background()
	reviewWaits(t, s, todo.ID)
	if err := s.linkTodoAgentWithSource(ctx, todo.ID, "server", "sup-00001", "supervisor", "/home", "claude", "", sourceLeader, ""); err != nil {
		t.Fatal(err)
	}
	cur := todoNow(s, todo.ID)
	if cur.memberSupervised() || !cur.supervisorTurn("server", "sup-00001") || cur.supervisorRow(cur.Agents[0]) || !cur.supervisorRow(cur.Agents[1]) {
		t.Fatalf("the link and the ledger are not in step yet, and the rows are read: %+v", cur.Agents)
	}
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		proto.Session{SessionID: "sup-00001", MachineID: "server", Status: proto.StatusWorking, State: proto.StateWorking, UpdatedAt: time.Now()})
	s.taskWorking(ctx, "server", "sup-00001")
	if st := todoNow(s, todo.ID).Supervision.State; st != reviewPending {
		t.Fatalf("the supervisor's first turn reset the review: state %q", st)
	}
	if got := s.settleTurn(ctx, "server", "sup-00001", ""); got != settleRefused {
		t.Fatalf("the supervisor's turn end was settled as the worker's: %v", got)
	}
	if agents := s.taskAgents(ctx, cur); len(agents) != 1 || agents[0].SessionID != firstAgent {
		t.Fatalf("the to-do's Agents: %+v", agents)
	}
	// A supervisor Agent of a to-do with no Agent of its own working on it (a leader's own to-do) is its worker, as ever.
	lone := todoNow(s, todo.ID)
	lone.Agents = lone.Agents[1:]
	if lone.overMembers() || lone.supervisorTurn("server", "sup-00001") {
		t.Fatal("a leader's own to-do reads its leader as a supervisor of members")
	}
}

// The bound of workers one supervisor Agent has open at a time counts members: the Agents working on its to-do count as
// the pieces it hands out did.
func TestTheSupervisorBoundCountsMembers(t *testing.T) {
	s, _, todo, lead := supervisedRequest(t)
	ctx := context.Background()
	for i := range leaderWorkersMost - 2 { // with the first Agent, seven members
		if err := s.linkTodoAgentWithSource(ctx, todo.ID, "box", fmt.Sprintf("extra-%04d", i), "extra", "/code/app", "claude", "", "manual", ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(todoNow(s, todo.ID).workingMembers()); got != leaderWorkersMost-1 {
		t.Fatalf("%d members", got)
	}
	join := func(n int) proto.ToolResult {
		var tasks []map[string]any
		for i := range n {
			tasks = append(tasks, map[string]any{"title": fmt.Sprintf("part %d", i), "prompt": "do it", "role": "worker", "machine": "mac", "cwd": "/code/web",
				"readonly": true, "todo_id": todo.ID, "join": true})
		}
		raw, _ := json.Marshal(map[string]any{"tasks": tasks})
		return s.mcpCall(ctx, lead.MachineID, lead.SessionID, "task_plan", raw, false)
	}
	if res := join(2); !res.IsError || !strings.Contains(res.Content[0].Text, "at most 8 workers") || !strings.Contains(res.Content[0].Text, "(7 now)") {
		t.Fatalf("two more members: %+v", res)
	}
	if res := join(1); res.IsError {
		t.Fatalf("the eighth member: %+v", res)
	}
	waitFor(t, "the eighth member", func() bool { return len(todoNow(s, todo.ID).workingMembers()) == leaderWorkersMost })
	if res := join(1); !res.IsError || !strings.Contains(res.Content[0].Text, "at most 8 workers") {
		t.Fatalf("a ninth member: %+v", res)
	}
}

// One scenario end to end: two members on two computers work one to-do, each reports its part, the to-do waits for review
// once the last has finished, and one acceptance of its one ledger closes it.
func TestTwoMembersOnTwoComputersOneLedgerOneAcceptance(t *testing.T) {
	s, f, todo := memberRequest(t)
	ctx := context.Background()
	m := joinMember(t, s, f, todo, map[string]any{"title": "web side", "done_when": []string{"2"}})
	s.taskWorking(ctx, "box", firstAgent)
	s.taskWorking(ctx, "mac", m.SessionID)
	if got := len(s.todosOf(ctx, "*")); got != 1 {
		t.Fatalf("%d to-dos", got)
	}
	memberStates(s, f, map[string]string{firstAgent: proto.StateIdle})
	memberReport(t, s, "box", firstAgent, upReportDone, "the app side is done: PR merged")
	s.taskTurnEnded(ctx, "box", firstAgent)
	if st := todoNow(s, todo.ID).Supervision.State; st != "" {
		t.Fatalf("review asked with a member at work: %q", st)
	}
	memberStates(s, f, map[string]string{firstAgent: proto.StateIdle, m.SessionID: proto.StateIdle})
	memberReport(t, s, "mac", m.SessionID, upReportDone, "the web side is done: screenshots in the PR")
	s.taskTurnEnded(ctx, "mac", m.SessionID)
	waitFor(t, "the review", func() bool { return todoNow(s, todo.ID).Supervision.State == reviewPending })
	for _, a := range todoNow(s, todo.ID).workingMembers() {
		if a := a.Assignment; a == nil || a.State != membership.StateDone {
			t.Fatalf("a member's part is not done: %+v", a)
		}
	}
	raw, _ := json.Marshal(map[string]any{"todo_id": todo.ID, "verdict": "accepted", "note": "both sides checked", "checks": completeReviewChecks(todoNow(s, todo.ID))})
	res, err := s.runTool(ctx, "server", "conv1", "task_review", raw)
	if err != nil || !strings.Contains(res, "accepted and closed") {
		t.Fatalf("acceptance: %q %v", res, err)
	}
	done := todoNow(s, todo.ID)
	if done.DoneAt == nil || done.Supervision.State != reviewAccepted || len(done.Supervision.Reviews) != 1 || len(done.workingMembers()) != 2 {
		t.Fatalf("the closed to-do: %+v / %+v", done, done.Supervision)
	}
	if got := len(s.todosOf(ctx, "*")); got != 1 {
		t.Fatalf("%d to-dos at the end", got)
	}
}

// The estimate clock is per member: a member with an estimate of its own is due at it, and is moved later on its own when
// it made progress lately, else its supervisor hears it with the member's session; one with none keeps the to-do's.
func TestTheEstimateClockIsPerMember(t *testing.T) {
	s, f, todo := memberRequest(t)
	ctx := context.Background()
	holdDigests(s)
	m := joinMember(t, s, f, todo, map[string]any{"title": "web side", "expect_minutes": 10})
	back := func(d time.Duration) {
		s.todos.mu.Lock()
		for i := range s.todos.todos[todo.ID].Agents {
			if s.todos.todos[todo.ID].Agents[i].SessionID == m.SessionID {
				s.todos.todos[todo.ID].Agents[i].LinkedAt = time.Now().Add(-d)
			}
		}
		s.todos.mu.Unlock()
	}
	now := time.Now()
	back(5 * time.Minute)
	s.memberClocks(ctx, todoNow(s, todo.ID), now)
	if got := waitingSignalIDs(s); len(got) != 0 {
		t.Fatalf("a member not yet due: %v", got)
	}
	back(30 * time.Minute) // due, and nothing moved since it was linked
	s.memberClocks(ctx, todoNow(s, todo.ID), now)
	id := todo.ID + "|estimate_due#" + m.SessionID
	if got := waitingSignalIDs(s); !slices.Equal(got, []string{id}) {
		t.Fatalf("the member's estimate is due: %v, want only %s", got, id)
	}
	g := superviseSignal{Todo: todo.ID, Kind: sigEstimateDue, Machine: "mac", Session: m.SessionID, Key: memberDueKey(todoNow(s, todo.ID).workingMembers()[1]), Minutes: 30}
	e, ok := s.digestEntry(ctx, g, "")
	if !ok || e.AgentID != m.SessionID || !strings.Contains(e.Detail, "10") {
		t.Fatalf("its supervisor's entry: %v %+v", ok, e)
	}
	// It made progress lately: the estimate moves, nothing is raised.
	s.supervise.mu.Lock()
	clear(s.supervise.pending)
	s.supervise.mu.Unlock()
	s.progressReported("mac", m.SessionID, 1, 1, now)
	s.memberClocks(ctx, todoNow(s, todo.ID), now)
	a := todoNow(s, todo.ID).workingMembers()[1].Assignment
	if got := waitingSignalIDs(s); len(got) != 0 || a.ExpectMinutes != 20 || a.Extended != 1 {
		t.Fatalf("a member that made progress: %v %+v", got, a)
	}
	if _, ok := s.digestEntry(ctx, g, ""); ok {
		t.Fatal("the old estimate is still told after it moved")
	}
	// The first Agent has no estimate of its own: the to-do's clock is its clock.
	if todoNow(s, todo.ID).workingMembers()[0].OwnClock() {
		t.Fatal("the first Agent has a clock of its own")
	}
}

// A supervisor Agent addresses the members of a to-do it supervises by session: unnamed, a message to a to-do with
// several is refused with them; a named one reaches that member alone.
func TestASupervisorAgentNamesTheMemberItWritesTo(t *testing.T) {
	s, f, todo, lead := supervisedRequest(t)
	ctx := context.Background()
	m := joinMemberBySupervisor(t, s, lead, todo)
	listAutoAgents(s, f)
	send := func(extra map[string]any) proto.ToolResult {
		args := map[string]any{"todo_id": todo.ID, "text": "please rebase"}
		for k, v := range extra {
			args[k] = v
		}
		raw, _ := json.Marshal(args)
		return s.mcpCall(ctx, lead.MachineID, lead.SessionID, "agent_send", raw, false)
	}
	res := send(nil)
	if !res.IsError || !strings.Contains(res.Content[0].Text, "name the one it is for") || !strings.Contains(res.Content[0].Text, short(m.SessionID)+" (web side, working)") {
		t.Fatalf("unnamed: %+v", res)
	}
	read, _ := json.Marshal(map[string]any{"todo_id": todo.ID})
	if res := s.mcpCall(ctx, lead.MachineID, lead.SessionID, "agent_read", read, false); !res.IsError || !strings.Contains(res.Content[0].Text, "members: ") {
		t.Fatalf("an unnamed read: %+v", res)
	}
	if got := deliveredTo(f, m.SessionID); len(got) != 0 {
		t.Fatalf("something was sent: %q", got)
	}
	if res := send(map[string]any{"session_id": m.SessionID}); res.IsError {
		t.Fatalf("named: %+v", res)
	}
	waitFor(t, "the message", func() bool { return len(deliveredTo(f, m.SessionID)) == 1 })
	if got := deliveredTo(f, "s1"); len(got) != 0 {
		t.Fatalf("the other member was told: %q", got)
	}
}

// joinMemberBySupervisor has the supervisor Agent join one member ("web side" on mac) and waits for it.
func joinMemberBySupervisor(t *testing.T, s *Service, lead TodoAgent, todo Todo) TodoAgent {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{"title": "web side", "prompt": "do the web side", "role": "worker", "machine": "mac",
		"cwd": "/code/web", "readonly": true, "todo_id": todo.ID, "join": true}}})
	if res := s.mcpCall(context.Background(), lead.MachineID, lead.SessionID, "task_plan", raw, false); res.IsError {
		t.Fatalf("join: %+v", res)
	}
	waitFor(t, "the member", func() bool {
		return len(todoNow(s, todo.ID).workingMembers()) == 2 && todoNow(s, todo.ID).workingMembers()[1].Assignment != nil
	})
	cur := todoNow(s, todo.ID)
	return cur.Agents[len(cur.Agents)-1]
}

// Every kind of supervision signal has a scope: a new kind has to say whether it is about one Agent's session or the to-do.
func TestEverySignalKindHasAScope(t *testing.T) {
	kinds := taskleader.Kinds
	if len(kinds) != len(membership.Kinds()) {
		t.Fatalf("%d kinds are routed, %d have a scope", len(kinds), len(membership.Kinds()))
	}
	for _, kind := range kinds {
		if _, ok := membership.ScopeOf(kind); !ok {
			t.Errorf("signal kind %s has no scope in membership", kind)
		}
	}
	// The kinds a signal names a session for are the ones that are session-bound where they are raised.
	for _, kind := range []string{sigReport, sigNeedsYou, sigStalled, sigNoProgress, sigInterrupted, sigAsk, sigIdle, sigEstimateDue} {
		if !membership.SessionBound(kind) {
			t.Errorf("%s is about one Agent's session", kind)
		}
	}
}

// The assignment is optional in the contract: a row with none reads as before, one with a part carries it.
func TestTheAssignmentIsAnOptionalFieldOfTheAgentRow(t *testing.T) {
	plain, _ := json.Marshal(TodoAgent{MachineID: "box", SessionID: "s1"})
	if strings.Contains(string(plain), "assignment") {
		t.Fatalf("a row with no part: %s", plain)
	}
	with, _ := json.Marshal(TodoAgent{MachineID: "mac", SessionID: "s2", Assignment: &membership.Assignment{Title: "web", Criteria: []string{"a"}, ExpectMinutes: 20, State: membership.StateWorking}})
	if !strings.Contains(string(with), `"assignment":{"title":"web","criteria":["a"],"expect_minutes":20,"state":"working"}`) {
		t.Fatalf("a row with a part: %s", with)
	}
	var back TodoAgent
	if err := json.Unmarshal([]byte(`{"machine_id":"m","session_id":"s","future":1}`), &back); err != nil || back.Assignment != nil {
		t.Fatalf("an old row: %+v %v", back, err)
	}
}
