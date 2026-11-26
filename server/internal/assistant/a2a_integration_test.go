package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistant/agents"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

type a2aTestHub struct {
	*fakeHub
	sends   []DeviceCall
	sendErr error
	wakes   chan DeviceCall
}

func (h *a2aTestHub) DeviceCall(ctx context.Context, call DeviceCall) (json.RawMessage, error) {
	if call.Method == "session.a2a.send" {
		h.mu.Lock()
		h.sends = append(h.sends, call)
		sendErr := h.sendErr
		h.mu.Unlock()
		return json.RawMessage(`{}`), sendErr
	}
	if call.Method == "session.a2a.wake" && h.wakes != nil {
		h.wakes <- call
		var p struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(call.Params, &p)
		return json.Marshal(map[string]any{"session": proto.Session{SessionID: p.SessionID}})
	}
	return h.fakeHub.DeviceCall(ctx, call)
}

func (h *a2aTestHub) sentCalls() []DeviceCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]DeviceCall, len(h.sends))
	copy(out, h.sends)
	return out
}

func a2aFixture(t *testing.T) (*Service, *a2aTestHub, *db.DB, assistantwire.A2AConfig, agents.Caller, agents.Ref) {
	t.Helper()
	database, err := db.Open(t.Context(), newDatabase(t), db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	f := newFakeHub()
	h := &a2aTestHub{fakeHub: f}
	cfg := assistantwire.A2AConfig{Machines: []string{"mac", "box"}, PublicURL: "https://persona.example.com"}
	f.hello.Agents.A2A = cfg
	from, to := agents.Ref{Machine: "mac", Session: "caller-0001"}, agents.Ref{Machine: "box", Session: "runtime.worker-0001"}
	f.sessions[from.Machine] = []proto.Session{{SessionID: from.Session, Status: proto.StatusWorking, Agent: "codex"}}
	f.sessions[to.Machine] = []proto.Session{{SessionID: to.Session, Status: proto.StatusIdle, Agent: "pi",
		SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}}}
	return New(Options{Hub: h, Store: func() Store { return database }, Logf: testLogf(t)}), h, database, cfg, agents.SessionCaller(from), to
}

// Process the same persisted row the background delivery worker would read.
func a2aProcessOne(t *testing.T, s *Service, st *db.DB, id string) *agents.Task {
	t.Helper()
	ctx := t.Context()
	if err := s.agents.ProcessTask(ctx, id); err != nil {
		t.Fatal(err)
	}
	task, err := agents.DecodeTask(st.A2AGet(ctx, id))
	if err != nil || task == nil {
		t.Fatalf("get processed task: %v %v", task, err)
	}
	return task
}

func a2aSendTask(ctx context.Context, s *Service, st *db.DB, c agents.Caller, args map[string]string) (*agents.Task, error) {
	raw, _ := json.Marshal(args)
	res := s.agents.Tool(ctx, c.Ref, "a2a_send", raw)
	if res.IsError {
		return nil, errors.New(resultText(res))
	}
	return agents.DecodeTask(st.A2ARequest(ctx, c.Owner, args["message_id"]))
}

type a2aRPCResponse struct {
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
	Result any `json:"result"`
}

func TestA2AArchiveClosesTheTaskAndTellsTheSender(t *testing.T) {
	s, h, st, _, parent, child := a2aFixture(t)
	ctx := t.Context()
	task, err := a2aSendTask(ctx, s, st, parent, map[string]string{"machine_id": child.Machine, "session_id": child.Session, "message_id": "initial", "text": "Inspect the build."})
	if err != nil {
		t.Fatal(err)
	}
	a2aProcessOne(t, s, st, task.ID)
	row := h.sessions[child.Machine][0]
	row.MachineID = child.Machine
	if err := s.noteAgentArchive(ctx, row, true); err != nil {
		t.Fatal(err)
	}
	setStatus(h.fakeHub, parent.Ref.Machine, parent.Ref.Session, proto.StatusIdle)
	task = a2aProcessOne(t, s, st, task.ID)
	if !task.TargetClosed || task.State != "failed" {
		t.Fatalf("archive did not close task: %+v", task)
	}
	a2aProcessOne(t, s, st, task.ID)
	if len(h.sends) != 2 || h.sends[1].Device != parent.Ref.Machine || !strings.Contains(string(h.sends[1].Params), "receiving session was closed") {
		t.Fatalf("archive result did not reach the parent: %+v", h.sends)
	}
	args, _ := json.Marshal(map[string]string{"task_id": task.ID})
	if res := s.agents.Tool(ctx, parent.Ref, "a2a_get", args); res.IsError {
		t.Fatalf("sender could not read the closed task: %s", resultText(res))
	}
	if res := s.agents.Tool(ctx, child, "a2a_get", args); !res.IsError {
		t.Fatalf("archived session could still read task: %s", resultText(res))
	}
}

