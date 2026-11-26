package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func a2aProcessOne(t *testing.T, s *Domain, st *testStore, cfg assistantwire.A2AConfig, id string) *Task {
	t.Helper()
	ctx := t.Context()
	task, err := DecodeTask(st.A2AGet(ctx, id))
	if err != nil || task == nil {
		t.Fatalf("get task: %v %v", task, err)
	}
	if err := s.process(ctx, st, cfg, task); err != nil {
		t.Fatal(err)
	}
	task, err = DecodeTask(st.A2AGet(ctx, id))
	if err != nil || task == nil {
		t.Fatalf("get processed task: %v %v", task, err)
	}
	return task
}

func TestA2ACrossHarnessTaskAndWakeAfterRestart(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, child := a2aFixture(t)
	ctx := t.Context()
	tk, err := s.a2aStart(ctx, st, cfg, parent, child, "task-request", "", "Check this build.", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.a2aStart(ctx, st, cfg, parent, child, "task-request", "", "Check this build.", true, nil)
	if err != nil || again.ID != tk.ID {
		t.Fatalf("retry %v %v", again, err)
	}
	if _, err := s.a2aStart(ctx, st, cfg, parent, child, "task-request", "", "Different text.", true, nil); !errors.Is(err, errA2ADuplicate) {
		t.Fatalf("same request with different text: %v", err)
	}
	queued, err := s.a2aStart(ctx, st, cfg, parent, child, "second-task", "", "Other text.", true, nil)
	if err != nil || queued.State != a2aQueued {
		t.Fatalf("concurrent task to same target should be queued: %+v %v", queued, err)
	}
	if _, err := s.a2aMutate(ctx, st, cfg, parent, queued.ID, s.a2aCancel); err != nil {
		t.Fatalf("cancel queued task: %v", err)
	}
	tk = a2aProcessOne(t, s, st, cfg, tk.ID)
	if tk.State != a2aWorking || len(h.sends) != 1 || h.sends[0].Device != child.Machine {
		t.Fatalf("child delivery state %s sends %+v", tk.State, h.sends)
	}
	worker := SessionCaller(child)
	tk, err = s.a2aMutate(ctx, st, cfg, worker, tk.ID, func(tk *Task) (bool, error) {
		return s.a2aReport(tk, child, "blocked-report", a2aInput, "Which target should I use?")
	})
	if err != nil || tk.State != a2aInput {
		t.Fatalf("blocked report: %v %v", tk, err)
	}
	restarted := h.domain(t, st)
	a2aProcessOne(t, restarted, st, cfg, tk.ID)
	if len(h.sends) != 1 {
		t.Fatal("busy parent was interrupted")
	}
	setStatus(h, parent.Ref.Machine, parent.Ref.Session, proto.StatusDone)
	tk = a2aProcessOne(t, restarted, st, cfg, tk.ID)
	if len(h.sends) != 2 || h.sends[1].Device != parent.Ref.Machine ||
		!strings.Contains(string(h.sends[1].Params), "Which target should I use?") {
		t.Fatalf("parent did not receive the durable blocker: %+v", h.sends)
	}
	a2aProcessOne(t, restarted, st, cfg, tk.ID)
	if len(h.sends) != 2 {
		t.Fatal("a delivered blocker replayed")
	}
	tk, err = restarted.a2aMutate(ctx, st, cfg, parent, tk.ID, func(tk *Task) (bool, error) {
		return restarted.a2aContinue(tk, "answer", "Use the integration target.")
	})
	if err != nil {
		t.Fatal(err)
	}
	tk = a2aProcessOne(t, restarted, st, cfg, tk.ID)
	seq := tk.Events[len(tk.Events)-2].Seq
	h.setItems(proto.Item{Role: "user", ClientMsgID: a2aDeliveryID(tk.ID, seq)}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: "Integration target passed."})
	setStatus(h, child.Machine, child.Session, proto.StatusDone)
	tk = a2aProcessOne(t, restarted, st, cfg, tk.ID)
	if tk.State != a2aComplete || len(tk.wire(nil, true).Artifacts) != 1 {
		t.Fatalf("outcome: state=%s events=%+v", tk.State, tk.Events)
	}
	tk = a2aProcessOne(t, restarted, st, cfg, tk.ID)
	if len(h.sends) != 4 || h.sends[3].Device != parent.Ref.Machine || !strings.Contains(string(h.sends[3].Params), "Integration target passed.") {
		t.Fatalf("parent did not receive final artifact: %+v", h.sends)
	}
	if _, err := restarted.a2aGet(ctx, st, cfg, SessionCaller(Ref{"mac", "unrelated"}), tk.ID); !errors.Is(err, errA2ANotFound) {
		t.Fatalf("unrelated session read a task: %v", err)
	}
}

