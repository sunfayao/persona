package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// #7539, the explicit reopens of an accepted task: whoever may ask it for more (its conversation, its supervisor, the
// background supervisor acting for the main conversation) opens it again with what it sends the task's agent, and that
// holds across a manager restart between the send and the turn it queued, also one before the send's answer came back.
// Everyone else's sends (the background supervisor's for a task a side conversation supervises included), a bare /exit
// or /quit, a card message for another task, a send that definitely failed or never went out (even when a turn of the
// agent started while it was out), one whose answer never came with no turn of the agent soon after, and a card
// message whose new agent did not start leave it closed.

// longAgent is the session id of the agent a plannedTask runs on: long enough for the agent tools to find it.
const longAgent = "s100001"

// plannedTask is newSupervisedServiceOn with its agent named longAgent (on box), st as its database (nil: none).
func plannedTask(t *testing.T, st Store) (*Service, *fakeHub, Todo) {
	t.Helper()
	s, f := newQuietServiceOn(t, st)
	f.mu.Lock()
	f.judge = `{"verdict":"done","reason":"merged"}`
	f.agents = 100000
	f.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{
		"title": "Fix the login crash", "prompt": "The login page crashes on an iPad in landscape: fix it and add tests",
		"machine": "box", "cwd": "/code/app", "readonly": true, "why": "the repository is on box",
		"done_when": []string{"PR merged", "light and dark screenshots in the PR"}, "expect_minutes": 30,
	}}})
	if _, err := s.toolTaskPlan(context.Background(), "conv1", raw); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { return len(onlyTodo(s).Agents) == 1 })
	todo := onlyTodo(s)
	if todo.Agents[0].SessionID != longAgent || todo.PlanID == "" || todo.Supervision == nil || todo.Supervision.Supervisor != "conv1" {
		t.Fatalf("not a plan task of conv1 on %s: %+v %+v", longAgent, todo, todo.Supervision)
	}
	return s, f, todo
}

// longRow lists the agent longAgent on box with status and its last words text, as its node does.
func longRow(f *fakeHub, status, text string) {
	st := proto.StateUnread
	if status == proto.StatusWorking {
		st = proto.StateWorking
	}
	f.setSessions("box", proto.Session{SessionID: longAgent, MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: status,
		State: st, LastText: text, UpdatedAt: time.Now()})
}

// longTurn is agentTurn for the agent longAgent.
func longTurn(s *Service, f *fakeHub, status, text string) {
	ctx := context.Background()
	longRow(f, status, text)
	if status == proto.StatusWorking {
		s.taskWorking(ctx, "box", longAgent)
	} else {
		s.taskTurnEnded(ctx, "box", longAgent)
	}
}

// sendTo sends agent longAgent on box text from conversation from, with tool (agent_send, or agent_interrupt with
// then).
func sendTo(t *testing.T, s *Service, tool, from, text string) error {
	t.Helper()
	args := map[string]any{"machine": "box", "session_id": longAgent, "text": text}
	if tool == "agent_interrupt" {
		args = map[string]any{"machine": "box", "session_id": longAgent, "then": text}
	}
	_, err := runJSON(t, s, from, tool, args)
	return err
}

// acceptedPlanTask is plannedTask (st its database, nil: none) with its agent's work done and accepted by conv1, the
// clock past the acceptance.
func acceptedPlanTask(t *testing.T, st Store) (*Service, *fakeHub, Todo) {
	t.Helper()
	s, f, todo := plannedTask(t, st)
	longTurn(s, f, proto.StatusWorking, "")
	longTurn(s, f, proto.StatusDone, "Fixed it: PR https://github.com/o/r/pull/77 is merged")
	waitFor(t, "the review message", func() bool { return supervisionOf(s, f, todo.ID).notices == 1 })
	accepted := reviewAndWait(t, s, todo.ID, reviewAccepted)
	waitFor(t, "the clock to pass the acceptance", func() bool { return time.Now().After(*accepted.DoneAt) })
	return s, f, accepted
}

