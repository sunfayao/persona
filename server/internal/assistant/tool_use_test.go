package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// An assistant conversation's tool calls are recorded through the path every call takes (mcpCall): the tool, the
// failure's code and the shape of the arguments, never a value of them. Another caller's calls are not.
func TestAssistantToolCallsAreRecordedWithoutValues(t *testing.T) {
	s, _, _ := roleContractStoredService(t)
	before := time.Now().Add(-time.Minute)
	if res := roleContractGeneralCall(t, s, "conv1", roleContractGeneralTask("")); !res.IsError {
		t.Fatalf("the task with no role was planned: %+v", res)
	}
	if res := s.mcpCall(context.Background(), "server", "conv1", "no_such_tool", json.RawMessage(`{"note":"private words"}`), true); !res.IsError {
		t.Fatal("an unknown tool answered")
	}
	s.mcpCall(context.Background(), "server", "agent-session", "no_such_tool", nil, false) // an Agent's call

	events := s.toolUse.Friction(before)
	if len(events) != 2 {
		t.Fatalf("friction = %+v, want the two assistant failures", events)
	}
	plan, unknown := events[0], events[1]
	// The role refusal is a plain error: it has no code to keep, and its text is never kept in its place.
	if plan.Tool != "task_plan" || plan.Session != "conv1" || plan.Code != "error" {
		t.Errorf("task_plan's failure = %+v", plan)
	}
	if !strings.HasPrefix(plan.Skeleton, "{tasks:[{") {
		t.Errorf("task_plan's skeleton = %q", plan.Skeleton)
	}
	if unknown.Tool != "no_such_tool" || unknown.Code != "unknown_tool" || unknown.Skeleton != "{note:str}" {
		t.Errorf("the unknown tool's failure = %+v", unknown)
	}
	for _, e := range events {
		for _, value := range []string{"private", "Restricted", "dispatch path", "focused"} {
			if strings.Contains(e.Skeleton, value) || strings.Contains(e.Code, value) {
				t.Errorf("%s's record holds %q: %+v", e.Tool, value, e)
			}
		}
	}
	if got := s.toolUse.Usage("", before)["no_such_tool"]; got.Calls != 1 || got.Errors != 1 {
		t.Errorf("no_such_tool usage = %+v, want the assistant's call only", got)
	}
}

// A refusal with a code keeps it through the whole path: tool() notes the RPC error's code before it becomes text.
func TestAssistantToolCallKeepsTheRefusalsCode(t *testing.T) {
	s, f, _ := roleContractStoredService(t)
	f.sessions["box"] = nil
	f.ownModels = true
	f.launcherList = readonlyEngineLaunchers()
	task := readonlyTestTask("")
	task.Launcher, task.Role = "pi", "worker"
	if res := s.mcpCall(context.Background(), "server", "readonly-chat", "task_plan", readonlyTaskArgs(t, task), true); !res.IsError {
		t.Fatalf("a read-only pi task was planned: %+v", res)
	}
	events := s.toolUse.Friction(time.Time{})
	if len(events) != 1 || events[0].Tool != "task_plan" || events[0].Code != "readonly_unsupported" {
		t.Fatalf("friction = %+v, want task_plan's readonly_unsupported", events)
	}
}

// A failure's code is its RPC error's code, else the key of an i18n error, else what the answer says of itself.
func TestToolCallCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&rpc.Error{Code: "readonly_unsupported", Message: "no"}, "readonly_unsupported"},
		{i18n.Err("assistant-tasks.readonly_agy_unsupported"), "assistant-tasks.readonly_agy_unsupported"},
		{errors.New("plain"), ""},
	} {
		r := &callRecord{}
		noteCallError(withCallRecord(context.Background(), r), tc.err)
		if r.code != tc.want {
			t.Errorf("code of %v = %q, want %q", tc.err, r.code, tc.want)
		}
	}
	noteCallError(context.Background(), errors.New("no record")) // a call that is not recorded: nothing to keep

	for _, tc := range []struct {
		res  proto.ToolResult
		want string
	}{
		{proto.TextResult(`{"code":"invalid_batch","detail":"launch_app cannot run in a batch","ok":false}`, true), "invalid_batch"},
		{proto.TextResult("unknown tool x", true), "unknown_tool"},
		{proto.TextResult("invalid arguments: unexpected end", true), "invalid_arguments"},
		{proto.ErrorResult(proto.ToolErrorUnavailable, "away", 0), proto.ToolErrorUnavailable},
		{proto.TextResult("something else", true), "error"},
	} {
		if got := resultCode(tc.res); got != tc.want {
			t.Errorf("resultCode(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
}
