package agy

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestBackgroundLifecycleAndTimerWakesComeFromTheTranscript(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	const at = "2026-01-01T00:00:10Z"
	const run = `{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-01-01T00:00:00Z","tool_calls":[{"name":"run_command","args":{"CommandLine":"echo ready","toolSummary":"Wait for ready"}},{"name":"schedule","args":{"Prompt":"Check again","DurationSeconds":30,"TimerCondition":"never"}},{"name":"schedule","args":{"Prompt":"Periodic check","CronExpression":"*/5 * * * *","MaxIterations":2}}]}` + "\n"
	output := filepath.Join(ref.Root, "brain", ref.Conversation, ".system_generated", "tasks", "task-2.log")
	uri := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(output), "/")}).String()
	shell, timer, cron := ref.Conversation+"/task-2", ref.Conversation+"/task-3", ref.Conversation+"/task-4"
	start := func(i int, id, desc, tail string) string {
		return agyParseLine(i, "GENERIC", "RUNNING", at, "Created At: "+at+"\n"+taskPrefix+" "+id+"\nTask Description: "+desc+tail)
	}
	message := func(i int, sender, text string) string {
		return agyParseLine(i, "SYSTEM_MESSAGE", "DONE", at, "<SYSTEM_MESSAGE>\n[Message] timestamp="+at+" sender="+sender+" priority=MESSAGE_PRIORITY_HIGH content="+text+"\n</SYSTEM_MESSAGE>")
	}
	file := agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "Start tasks")+run+
		start(2, shell, "Wait for ready", "\nTask logs are available at: "+uri)+start(3, timer, "Timer: 30s", "")+start(4, cron, "Cron: every five minutes", ""))
	r := New(Dependencies{Clock: testClock{}})
	h := readFixture(t, r, ref)
	var row proto.Session
	h.Apply(&row, true)
	if len(row.Background) != 1 || row.Background[0].ID != shell || row.Background[0].OutputFile != output || row.Background[0].Command != "echo ready" {
		t.Fatalf("background shell: %+v", row.Background)
	}
	if len(row.Facets.Timers) != 2 || row.Loop == nil || row.Loop.Kind != "cron" || row.Facets.Timers[0].Timer.NextAt.UTC().Format("15:04:05") != "00:00:40" {
		t.Fatalf("scheduled timers: %+v, loop: %+v", row.Facets.Timers, row.Loop)
	}
	shellCall := h.items[1].ToolUseID
	appendFixture(t, file, message(5, shell, "Background command is still running after 120s: echo ready")+
		message(6, shell, fmt.Sprintf("Task id %q finished with result:\nCommand exited with code 3.", shell))+
		message(7, timer, "Check again")+message(8, cron, "Periodic check")+
		agyParseLine(9, "SYSTEM_MESSAGE", "DONE", stamp, "Reminder: work efficiently.")+
		agyParseLine(10, "PLANNER_RESPONSE", "DONE", stamp, "Follow-up work"))
	h = readFixture(t, r, ref)
	h.Apply(&row, true)
	if len(row.Background) != 0 || len(row.BackgroundEnded()) != 1 || row.BackgroundEnded()[0].Status != "failed" ||
		len(row.Facets.Timers) != 1 || row.Facets.Timers[0].ID != cron || row.Loop.Ticks != 1 {
		t.Fatalf("completed shell and fired timers: %+v / %+v / %+v", row.Background, row.BackgroundEnded(), row.Facets.Timers)
	}
	notices := 0
	for _, it := range h.items {
		if it.Kind != proto.KindNotice {
			continue
		}
		notices++
		if !it.Wake || it.Facets.TurnID != "t0" {
			t.Fatalf("expected a wake in the same turn: %+v", it)
		}
		switch it.Facets.Notice.Code {
		case proto.NoticeCodeTaskUpdate:
			if it.Task.TaskID != shell || it.Task.Title != "Wait for ready" || it.Task.Status != "failed" || it.Task.ToolUseID != shellCall ||
				it.Facets.ParentID != shellCall || it.Facets.Notice.Severity != "error" || *it.Facets.Task.ExitCode != 3 {
				t.Fatalf("task notice: %+v", it)
			}
		case proto.NoticeCodeTimerFired:
			if it.Task.Kind != "monitor" || it.Task.Status != "event" || it.Task.Title == "" || it.Facets.Task.Status != "fired" {
				t.Fatalf("timer notice: %+v", it)
			}
		default:
			t.Fatalf("unexpected notice: %+v", it)
		}
	}
	if notices != 3 {
		t.Fatalf("got %d notices, want 3", notices)
	}
	appendFixture(t, file, message(11, cron, "Periodic check")+agyParseLine(12, "USER_INPUT", "DONE", stamp, "Manual follow-up")+
		agyParseLine(13, "PLANNER_RESPONSE", "DONE", stamp, "Not a wake"))
	h = readFixture(t, r, ref)
	h.Apply(&row, true)
	lastCron := h.items[len(h.items)-3]
	if len(row.Facets.Timers) != 0 || row.Loop != nil || lastCron.Wake || lastCron.Task.TaskID != cron {
		t.Fatalf("cron cap or input boundary not respected: %+v / %+v", lastCron, row.Facets.Timers)
	}
}