func TestA2AAmbiguousSendIsReconciledWithoutReplay(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, child := a2aFixture(t)
	tk, err := s.a2aStart(t.Context(), st, cfg, parent, child, "task-request", "", "Check this build.", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.sendErr = context.DeadlineExceeded
	a2aProcessOne(t, s, st, cfg, tk.ID)
	tk = a2aProcessOne(t, s, st, cfg, tk.ID)
	if len(h.sends) != 1 || tk.Events[0].Delivery != a2aUncertain {
		t.Fatalf("uncertain send retried: %+v", tk.Events)
	}
	h.clock.now = h.clock.Now().Add(a2aConfirmFor + time.Second)
	marked := prompts.RelayMessageText("A2A request", nil, a2aDeliveryID(tk.ID, 1))
	h.setItems(proto.Item{Role: "user", Text: marked}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: "Result."},
		proto.Item{Role: "user", Text: "An unrelated new prompt."}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: "Unrelated."})
	tk = a2aProcessOne(t, s, st, cfg, tk.ID)
	if len(h.sends) != 1 || tk.State != a2aWorking || tk.Events[0].Delivery != a2aDelivered {
		t.Fatalf("late delivery was not confirmed: %+v", tk.Events)
	}
	tk = a2aProcessOne(t, s, st, cfg, tk.ID)
	if len(h.sends) != 1 || tk.State != a2aFailed {
		t.Fatalf("unrelated turn was accepted as a result or the input was replayed: %+v", tk.Events)
	}
}