func TestA2ALaunchesShareTheDispatchLimitAndDepth(t *testing.T) {
	s, h, st, _, parent, _ := a2aFixture(t)
	ctx := t.Context()
	h.hello.Agents.Dispatch.MaxActive = 1
	start := func(c agents.Caller, machine, id string) (*agents.Task, error) {
		return a2aSendTask(ctx, s, st, c, map[string]string{"machine_id": machine, "cwd": "/code/app", "message_id": id, "text": "Inspect the build."})
	}
	task, err := start(parent, "box", "launch")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := start(parent, "box", "launch"); err != nil || again.ID != task.ID {
		t.Fatalf("an idempotent retry consumed capacity: %+v %v", again, err)
	}
	if _, err := start(parent, "box", "second-launch"); err == nil || !strings.Contains(err.Error(), "the limit is 1") {
		t.Fatalf("a second launch exceeded the dispatch limit: %v", err)
	}
	if _, err := start(agents.SessionCaller(agents.Ref{Machine: task.TargetMachine, Session: task.TargetSession}), "mac", "child-launch"); err == nil || !strings.Contains(err.Error(), "does not dispatch") {
		t.Fatalf("a child could dispatch another child: %v", err)
	}
}

func TestA2AJSONRPCTaskOwnershipAndCancel(t *testing.T) {
	s, h, _, _, _, child := a2aFixture(t)
	path := "/api/v1/a2a/" + child.Machine + "/" + child.Session + "/rpc"
	call := func(device, method, params string) a2aRPCResponse {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
		r.Header.Set("A2A-Version", "1.0")
		r.Header.Set(extwire.HeaderDevice, device)
		r.Header.Set(extwire.HeaderRole, "scoped")
		r.Header.Set(extwire.HeaderScopes, assistantwire.ScopeA2A)
		w := httptest.NewRecorder()
		s.ServeSocket(w, r)
		var answer a2aRPCResponse
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &answer) != nil {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		return answer
	}
	reply := call("first", "SendMessage", `{"configuration":{"returnImmediately":true},"message":{"messageId":"remote-message","role":"ROLE_USER","parts":[{"text":"Hello."}]}}`)
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	id := reply.Result.(map[string]any)["task"].(map[string]any)["id"].(string)
	params := `{"id":"` + id + `"}`
	if e := call("second", "GetTask", params).Error; e == nil || e.Code != -32001 {
		t.Fatalf("task available to different client: %+v", e)
	}
	page := call("first", "ListTasks", `{"historyLength":0,"statusTimestampAfter":"2020-01-01T00:00:00Z"}`)
	if page.Error != nil || page.Result.(map[string]any)["totalSize"] != float64(1) {
		t.Fatalf("list result: %+v", page)
	}
	cancel := call("first", "CancelTask", params)
	if cancel.Error != nil || cancel.Result.(map[string]any)["status"].(map[string]any)["state"] != "TASK_STATE_CANCELED" {
		t.Fatalf("cancel result: %+v", cancel)
	}
	row := h.sessions[child.Machine][0]
	row.MachineID = child.Machine
	if err := s.noteAgentArchive(t.Context(), row, true); err != nil {
		t.Fatal(err)
	}
	got := call("first", "GetTask", params)
	if got.Error != nil || got.Result.(map[string]any)["status"].(map[string]any)["state"] != "TASK_STATE_CANCELED" {
		t.Fatalf("owner could not read the final state after archive: %+v", got)
	}
	if e := call("second", "GetTask", params).Error; e == nil || e.Code != -32001 {
		t.Fatalf("archived task available to different client: %+v", e)
	}
}