func TestTaskKillConfirmationAndRestartDoNotLeaveRunningWork(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	id := ref.Conversation + "/task-1"
	line := func(i int, typ, content string) string { return agyParseLine(i, typ, "RUNNING", stamp, content) }
	fixture := `{"step_index":0,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"run_command","args":{"CommandLine":"example"}},{"name":"run_command","args":{"CommandLine":"second"}},{"name":"schedule","args":{"CronExpression":"*/5 * * * *"}}]}` + "\n" +
		line(1, "GENERIC", taskPrefix+" "+id) + line(2, "GENERIC", taskPrefix+" "+ref.Conversation+"/task-2") + line(3, "GENERIC", taskPrefix+" "+ref.Conversation+"/task-3") +
		fmt.Sprintf(`{"step_index":5,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"manage_task","args":{"Action":"kill","TaskId":%q}}]}`+"\n", id) +
		line(6, "SYSTEM_MESSAGE", fmt.Sprintf(`[Message] timestamp=%s sender=%[2]s content=Task id %[2]q was canceled with result: Stopped.`, stamp, id)) +
		line(7, "SYSTEM_MESSAGE", "[Message] timestamp="+stamp+" sender=system content=[Notice] "+serverRestart)
	agyTestFile(t, ref.Root, ref.Conversation, fixture)
	h := readFixture(t, New(Dependencies{Clock: testClock{}}), ref)
	var row proto.Session
	h.Apply(&row, true)
	if len(row.Background) != 0 || len(row.BackgroundEnded()) != 2 || len(row.Facets.Timers) != 0 {
		t.Fatalf("restart or cancellation was not folded: %+v / %+v / %+v", row.Background, row.BackgroundEnded(), row.Facets.Timers)
	}
	kill, cancelled, restart := h.items[len(h.items)-3], h.items[len(h.items)-2], h.items[len(h.items)-1]
	if kill.ToolName != "TaskStop" || string(kill.ToolInput) != fmt.Sprintf(`{"task_id":%q}`, id) || kill.Facets.Task.Status != "cancelled" ||
		cancelled.Task.Status != "killed" || restart.Facets.Notice.Code != "server_restart" {
		t.Fatalf("task cancel or restart notices: %+v / %+v / %+v", kill, cancelled, restart)
	}
}

func TestTaskLogRejectsAnEscapingSymlink(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	out := writeFixture(t, Ref{t.TempDir(), otherID}, "outside.log", "unrelated")
	path := writeFixture(t, ref, ".system_generated/tasks/task-0.log", "")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	uri := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}).String()
	agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "GENERIC", "RUNNING", stamp, taskPrefix+" "+ref.Conversation+"/task-0\nTask logs are available at: "+uri))
	h := readFixture(t, New(Dependencies{Clock: testClock{}}), ref)
	if task := h.items[0].Facets.Task; task == nil || task.OutputFile != "" {
		t.Fatalf("trusted a symlinked log: %+v", task)
	}
}

func TestCanonicalCallsAndAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ raw, name, args, kind string }{
		{`{"name":"run_command","args":{"CommandLine":"echo sample","toolSummary":"Describe","IsDaemon":true}}`, "Bash", `{"command":"echo sample","description":"Describe","run_in_background":true}`, "execute"},
		{`{"name":"read_file","args":{"Path":"/work/example","StartLine":4,"EndLine":8}}`, "Read", `{"file_path":"/work/example","limit":5,"offset":4}`, "read"},
		{`{"name":"read_file","args":{"Path":"\"/work/example\""}}`, "Read", `{"file_path":"\"/work/example\""}`, "read"},
		{`{"name":"write_file","args":{"Path":"/work/example","Content":"sample"}}`, "Write", `{"content":"sample","file_path":"/work/example"}`, "write"},
		{`{"name":"edit_file","args":{"Path":"/work/example","Target":"a","Replacement":"b"}}`, "Edit", `{"file_path":"/work/example","new_string":"b","old_string":"a"}`, "edit"},
		{`{"name":"search_web","args":{"query":"sample","domain":"example.com"}}`, "WebSearch", `{"allowed_domains":["example.com"],"query":"sample"}`, "web_search"},
		{`{"name":"manage_task","args":{"Action":"status","TaskId":"task-1"}}`, "TaskOutput", `{"task_id":"task-1"}`, "task"},
		{`{"name":"manage_task","args":{"Action":"list"}}`, "TaskList", `{"task_id":""}`, "plan"},
		{`{"name":"schedule","args":{"Prompt":"Sample","DurationSeconds":10,"TimerCondition":"never","MaxIterations":2}}`, "Schedule", `{"condition":"never","delay_s":10,"max_runs":2,"prompt":"Sample"}`, "schedule"},
		{`{"name":"mcp_persona_1234abcd_example","args":{"value":true}}`, "mcp__persona__example", `{"value":true}`, "mcp"},
		{`{"name":"persona_call","args":{"name":"example","arguments":"{\"value\":4}"}}`, "mcp__persona__example", `{"value":4}`, "mcp"},
		{`{"name":"mcp_persona_persona_call","args":{"name":"example","arguments":{"value":5}}}`, "mcp__persona__example", `{"value":5}`, "mcp"},
		{`{"name":"invoke_subagent","args":{"toolSummary":"Run helper","Subagents":[{"Prompt":"Sample"}]}}`, "Agent", `{"description":"Run helper","prompt":"[{\"Prompt\":\"Sample\"}]"}`, "agent"},
		{`{"name":"run_workflow","args":{"description":"sample"}}`, "run_workflow", `{"description":"sample"}`, "other"},
	} {
		var call nativeCall
		if err := json.Unmarshal([]byte(tc.raw), &call); err != nil {
			t.Fatal(err)
		}
		it := proto.Item{Facets: &proto.ItemFacets{Native: &proto.NativeRef{}}}
		mapCall(&call, &it)
		if it.ToolName != tc.name || string(it.ToolInput) != tc.args || it.Facets.Tool.Kind != tc.kind || it.Facets.Native.Name != call.Name {
			t.Errorf("canonical tool %s: %s %s %+v", call.Name, it.ToolName, it.ToolInput, it.Facets.Tool)
		}
	}
	fixture := agyParseLine(0, "USER_INPUT", "DONE", stamp, "Start") +
		`{"step_index":1,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"update_steps","args":{"steps":[{"title":"Open","status":"pending"},{"title":"Now","status":"in_progress"},{"title":"Done","status":"done"},{"title":"Skipped","status":"skipped"},{"title":"Failed","status":"failed"},{"title":"Cancelled","status":"cancelled"}]}},{"name":"ask_question","args":{"questions":[{"question":"First?","options":["A","B"],"is_multi_select":true},{"question":"Second?","options":["C"]}]}}]}` + "\n" +
		agyParseLine(2, "GENERIC", "DONE", stamp, "OK") + agyParseLine(3, "GENERIC", "DONE", stamp, "Created At: 2026-01-01T00:00:00Z\nCompleted At: 2026-01-01T00:00:02Z\nA1: A, B\nA2: C")
	ref := Ref{t.TempDir(), agyTestID}
	agyTestFile(t, ref.Root, ref.Conversation, fixture)
	h := readFixture(t, New(Dependencies{Clock: testClock{}}), ref)
	var row proto.Session
	h.Apply(&row, true)
	if row.Facets.Plan == nil || len(row.Facets.Plan.Entries) != 6 || row.Plan == nil || row.Plan.Current != "Now" ||
		row.Facets.Plan.Entries[3].Status != "skipped" || row.Facets.Plan.Entries[4].Status != "failed" || row.Facets.Plan.Entries[5].Status != "cancelled" ||
		strings.Count(string(h.items[1].ToolInput), `"status":"completed"`) != 4 {
		t.Fatalf("plan projection: %+v / %s", row.Facets.Plan, h.items[1].ToolInput)
	}
	q, answer := h.items[2], h.items[4]
	if string(q.ToolInput) != `{"questions":[{"header":"","multiSelect":true,"options":[{"label":"A"},{"label":"B"}],"question":"First?"},{"header":"","multiSelect":false,"options":[{"label":"C"}],"question":"Second?"}]}` ||
		answer.Outcome == nil || answer.Outcome.Status != "ok" || answer.Outcome.DurationMS == nil || *answer.Outcome.DurationMS != 2000 ||
		answer.Outcome.Answers["First?"] != "A, B" || answer.Outcome.Answers["Second?"] != "C" || answer.Facets.Interaction.Status != "answered" {
		t.Fatalf("question: %s; answer: %+v", q.ToolInput, answer)
	}
	for _, it := range h.items {
		if it.Facets.TurnID != "t0" {
			t.Errorf("item %s lacks turn identity", it.ID)
		}
	}
}

func TestManageSubagentsKillCancelsRunningSubagent(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	fixture := agyParseLine(0, "USER_INPUT", "DONE", stamp, "Spawn helper") +
		`{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-01-01T00:00:00Z","tool_calls":[{"name":"invoke_subagent","args":{"Role":"Helper","TypeName":"research","Prompt":"Inspect"}}]}` + "\n" +
		agyParseLine(2, "GENERIC", "DONE", stamp, "Launched subagent conversation "+otherID) +
		`{"step_index":3,"type":"PLANNER_RESPONSE","created_at":"2026-01-01T00:01:00Z","tool_calls":[{"name":"manage_subagents","args":{"Action":"kill","ConversationIds":["` + otherID + `"]}}]}` + "\n" +
		agyParseLine(4, "GENERIC", "DONE", stamp, "Killed")
	agyTestFile(t, ref.Root, ref.Conversation, fixture)
	h := readFixture(t, New(Dependencies{Clock: testClock{}}), ref)
	var row proto.Session
	h.Apply(&row, true)
	if len(row.Subagents) != 1 || row.Subagents[0].ID != otherID || row.Subagents[0].Running || row.Subagents[0].Status != proto.SubagentStatusCancelled {
		t.Fatalf("killed agy subagent = %+v, want cancelled", row.Subagents)
	}
}