func TestA2ADeletedRecipientReleasesTask(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, child := a2aFixture(t)
	ctx := t.Context()
	task, err := s.a2aStart(ctx, st, cfg, parent, child, "request", "", "Check the build.", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.deleted = child
	if _, err := s.a2aSession(ctx, cfg, child); !errors.Is(err, errA2AUnavailable) {
		t.Fatalf("deleted recipient remained eligible: %v", err)
	}
	task = a2aProcessOne(t, s, st, cfg, task.ID)
	if task.State != a2aFailed || !task.TargetClosed || len(h.sends) != 0 {
		t.Fatalf("deleted recipient left an active or delivered task: %+v", task)
	}
}

func TestA2AUnconfirmedInputReleasesTheTarget(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, child := a2aFixture(t)
	ctx := t.Context()
	task, err := s.a2aStart(ctx, st, cfg, parent, child, "initial", "", "Inspect the build.", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.sendErr = errors.New("lost send reply")
	task = a2aProcessOne(t, s, st, cfg, task.ID)
	if task.Events[0].Delivery != a2aUncertain {
		t.Fatalf("delivery should remain uncertain: %+v", task.Events[0])
	}
	h.clock.now = h.clock.Now().Add(a2aConfirmFor + time.Second)
	task = a2aProcessOne(t, s, st, cfg, task.ID)
	if task.State != a2aFailed || task.Events[0].Delivery != a2aExpired || len(h.sends) != 1 {
		t.Fatalf("expired task was replayed or still holds the target: %+v (%d sends)", task.Events, len(h.sends))
	}
	if _, err := s.a2aStart(ctx, st, cfg, parent, child, "next", "", "Inspect the next build.", true, nil); err != nil {
		t.Fatalf("expired task did not release the target: %v", err)
	}
}

func TestA2ATextOnlyHarnessReturnsQuestionAndContinues(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, child := a2aFixture(t)
	ctx := t.Context()
	task, err := s.a2aStart(ctx, st, cfg, parent, child, "text-task", "", "Check the build.", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	a2aProcessOne(t, s, st, cfg, task.ID)
	question := "A2A " + task.ID + " input_required\nWhich build target?"
	h.setItems(proto.Item{Role: "user", ClientMsgID: "different-message"}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: question})
	if task = a2aProcessOne(t, s, st, cfg, task.ID); task.State != a2aWorking {
		t.Fatalf("unrelated turn changed the task: %s", task.State)
	}
	h.setItems(proto.Item{Role: "user", Text: prompts.RelayMessageText("Check the build.", nil, a2aDeliveryID(task.ID, 1))},
		proto.Item{Role: "user", Kind: proto.KindToolResult, Text: "The tool output appeared inside the same turn."},
		proto.Item{Role: "assistant", Kind: proto.KindText, Text: question}, proto.Item{Kind: "turn_duration", ID: "closed-turn"},
		proto.Item{Role: "user", ClientMsgID: "different-message"})
	task = a2aProcessOne(t, s, st, cfg, task.ID)
	if last := task.Events[len(task.Events)-1]; task.State != a2aInput || last.Text != "Which build target?" || last.Delivery != a2aQueued {
		t.Fatalf("finished question was not returned: %+v", last)
	}
	h.sessions[parent.Ref.Machine][0].Status = proto.StatusOffline
	h.sessions[parent.Ref.Machine][0].SourceKind = proto.SourceRuntime
	h.sessions[parent.Ref.Machine][0].Capabilities = &proto.SessionCapabilities{Send: true, A2AWakeOnSend: true}
	a2aProcessOne(t, s, st, cfg, task.ID)
	var wake struct {
		Wake bool `json:"a2a_wake_on_send"`
	}
	if len(h.sends) != 2 || h.sends[1].Device != parent.Ref.Machine || json.Unmarshal(h.sends[1].Params, &wake) != nil || !wake.Wake {
		t.Fatalf("the offline runtime parent did not receive its update: %+v", h.sends)
	}
	questionSeq := task.Events[len(task.Events)-1].Seq
	h.sessions[parent.Ref.Machine][0].Status = proto.StatusIdle
	h.setItems(proto.Item{Role: "user", ClientMsgID: a2aDeliveryID(task.ID, questionSeq)},
		proto.Item{Role: "assistant", Kind: proto.KindText, Text: "A2A unrelated-task answer\nThis belongs to another task."})
	if task = a2aProcessOne(t, s, st, cfg, task.ID); task.State != a2aInput {
		t.Fatalf("another task's answer changed the task: %s", task.State)
	}
	h.setItems(proto.Item{Role: "user", ClientMsgID: a2aDeliveryID(task.ID, questionSeq)},
		proto.Item{Role: "assistant", Kind: proto.KindText, Text: "A2A " + task.ID + " answer\nUse the integration target."})
	h.sendRefusal = errors.New("dispatch guard refused conversation_send")
	if task = a2aProcessOne(t, s, st, cfg, task.ID); task.State != a2aInput {
		t.Fatalf("a text reply bypassed the dispatch guard: %s", task.State)
	}
	args, _ := json.Marshal(map[string]string{"task_id": task.ID, "message_id": "guarded-answer", "text": "Use the integration target."})
	if _, err := s.a2aToolCall(ctx, st, cfg, parent, "a2a_send", args); err == nil {
		t.Fatal("a follow-up tool call bypassed the dispatch guard")
	}
	h.sendRefusal = nil
	if task = a2aProcessOne(t, s, st, cfg, task.ID); task.State != a2aSubmitted {
		t.Fatalf("text answer was not accepted: %s", task.State)
	}
	task = a2aProcessOne(t, s, st, cfg, task.ID)
	h.setItems(proto.Item{Role: "user", ClientMsgID: a2aDeliveryID(task.ID, len(task.Events)-1)},
		proto.Item{Role: "assistant", Kind: proto.KindText, Text: "The integration target passed."})
	task = a2aProcessOne(t, s, st, cfg, task.ID)
	if last := task.Events[len(task.Events)-1]; task.State != a2aComplete || last.Text != "The integration target passed." {
		t.Fatalf("continued task did not finish: %+v", last)
	}
}

func TestA2AStoreClaimsPersist(t *testing.T) {
	t.Parallel()
	s, _, st, cfg, parent, child := a2aFixture(t)
	tk, err := s.a2aStart(t.Context(), st, cfg, parent, child, "task-request", "", "Work.", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.A2AClaim(t.Context(), 1)
	if err != nil || len(rows) != 1 || rows[0].ID != tk.ID || time.Since(rows[0].Created) > time.Minute {
		t.Fatalf("claim %+v %v", rows, err)
	}
	rows, err = st.A2AClaim(t.Context(), 1)
	if err != nil || len(rows) != 0 {
		t.Fatalf("duplicate lease %+v %v", rows, err)
	}
}

func TestA2AExplicitEmptyScopeSurvivesJSON(t *testing.T) {
	t.Parallel()
	cfg := assistantwire.A2AConfig{Machines: []string{}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded assistantwire.A2AConfig
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Machines == nil || len(decoded.Machines) != 0 {
		t.Fatalf("empty scope reverted to defaults: %s %v", raw, err)
	}
}

func TestA2ARevokedCredentialCannotDeliverQueuedInput(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, _, child := a2aFixture(t)
	task, err := s.a2aStart(t.Context(), st, cfg, Caller{Owner: "device:remote"}, child, "request", "", "Queued work.", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.revokedDevices = map[string]bool{"remote": true}
	h.mu.Unlock()
	task = a2aProcessOne(t, s, st, cfg, task.ID)
	if len(h.sends) != 0 || task.State != a2aCanceled {
		t.Fatalf("state %s after credential revocation, sends %v", task.State, h.sends)
	}
}

func TestA2AWakesAnIdleStoppedParentAndLeavesAUserStopAlone(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, child := a2aFixture(t)
	h.wakes = make(chan assistantwire.DeviceCall, 1)
	ctx := t.Context()
	task, err := s.a2aStart(ctx, st, cfg, parent, child, "wake-task", "", "Check the integration.", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	a2aProcessOne(t, s, st, cfg, task.ID)
	task, err = s.a2aMutate(ctx, st, cfg, SessionCaller(child), task.ID, func(task *Task) (bool, error) {
		return s.a2aReport(task, child, "done", a2aComplete, "Integration passed.")
	})
	if err != nil {
		t.Fatal(err)
	}
	setStop := func(cause proto.StopCause) {
		h.mu.Lock()
		h.sessions[parent.Ref.Machine][0].Status = proto.StatusOffline
		h.sessions[parent.Ref.Machine][0].Capabilities = &proto.SessionCapabilities{StopCause: cause}
		h.mu.Unlock()
	}
	setStop(proto.StopUser)
	a2aProcessOne(t, s, st, cfg, task.ID)
	select {
	case <-h.wakes:
		t.Fatal("a user-stopped session was resumed")
	default:
	}
	setStop(proto.StopIdle)
	a2aProcessOne(t, s, st, cfg, task.ID)
	select {
	case call := <-h.wakes:
		if call.Device != parent.Ref.Machine || !strings.Contains(string(call.Params), "Integration passed.") {
			t.Fatalf("resume call: %+v", call)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("parent was not resumed")
	}
	st.waitForUpdate(t, "the durable A2A wake receipt", func() bool {
		task, err := DecodeTask(st.A2AGet(ctx, task.ID))
		if err != nil {
			t.Fatal(err)
		}
		return task.Events[len(task.Events)-1].Delivery == a2aDelivered
	})
}

func TestA2AStandardStreamCarriesTaskAndArtifact(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, _, child := a2aFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(rpcHandler(s).ServeHTTP))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	path := srv.URL + "/api/v1/a2a/" + child.Machine + "/" + child.Session
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path+"/rpc", strings.NewReader(`{
		"jsonrpc":"2.0","id":"stream","method":"SendStreamingMessage","params":{"message":{
			"messageId":"remote-message","role":"ROLE_USER","parts":[{"text":"Hello."}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("A2A-Version", "1.0")
	req.Header.Set(extwire.HeaderDevice, "first")
	req.Header.Set(extwire.HeaderRole, "full")
	response, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected stream response: %v", response)
	}
	scanner := bufio.NewScanner(response.Body)
	var id string
	artifact, complete := false, false
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var frame struct {
			ID     string `json:"id"`
			Result struct {
				Task           *a2aWireTask                    `json:"task"`
				ArtifactUpdate *struct{ Artifact a2aArtifact } `json:"artifactUpdate"`
				StatusUpdate   *struct{ Status a2aStatus }     `json:"statusUpdate"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil || frame.ID != "stream" {
			t.Fatalf("stream frame: %s, %v", data, err)
		}
		if frame.Result.Task != nil {
			id = frame.Result.Task.ID
			a2aProcessOne(t, s, st, cfg, id)
			h.setItems(proto.Item{Role: "user", ClientMsgID: a2aDeliveryID(id, 1)}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: "Done."})
			a2aProcessOne(t, s, st, cfg, id)
			h.clock.ticks <- h.clock.Now()
		}
		if a := frame.Result.ArtifactUpdate; a != nil {
			artifact = len(a.Artifact.Parts) == 1 && *a.Artifact.Parts[0].Text == "Done."
		}
		if status := frame.Result.StatusUpdate; status != nil {
			complete = complete || status.Status.State == "TASK_STATE_COMPLETED"
		}
	}
	if err := scanner.Err(); err != nil || id == "" || !artifact || !complete {
		t.Fatalf("stream id=%q artifact=%t complete=%t err=%v", id, artifact, complete, err)
	}
}

func TestA2AAcceptsOneLaunchPerMessageID(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, _ := a2aFixture(t)
	start := func() (*Task, error) {
		return s.a2aStart(t.Context(), st, cfg, parent, Ref{Machine: "box"}, "launch", "", "Inspect the build.", true,
			&Launch{CWD: "/code/project", Pending: true})
	}
	task, err := start()
	if err != nil {
		t.Fatal(err)
	}
	a2aProcessOne(t, s, st, cfg, task.ID)
	st.waitForUpdate(t, "the A2A launch result", func() bool {
		task, err = DecodeTask(st.A2AGet(t.Context(), task.ID))
		if err != nil {
			t.Fatal(err)
		}
		return !task.Launch.Pending
	})
	if task.Launch.Pending || task.Events[0].Delivery != a2aUncertain || len(h.starts) != 1 ||
		h.starts[0]["permission_mode"] != "plan" {
		t.Fatalf("launch: %+v; starts %+v", task, h.starts)
	}
	retry, err := start()
	if err != nil || retry.ID != task.ID || retry.TargetSession != task.TargetSession {
		t.Fatalf("launch retry: %+v %v", retry, err)
	}
}

func TestA2AQueuedSendsToBusySupervisorDrainInFIFOOrder(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, _, _ := a2aFixture(t)
	ctx := t.Context()
	supervisor := Ref{Machine: "mac", Session: "supervisor-0001"}
	w0 := Ref{Machine: "box", Session: "runtime.worker-0"}
	w1 := Ref{Machine: "box", Session: "runtime.worker-1"}
	w2 := Ref{Machine: "box", Session: "runtime.worker-2"}
	h.mu.Lock()
	h.sessions["mac"] = []proto.Session{{SessionID: supervisor.Session, Status: proto.StatusIdle, Agent: "codex"}}
	h.sessions["box"] = []proto.Session{
		{SessionID: w0.Session, Status: proto.StatusWorking, Agent: "pi", SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}},
		{SessionID: w1.Session, Status: proto.StatusWorking, Agent: "pi", SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}},
		{SessionID: w2.Session, Status: proto.StatusWorking, Agent: "pi", SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}},
	}
	h.mu.Unlock()

	callSend := func(from Ref, msgID, text string) a2aTaskView {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{
			"machine_id": supervisor.Machine, "session_id": supervisor.Session,
			"message_id": msgID, "text": text, "wake": false,
		})
		res := s.Tool(ctx, from, "a2a_send", raw)
		if res.IsError {
			t.Fatalf("a2a_send(%s) refused: %s", msgID, resultText(res))
		}
		var view a2aTaskView
		if err := json.Unmarshal([]byte(resultText(res)), &view); err != nil {
			t.Fatalf("decode a2a_send(%s): %v (%s)", msgID, err, resultText(res))
		}
		return view
	}

	// Worker 0 occupies the supervisor with an active task.
	t0 := callSend(w0, "w0-msg", "Initial active exchange.")
	if t0.State != a2aSubmitted || t0.Queued {
		t.Fatalf("first task should be submitted and not queued: %+v", t0)
	}
	a2aProcessOne(t, s, st, cfg, t0.ID)

	// Two workers report to the same busy supervisor back to back.
	t1 := callSend(w1, "w1-report", "Report from worker 1.")
	if t1.State != a2aQueued || !t1.Queued {
		t.Fatalf("worker 1 send while busy should return queued: %+v", t1)
	}
	// Idempotent retry on a queued send returns the same task without duplicating.
	t1Retry := callSend(w1, "w1-report", "Report from worker 1.")
	if t1Retry.ID != t1.ID || t1Retry.State != a2aQueued || !t1Retry.Queued {
		t.Fatalf("retry of queued send mismatch: %+v vs %+v", t1Retry, t1)
	}
	t2 := callSend(w2, "w2-report", "Report from worker 2.")
	if t2.State != a2aQueued || !t2.Queued {
		t.Fatalf("worker 2 send while busy should return queued: %+v", t2)
	}

	// Queued tasks must not deliver prematurely even if the supervisor session is idle.
	a2aProcessOne(t, s, st, cfg, t1.ID)
	a2aProcessOne(t, s, st, cfg, t2.ID)
	if len(h.sentCalls()) != 1 {
		t.Fatalf("queued tasks delivered prematurely: %+v", h.sentCalls())
	}

	// Completing t0 promotes t1 (not t2) and delivers worker 1's report first.
	if _, err := s.a2aMutate(ctx, st, cfg, SessionCaller(supervisor), t0.ID, func(tk *Task) (bool, error) {
		return s.a2aReport(tk, supervisor, "t0-done", a2aComplete, "Ack 0.")
	}); err != nil {
		t.Fatal(err)
	}
	got1, err := DecodeTask(st.A2AGet(ctx, t1.ID))
	if err != nil || got1.State != a2aSubmitted {
		t.Fatalf("t1 should be promoted to submitted after t0 completes: %+v %v", got1, err)
	}
	got2, err := DecodeTask(st.A2AGet(ctx, t2.ID))
	if err != nil || got2.State != a2aQueued {
		t.Fatalf("t2 should remain queued while t1 is active: %+v %v", got2, err)
	}
	a2aProcessOne(t, s, st, cfg, t1.ID)
	sends := h.sentCalls()
	if len(sends) != 2 || !strings.Contains(string(sends[1].Params), "Report from worker 1.") {
		t.Fatalf("worker 1 report did not arrive next: %+v", sends)
	}

	// Completing t1 promotes t2 and delivers worker 2's report second (FIFO).
	if _, err := s.a2aMutate(ctx, st, cfg, SessionCaller(supervisor), t1.ID, func(tk *Task) (bool, error) {
		return s.a2aReport(tk, supervisor, "t1-done", a2aComplete, "Ack 1.")
	}); err != nil {
		t.Fatal(err)
	}
	got2, err = DecodeTask(st.A2AGet(ctx, t2.ID))
	if err != nil || got2.State != a2aSubmitted {
		t.Fatalf("t2 should be promoted to submitted after t1 completes: %+v %v", got2, err)
	}
	a2aProcessOne(t, s, st, cfg, t2.ID)
	sends = h.sentCalls()
	if len(sends) != 3 || !strings.Contains(string(sends[2].Params), "Report from worker 2.") {
		t.Fatalf("worker 2 report did not arrive after worker 1: %+v", sends)
	}
}

func TestA2AQueuedDrainOnFailureCancelAndQueuedCancel(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, parent, child := a2aFixture(t)
	ctx := t.Context()

	// 1. Active task fails -> queued task promotes and delivers.
	t1, err := s.a2aStart(ctx, st, cfg, parent, child, "fail-1", "", "Task 1", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.a2aStart(ctx, st, cfg, parent, child, "fail-2", "", "Task 2", false, nil)
	if err != nil || t2.State != a2aQueued {
		t.Fatalf("t2 state: %+v %v", t2, err)
	}
	t3, err := s.a2aStart(ctx, st, cfg, parent, child, "fail-3", "", "Task 3", false, nil)
	if err != nil || t3.State != a2aQueued {
		t.Fatalf("t3 state: %+v %v", t3, err)
	}
	a2aProcessOne(t, s, st, cfg, t1.ID)

	// Cancel the middle queued task (t2) while t1 is still working.
	if _, err := s.a2aMutate(ctx, st, cfg, parent, t2.ID, s.a2aCancel); err != nil {
		t.Fatalf("cancel queued t2: %v", err)
	}
	// t3 must remain queued because t1 is still active.
	check3, err := DecodeTask(st.A2AGet(ctx, t3.ID))
	if err != nil || check3.State != a2aQueued {
		t.Fatalf("canceling queued t2 promoted t3 prematurely: %+v %v", check3, err)
	}

	// Failing t1 skips canceled t2 and promotes t3.
	if _, err := s.a2aMutate(ctx, st, cfg, SessionCaller(child), t1.ID, func(tk *Task) (bool, error) {
		return s.a2aReport(tk, child, "t1-fail", a2aFailed, "build broke")
	}); err != nil {
		t.Fatal(err)
	}
	check3, err = DecodeTask(st.A2AGet(ctx, t3.ID))
	if err != nil || check3.State != a2aSubmitted {
		t.Fatalf("failing t1 did not promote t3: %+v %v", check3, err)
	}

	// 2. Canceling an active (submitted) task promotes the next queued task.
	t4, err := s.a2aStart(ctx, st, cfg, parent, child, "cancel-4", "", "Task 4", false, nil)
	if err != nil || t4.State != a2aQueued {
		t.Fatalf("t4 state: %+v %v", t4, err)
	}
	if _, err := s.a2aMutate(ctx, st, cfg, parent, t3.ID, s.a2aCancel); err != nil {
		t.Fatalf("cancel active submitted t3: %v", err)
	}
	check4, err := DecodeTask(st.A2AGet(ctx, t4.ID))
	if err != nil || check4.State != a2aSubmitted {
		t.Fatalf("canceling active t3 did not promote t4: %+v %v", check4, err)
	}

	// 3. Expiring an unconfirmed active task promotes the next queued task.
	t5, err := s.a2aStart(ctx, st, cfg, parent, child, "expire-5", "", "Task 5", false, nil)
	if err != nil || t5.State != a2aQueued {
		t.Fatalf("t5 state: %+v %v", t5, err)
	}
	h.mu.Lock()
	h.sendErr = errors.New("lost send reply")
	h.mu.Unlock()
	a2aProcessOne(t, s, st, cfg, t4.ID)
	h.clock.now = h.clock.Now().Add(a2aConfirmFor + time.Second)
	check4 = a2aProcessOne(t, s, st, cfg, t4.ID)
	if check4.State != a2aFailed {
		t.Fatalf("expected t4 to expire as failed: %+v", check4)
	}
	h.mu.Lock()
	h.sendErr = nil
	h.mu.Unlock()
	check5, err := DecodeTask(st.A2AGet(ctx, t5.ID))
	if err != nil || check5.State != a2aSubmitted {
		t.Fatalf("expiring t4 did not promote t5: %+v %v", check5, err)
	}
	a2aProcessOne(t, s, st, cfg, t5.ID)
	sends := h.sentCalls()
	if len(sends) != 3 || !strings.Contains(string(sends[2].Params), "Task 5") {
		t.Fatalf("t5 was not delivered after t4 expired: %+v", sends)
	}
}

func TestA2ALoopDrainsBusySupervisorQueueWithoutLeaks(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, _, _ := a2aFixture(t)
	ctx := t.Context()
	supervisor := Ref{Machine: "mac", Session: "supervisor-loop"}
	w0 := Ref{Machine: "box", Session: "runtime.loop-0"}
	w1 := Ref{Machine: "box", Session: "runtime.loop-1"}
	w2 := Ref{Machine: "box", Session: "runtime.loop-2"}
	h.mu.Lock()
	h.sessions["mac"] = []proto.Session{{SessionID: supervisor.Session, Status: proto.StatusIdle, Agent: "codex"}}
	h.sessions["box"] = []proto.Session{
		{SessionID: w0.Session, Status: proto.StatusWorking, Agent: "pi", SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}},
		{SessionID: w1.Session, Status: proto.StatusWorking, Agent: "pi", SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}},
		{SessionID: w2.Session, Status: proto.StatusWorking, Agent: "pi", SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}},
	}
	h.mu.Unlock()

	loopCtx, stopLoop := context.WithCancel(ctx)
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		s.RunDelivery(loopCtx)
	}()
	t.Cleanup(func() {
		stopLoop()
		select {
		case <-loopDone:
		case <-time.After(5 * time.Second):
			t.Error("RunDelivery did not exit after context cancellation")
		}
	})

	t0, err := s.a2aStart(ctx, st, cfg, SessionCaller(w0), supervisor, "loop-0", "", "Active task 0", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Wake()
	h.waitForSend(t, "active task 0 delivered", 1)

	// Two workers send reports back to back while task 0 is active.
	t1, err := s.a2aStart(ctx, st, cfg, SessionCaller(w1), supervisor, "loop-1", "", "Queued report 1", false, nil)
	if err != nil || t1.State != a2aQueued {
		t.Fatalf("t1: %+v %v", t1, err)
	}
	t2, err := s.a2aStart(ctx, st, cfg, SessionCaller(w2), supervisor, "loop-2", "", "Queued report 2", false, nil)
	if err != nil || t2.State != a2aQueued {
		t.Fatalf("t2: %+v %v", t2, err)
	}

	// Complete task 0 -> RunDelivery automatically drains queued report 1.
	if _, err := s.a2aMutate(ctx, st, cfg, SessionCaller(supervisor), t0.ID, func(tk *Task) (bool, error) {
		return s.a2aReport(tk, supervisor, "done-0", a2aComplete, "Done 0")
	}); err != nil {
		t.Fatal(err)
	}
	h.waitForSend(t, "queued report 1 delivered by RunDelivery", 2)

	// Fail task 1 -> RunDelivery automatically drains queued report 2.
	if _, err := s.a2aMutate(ctx, st, cfg, SessionCaller(supervisor), t1.ID, func(tk *Task) (bool, error) {
		return s.a2aReport(tk, supervisor, "fail-1", a2aFailed, "Failed 1")
	}); err != nil {
		t.Fatal(err)
	}
	h.waitForSend(t, "queued report 2 delivered by RunDelivery", 3)

	sends := h.sentCalls()
	if !strings.Contains(string(sends[0].Params), "Active task 0") ||
		!strings.Contains(string(sends[1].Params), "Queued report 1") ||
		!strings.Contains(string(sends[2].Params), "Queued report 2") {
		t.Fatalf("unexpected delivery order: %+v", sends)
	}
}