// acceptedByBackground is plannedTask with a real pre-#5732 notice delivered to an existing legacy background
// supervisor bg. That supervisor accepts the old task for the main conversation (the ledger still names conv1), and
// the clock moves past the acceptance. No replacement background conversation is created.
func acceptedByBackground(t *testing.T) (s *Service, f *fakeHub, accepted Todo, bg string) {
	t.Helper()
	s, f, todo := plannedTask(t, nil)
	s.noBackground = false
	bg = "bg1"
	f.known[bg] = true
	f.setSessions("server",
		proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		proto.Session{SessionID: bg, MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	setSettings(t, s, func(v *Settings) { v.BackgroundSupervisor, v.BackgroundSupervisorMachineID = bg, "server" })
	longTurn(s, f, proto.StatusWorking, "")
	longTurn(s, f, proto.StatusDone, "Fixed it: PR https://github.com/o/r/pull/77 is merged")
	tellLegacyBackground(t, s, f, todo.ID, bg)
	if _, err := runJSON(t, s, bg, "task_review", map[string]any{"todo_id": todo.ID, "verdict": reviewAccepted,
		"note": "Checked the PR and the screenshots", "checks": completeReviewChecks(todoNow(s, todo.ID))}); err != nil {
		t.Fatalf("the legacy background supervisor's accept: %v", err)
	}
	settleService(t, s)
	accepted = todoNow(s, todo.ID)
	if accepted.DoneAt == nil || accepted.DoneBy != db.DoneByReview || accepted.Supervision.Supervisor != "conv1" {
		t.Fatalf("not accepted for the main conversation: %+v %+v", accepted, accepted.Supervision)
	}
	waitFor(t, "the clock to pass the acceptance", func() bool { return time.Now().After(*accepted.DoneAt) })
	return s, f, accepted, bg
}

// beforeSend has f run fn, once, when the manager next calls session.send, before the node answers.
func beforeSend(f *fakeHub, fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beforeSend = fn
}

// failSends has every session.send f gets next fail with err (nil: answered).
func failSends(f *fakeHub, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failSend = err
}

// The node's errors for a send, as it words them (node/sendsafe.go, node/claudebg.go): a Claude's inbox read the text
// but the session had not taken it in time (held for the person at the computer, say), and typing that failed part way,
// both delivered unknown; a refusal with nothing typed, delivered none.
var (
	nodeUnconfirmed = &rpc.Error{Code: "peer_unconfirmed", Message: "the session has not taken the message yet",
		Data: json.RawMessage(`{"delivered":"unknown"}`)}
	nodeTypingFailed = &rpc.Error{Message: "the pane closed while typing", Data: json.RawMessage(`{"delivered":"unknown"}`)}
	relayedDeadline  = &rpc.Error{Message: context.DeadlineExceeded.Error()} // the bus only carries the words
	nodeAfterWrite   = &rpc.Error{Code: "internal", Message: "could not save the receipt", Data: json.RawMessage(`{"delivered":"yes"}`)}
	nodeRefused      = &rpc.Error{Code: "not_delivered", Message: "the terminal took nothing", Data: json.RawMessage(`{"delivered":"none"}`)}
)

// The node acknowledged its write but returned a receipt of the wrong shape.
func unreadableWriteReceipt(string, json.RawMessage) (any, error) { return 42, nil }

// sessionSends counts the session.send calls f got, answered or not.
func sessionSends(f *fakeHub) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == DeviceSessionSend {
			n++
		}
	}
	return n
}

// The background supervisor stands in for the main conversation only. A task a side conversation supervises sends that
// conversation its review signals; once accepted, the background supervisor's agent_send or agent_interrupt to its
// agent, and the agent's turn after it, leave it closed.
func TestBackgroundSupervisorLeavesASideConversationsTaskClosed(t *testing.T) {
	for _, tool := range []string{"agent_send", "agent_interrupt"} {
		t.Run(tool, func(t *testing.T) {
			s, f, todo, bg := acceptedByBackground(t)
			side := todo
			ledger := *todo.Supervision
			side.Origin, ledger.Supervisor, side.Supervision = "side1", "side1", &ledger
			putTodo(s, side)
			if !s.backgroundSupervises(context.Background(), bg, side) {
				t.Fatal("the background supervisor may not review the side conversation's task")
			}
			if err := sendTo(t, s, tool, bg, "Add the landscape case to the tests"); err != nil {
				t.Fatalf("%s from the background supervisor: %v", tool, err)
			}
			closedUnchanged(t, s, side)
			longTurn(s, f, proto.StatusWorking, "")
			closedUnchanged(t, s, side)
		})
	}
}

// The owner's message on the card of an accepted task whose agents have all stopped starts a new agent for it. When
// that start definitely fails (the agent's computer is unreachable, its launcher is refused, the start never went out),
// nothing was asked of the task: it stays exactly as accepted, and its supervisor hears nothing of it.
func TestACardMessageWhoseNewAgentFailsLeavesTheTaskClosed(t *testing.T) {
	for _, why := range []string{"computer unreachable", "launcher refused", "start never went out"} {
		t.Run(why, func(t *testing.T) {
			ctx := context.Background()
			s, f, todo := doneAndReviewed(t)
			accepted := reviewAndWait(t, s, todo.ID, reviewAccepted)
			before := supervisionOf(s, f, todo.ID)
			f.setSessions("box", proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude",
				LastText: "Fixed it: PR https://github.com/o/r/pull/77 is merged", Status: proto.StatusOffline, State: proto.StateOffline,
				UpdatedAt: time.Now(), Capabilities: &proto.SessionCapabilities{Read: true, Terminal: true}, ReadOnlyReason: "stopped"})
			f.mu.Lock()
			switch why {
			case "computer unreachable":
				f.devices = []Device{{ID: "box", Name: "box", Online: true, Reachable: false}}
			case "launcher refused":
				f.failStart = errors.New("launcher not found")
			default: // the node refused it as restarting until the manager's retries ran out (devices.Machine.call)
				f.failStart = rpc.NotSent(context.DeadlineExceeded)
			}
			f.mu.Unlock()
			if _, err := s.routeToTask(ctx, "conv1", todo.ID, "Please cover the landscape case too"); err == nil {
				t.Fatal("the new agent started")
			}
			closedUnchanged(t, s, accepted)
			nothingToldAfter(t, s, f, todo.ID, before)
		})
	}
}