func TestA2AEnrolledSessionGetsTools(t *testing.T) {
	s, _, _, _, parent, child := a2aFixture(t)
	ctx := t.Context()
	tools, err := s.sessionMCPToolsChecked(ctx, parent.Ref.Machine, parent.Ref.Session, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range assistantwire.A2AToolNames {
		if !toolListed(tools, name) {
			t.Fatalf("MCP tool %q missing", name)
		}
	}
	r := s.mcpCall(ctx, parent.Ref.Machine, parent.Ref.Session, "a2a_sessions", json.RawMessage(`{}`), false)
	if r.IsError || !strings.Contains(resultText(r), child.Session) {
		t.Fatalf("discovery: %s", resultText(r))
	}
	if r := s.mcpCall(ctx, "unenrolled", parent.Ref.Session, "a2a_sessions", json.RawMessage(`{}`), false); !r.IsError {
		t.Fatal("an unenrolled machine could discover sessions")
	}
}

func TestA2AIsDefaultForTrustedSessionsAndDispatch(t *testing.T) {
	s, h, st, _, parent, _ := a2aFixture(t)
	ctx := t.Context()
	h.fakeModel(t)
	h.hello.Agents.A2A = assistantwire.A2AConfig{}
	on := true
	h.hello.Agents.Dispatch.Enabled = &on
	h.hello.Agents.Dispatch.MaxActive = 1
	h.startReply = func(args map[string]any, row *proto.Session) *proto.StartInstead {
		row.SessionID = args["session_id"].(string)
		return nil
	}
	h.onStart = func(f *fakeHub, args map[string]any) {
		f.sessions["box"] = append(f.sessions["box"], proto.Session{
			SessionID: args["session_id"].(string), Status: proto.StatusWorking, Agent: "codex",
		})
	}
	if tools := s.sessionMCPTools(ctx, parent.Ref.Machine, parent.Ref.Session, false, false); !toolListed(tools, "a2a_send") {
		t.Fatal("trusted session without an A2A stanza did not get the A2A tool")
	}
	// Some runtime plugins declare their tool catalog before they have a session.
	if tools := s.sessionMCPTools(ctx, parent.Ref.Machine, "", false, false); !toolListed(tools, "a2a_report") {
		t.Fatal("the runtime's machine-scoped catalog lacks A2A declarations")
	}
	if result := s.mcpCall(ctx, parent.Ref.Machine, "", "a2a_sessions", json.RawMessage(`{}`), false); !result.IsError {
		t.Fatal("a runtime without a bound session could invoke A2A")
	}
	fullPrompt := "Run the integration checks.\n" + strings.Repeat("Check a test case.\n", 800) + "Final instruction."
	request := map[string]any{"machine": "box", "cwd": "/code/app", "prompt": fullPrompt, "message_id": "dispatch-receipt",
		"worktree": map[string]string{"branch": "fix/linux"}}
	args, _ := json.Marshal(request)
	result := dispatchCall(s, parent.Ref.Machine, parent.Ref.Session, "dispatch_start", string(args))
	if result.IsError || !strings.Contains(resultText(result), "A2A task") {
		t.Fatalf("default dispatch: %s", resultText(result))
	}
	task, err := agents.DecodeTask(st.A2ARequest(ctx, parent.Owner, "dispatch-receipt"))
	if err != nil || task == nil || task.Launch.Pending || !task.Wake || !task.Launch.Dispatch {
		t.Fatalf("durable dispatch: %+v %v", task, err)
	}
	if task.Launch.CWD != "/code/app" || task.Launch.Worktree != "/wt/app/fix-linux" {
		t.Fatalf("dispatch lost the original directory or the actual worktree: %+v", task.Launch)
	}
	if len(h.starts) != 1 || len(s.dispatched(ctx)) != 0 {
		t.Fatalf("unexpected start or legacy record: %+v", h.starts)
	}
	prompt := h.starts[0]["prompt"].(string)
	if prompts.RelayMessageID(prompt) != fmt.Sprintf("%s%s-1", proto.A2ADeliveryPrefix, task.ID) || !strings.Contains(prompt, fullPrompt) {
		t.Fatal("dispatch prompt lost its A2A identity or original content")
	}
	again := dispatchCall(s, parent.Ref.Machine, parent.Ref.Session, "dispatch_start", string(args))
	if again.IsError || !strings.Contains(resultText(again), task.ID) || len(h.starts) != 1 {
		t.Fatalf("retry at the capacity limit: %s, starts %d", resultText(again), len(h.starts))
	}
	request["prompt"] = fullPrompt + " Different ending."
	conflictArgs, _ := json.Marshal(request)
	conflict := dispatchCall(s, parent.Ref.Machine, parent.Ref.Session, "dispatch_start", string(conflictArgs))
	if !conflict.IsError || !strings.Contains(resultText(conflict), "message_id") || len(h.starts) != 1 {
		t.Fatalf("changed prompt was accepted on retry: %s", resultText(conflict))
	}
	h.setItems(proto.Item{Role: "user", Text: prompt})
	task = a2aProcessOne(t, s, st, task.ID)
	if task.State != "working" {
		t.Fatalf("first prompt was not recognized: %+v", task.Events)
	}
	setStatus(h.fakeHub, task.TargetMachine, task.TargetSession, proto.StatusBlocked)
	task = a2aProcessOne(t, s, st, task.ID)
	if task.State != "auth_required" {
		t.Fatalf("native blocker status: %s", task.State)
	}
	parentSession := parent.Ref.Session
	setStatus(h.fakeHub, parent.Ref.Machine, parentSession, proto.StatusDone)
	restarted := New(Options{Hub: h, Store: func() Store { return st }})
	a2aProcessOne(t, restarted, st, task.ID)
	if len(h.sends) != 1 || h.sends[0].Device != parent.Ref.Machine {
		t.Fatalf("default dispatch did not wake its parent: %+v", h.sends)
	}
	read := dispatchCall(restarted, parent.Ref.Machine, parentSession, "dispatch_read", `{"session_id":"`+task.ID+`"}`)
	if read.IsError || !strings.Contains(resultText(read), "waiting for the user") {
		t.Fatalf("compatibility read: %s", resultText(read))
	}
	setStatus(h.fakeHub, task.TargetMachine, task.TargetSession, proto.StatusWorking)
	if task = a2aProcessOne(t, restarted, st, task.ID); task.State != "working" {
		t.Fatalf("task did not resume after approval: %s", task.State)
	}
	a2aProcessOne(t, restarted, st, task.ID)
	if len(h.sends) != 2 || h.sends[1].Device != parent.Ref.Machine || !strings.Contains(string(h.sends[1].Params), "resumed work") {
		t.Fatalf("resumed status was not sent to the parent: %+v", h.sends)
	}
}

// The integration tests only need the identifier returned by the tool.
type a2aTaskView struct {
	ID string `json:"task_id"`
}