func TestA2AJSONRPCQueuesWhenTargetBusy(t *testing.T) {
	t.Parallel()
	s, _, st, _, _, child := a2aFixture(t)
	path := "/api/v1/a2a/" + child.Machine + "/" + child.Session + "/rpc"
	call := func(device, method, params string) a2aRPCResponse {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
		r.Header.Set("A2A-Version", "1.0")
		r.Header.Set(extwire.HeaderDevice, device)
		r.Header.Set(extwire.HeaderRole, "scoped")
		r.Header.Set(extwire.HeaderScopes, assistantwire.ScopeA2A)
		w := httptest.NewRecorder()
		rpcHandler(s).ServeHTTP(w, r)
		var answer a2aRPCResponse
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &answer) != nil {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		return answer
	}
	first := call("client-a", "SendMessage", `{"configuration":{"returnImmediately":true},"message":{"messageId":"msg-1","role":"ROLE_USER","parts":[{"text":"First."}]}}`)
	if first.Error != nil {
		t.Fatal(first.Error)
	}
	firstID := first.Result.(map[string]any)["task"].(map[string]any)["id"].(string)

	second := call("client-b", "SendMessage", `{"configuration":{"returnImmediately":true},"message":{"messageId":"msg-2","role":"ROLE_USER","parts":[{"text":"Second."}]}}`)
	if second.Error != nil {
		t.Fatalf("second SendMessage while busy should queue, got error: %+v", second.Error)
	}
	taskObj := second.Result.(map[string]any)["task"].(map[string]any)
	secondID := taskObj["id"].(string)
	if taskObj["status"].(map[string]any)["state"] != "TASK_STATE_SUBMITTED" {
		t.Fatalf("wire state should remain TASK_STATE_SUBMITTED: %+v", taskObj)
	}
	if taskObj["metadata"].(map[string]any)["personaQueued"] != true {
		t.Fatalf("expected personaQueued metadata on queued task: %+v", taskObj)
	}

	// ListTasks filtering by TASK_STATE_SUBMITTED includes the queued task.
	page := call("client-b", "ListTasks", `{"status":"TASK_STATE_SUBMITTED","historyLength":0}`)
	if page.Error != nil || page.Result.(map[string]any)["totalSize"] != float64(1) {
		t.Fatalf("ListTasks for queued submitted task: %+v", page)
	}

	// Canceling the first task promotes the second task in Postgres.
	if cancel := call("client-a", "CancelTask", `{"id":"`+firstID+`"}`); cancel.Error != nil {
		t.Fatal(cancel.Error)
	}
	got, err := DecodeTask(st.A2AGet(t.Context(), secondID))
	if err != nil || got.State != a2aSubmitted {
		t.Fatalf("second task not promoted after first canceled: %+v %v", got, err)
	}
}