// A message on an accepted task's card reopens that task only. Its agent is handed to another task of the same
// conversation, accepted too, while the card's message looks its agents up: the message goes to the agent as before,
// and neither task opens again, not even on the agent's next turn.
func TestACardMessageReopensOnlyItsOwnTask(t *testing.T) {
	ctx := context.Background()
	s, f, accepted := acceptedPlanTask(t, nil)
	now := time.Now()
	putTodo(s, Todo{ID: "todo_b", Title: "Tidy the release notes", Origin: "conv1", CreatedAt: now, UpdatedAt: now,
		Supervision: &Supervision{Supervisor: "conv1", State: reviewAccepted, Reviews: []TaskReview{{At: now, Verdict: reviewAccepted}}}})
	var other Todo
	f.mu.Lock()
	f.afterList = map[string]func(){"box": func() { // the agents of the card's task were just read
		if err := s.linkTodoAgent(ctx, "todo_b", "box", longAgent, "", "/code/app", "claude"); err != nil {
			t.Errorf("hand the agent to the other task: %v", err)
			return
		}
		done := time.Now()
		s.applyTodoRow(ctx, "todo_b", db.TodoRow{ID: "todo_b", Title: "Tidy the release notes", Origin: "conv1", CreatedAt: now,
			UpdatedAt: done, DoneAt: &done, DoneBy: db.DoneByReview}, nil)
		other = todoNow(s, "todo_b")
	}}
	f.mu.Unlock()
	// Only the card message's own look at the agents runs the hook: the token sync the acceptance started may still be
	// listing box.
	if _, err := s.routeToTask(listHooked(ctx), "conv1", accepted.ID, "Please cover the landscape case too"); err != nil {
		t.Fatalf("the card message: %v", err)
	}
	if other.ID == "" || other.DoneAt == nil || other.DoneBy != db.DoneByReview || !slices.ContainsFunc(other.Agents, func(a TodoAgent) bool {
		return a.SessionID == longAgent && a.ReleasedAt == nil
	}) {
		t.Fatalf("the other task did not take the agent and close: %+v", other)
	}
	closedUnchanged(t, s, other)
	if got := todoNow(s, accepted.ID); got.DoneAt == nil {
		t.Fatalf("the card's task opened again without its agent: %+v", got)
	}
	longTurn(s, f, proto.StatusWorking, "")
	closedUnchanged(t, s, other)
}

// A bare /exit or /quit on the card of an accepted task whose agents have all stopped asks the task for nothing, and
// what it would close is closed already: no new agent starts, the card says so, and the task stays exactly as accepted,
// its supervisor told nothing.
func TestAQuitCommandToAStoppedTaskStartsNothing(t *testing.T) {
	for _, text := range []string{"/exit", " /quit\n"} {
		t.Run(strings.TrimSpace(text), func(t *testing.T) {
			ctx := context.Background()
			s, f, todo := doneAndReviewed(t)
			accepted := reviewAndWait(t, s, todo.ID, reviewAccepted)
			before := supervisionOf(s, f, todo.ID)
			f.setSessions("box", proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude",
				LastText: "Fixed it: PR https://github.com/o/r/pull/77 is merged", Status: proto.StatusOffline, State: proto.StateOffline,
				UpdatedAt: time.Now(), Capabilities: &proto.SessionCapabilities{Read: true, Terminal: true}, ReadOnlyReason: "stopped"})
			starts := func() int {
				f.mu.Lock()
				defer f.mu.Unlock()
				n := 0
				for _, c := range f.calls {
					if c.Method == DeviceSessionStart {
						n++
					}
				}
				return n
			}
			started := starts()
			_, err := s.routeToTask(ctx, "conv1", todo.ID, text)
			if msg := (*i18n.Message)(nil); !errors.As(err, &msg) || msg.Key != "assistant-tasks.route_agents_exited" {
				t.Fatalf("%q on the card of a task whose agents stopped: %v, want route_agents_exited", text, err)
			}
			if n := starts() - started; n != 0 {
				t.Fatalf("%q started %d new agents", text, n)
			}
			closedUnchanged(t, s, accepted)
			nothingToldAfter(t, s, f, todo.ID, before)
		})
	}
}