func TestA2AConcurrentSendsToSameTargetSerializeCleanly(t *testing.T) {
	t.Parallel()
	s, h, st, cfg, _, child := a2aFixture(t)
	ctx := t.Context()
	const n = 5
	workers := make([]Ref, n)
	h.mu.Lock()
	h.sessions["mac"] = nil
	for i := range n {
		workers[i] = Ref{Machine: "mac", Session: fmt.Sprintf("concurrent-worker-%d", i)}
		h.sessions["mac"] = append(h.sessions["mac"], proto.Session{SessionID: workers[i].Session, Status: proto.StatusWorking, Agent: "codex"})
	}
	h.mu.Unlock()

	tasks := make([]*Task, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			tasks[i], errs[i] = s.a2aStart(ctx, st, cfg, SessionCaller(workers[i]), child, fmt.Sprintf("concurrent-%d", i), "", fmt.Sprintf("Report %d", i), false, nil)
		})
	}
	wg.Wait()

	submitted, queued := 0, 0
	var active *Task
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("worker %d refused: %v", i, errs[i])
		}
		switch tasks[i].State {
		case a2aSubmitted:
			submitted++
			active = tasks[i]
		case a2aQueued:
			queued++
		default:
			t.Fatalf("worker %d unexpected state: %s", i, tasks[i].State)
		}
	}
	if submitted != 1 || queued != n-1 || active == nil {
		t.Fatalf("expected 1 submitted and %d queued, got %d submitted and %d queued", n-1, submitted, queued)
	}

	// Drain all n tasks strictly one active at a time in created_at order.
	var delivered []time.Time
	for range n {
		a2aProcessOne(t, s, st, cfg, active.ID)
		delivered = append(delivered, active.Created)
		if _, err := s.a2aMutate(ctx, st, cfg, SessionCaller(child), active.ID, func(tk *Task) (bool, error) {
			return s.a2aReport(tk, child, "done-"+active.ID, a2aComplete, "ok")
		}); err != nil {
			t.Fatal(err)
		}
		var nextActive *Task
		for _, candidate := range tasks {
			cur, err := DecodeTask(st.A2AGet(ctx, candidate.ID))
			if err != nil {
				t.Fatal(err)
			}
			if cur.State == a2aSubmitted {
				if nextActive != nil {
					t.Fatalf("more than one active task promoted: %s and %s", nextActive.ID, cur.ID)
				}
				nextActive = cur
			}
		}
		active = nextActive
	}
	if active != nil {
		t.Fatalf("expected queue to be empty after draining %d tasks, still active: %+v", n, active)
	}
	for i := 1; i < len(delivered); i++ {
		if !delivered[i].After(delivered[i-1]) {
			t.Fatalf("tasks did not drain in strict FIFO created_at order: %v", delivered)
		}
	}
}
