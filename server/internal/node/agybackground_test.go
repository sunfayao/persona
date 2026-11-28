package node

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func agyBackgroundLogURI(path string) string {
	return (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}).String()
}

func agyBackgroundStep(index int, status, started string, task int, description, logURI string) string {
	content := fmt.Sprintf("Created At: %s\n%s %s/task-%d\nTask Description: %s", started, agyTaskPrefix, agyTestID, task, description)
	if logURI != "" {
		content += "\nTask logs are available at: " + logURI
	}
	return fmt.Sprintf(`{"step_index":%d,"type":"GENERIC","status":%q,"created_at":%q,"content":%q}`+"\n", index, status, started, content)
}

func TestAgyBackgroundLifecycle(t *testing.T) {
	n := &Node{home: t.TempDir()}
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	dir := n.agyDataRoot()
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	finish := start.Add(time.Minute)
	started, ended := start.Format(time.RFC3339), finish.Format(time.RFC3339)
	logPath := filepath.Join(dir, "brain", agyTestID, ".system_generated", "tasks", "task-1.log")
	logURI := agyBackgroundLogURI(logPath)
	planner := fmt.Sprintf(`{"step_index":0,"type":"PLANNER_RESPONSE","created_at":%q,"tool_calls":[{"name":"read_file"},{"name":"run_command","args":{"CommandLine":"\"echo ready\""}},{"name":"schedule"},{"name":"run_workflow"},{"name":"invoke_subagent"}]}`+"\n", started)
	// The command's result arrives after its sibling's result.
	file := agyTestFile(t, dir, agyTestID, planner+
		agyBackgroundStep(3, "RUNNING", started, 2, "Cron: */30 * * * * Check status", "")+
		agyBackgroundStep(2, "RUNNING", started, 1, "Wait for ready", logURI)+
		agyBackgroundStep(4, "RUNNING", started, 3, "Run checks", "file:///unrelated/task-3.log")+
		agyBackgroundStep(5, "RUNNING", started, 4, "Review changes", ""))
	row := proto.Session{Status: proto.StatusIdle}
	n.mergeAgyTranscript(&row, agyTestID)
	if len(row.Background) != 4 || len(row.BackgroundEnded()) != 0 || row.Loop != nil {
		t.Fatalf("unexpected background state: %+v / %+v", row.Background, row.BackgroundEnded())
	}
	for i, kind := range []string{"shell", "monitor", "workflow", "agent"} {
		task := row.Background[i]
		if task.ID != fmt.Sprintf("%s/task-%d", agyTestID, i+1) || task.Kind != kind || !task.StartedAt.Equal(start) {
			t.Fatalf("task %d: %+v", i, task)
		}
	}
	if row.Background[0].Command != "echo ready" || row.Background[0].OutputFile != logPath || row.Background[1].Description != "Cron: */30 * * * * Check status" || row.Background[2].OutputFile != "" {
		t.Fatalf("lost or misattributed task metadata: %+v", row.Background)
	}

	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":6,"type":"SYSTEM_MESSAGE","created_at":%q,"content":%q}`+"\n", ended,
		fmt.Sprintf("<SYSTEM_MESSAGE>\n[Message] taskId=%[1]s/task-1 message=Task id \"%[1]s/task-1\" finished with result: Ready\n</SYSTEM_MESSAGE>", agyTestID)))
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":7,"type":"PLANNER_RESPONSE","created_at":%q,"tool_calls":[{"name":"manage_task","args":{"Action":"\"kill\"","TaskId":"\"%s/task-2\""}},{"name":"manage_task","args":{"Action":"status","TaskId":"%s/task-4"}}]}`+"\n", ended, agyTestID, agyTestID))
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":4,"type":"GENERIC","status":"ERROR","created_at":%q,"updated_at":%q,"content":"Workflow failed"}`+"\n", started, ended))
	n.mergeAgyTranscript(&row, agyTestID)
	if len(row.Background) != 1 || row.Background[0].Kind != "agent" {
		t.Fatalf("running tasks: %+v", row.Background)
	}
	outcomes := row.BackgroundEnded()
	if len(outcomes) != 3 {
		t.Fatalf("ended tasks: %+v", outcomes)
	}
	for i, status := range []string{"completed", "killed", "failed"} {
		if outcomes[i].Status != status || !outcomes[i].EndedAt.Equal(finish) {
			t.Fatalf("outcome %d: %+v", i, outcomes[i])
		}
	}
	if outcomes[0].OutputFile != logPath || outcomes[2].Description != "Run checks" {
		t.Fatalf("lost outcome metadata: %+v", outcomes)
	}

	for _, status := range []string{proto.StatusOffline, proto.StatusInterrupted} {
		row.Status = status
		n.mergeAgyTranscript(&row, agyTestID)
		if len(row.Background) != 0 || len(row.BackgroundEnded()) != 3 {
			t.Fatalf("stopped row %s: %+v / %+v", status, row.Background, row.BackgroundEnded())
		}
	}
	row.Status = proto.StatusIdle
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":8,"type":"SYSTEM_MESSAGE","created_at":%q,"content":%q}`+"\n", ended,
		fmt.Sprintf(`Task id "%s/task-4" was canceled.`, agyTestID)))
	n.mergeAgyTranscript(&row, agyTestID)
	if outcomes := row.BackgroundEnded(); len(row.Background) != 0 || len(outcomes) != 4 || outcomes[3].Status != "killed" {
		t.Fatalf("canceled subagent: %+v / %+v", row.Background, outcomes)
	}

	old := time.Now().UTC().Add(-2 * workflowKeep).Format(time.RFC3339)
	agyTestWrite(t, file, agyBackgroundStep(0, "RUNNING", old, 1, "Old task", "")+
		fmt.Sprintf(`{"step_index":1,"type":"SYSTEM_MESSAGE","created_at":%q,"content":%q}`+"\n", old,
			fmt.Sprintf(`Task id "%s/task-1" was canceled.`, agyTestID)))
	n.mergeAgyTranscript(&row, agyTestID)
	if len(row.Background) != 0 || len(row.BackgroundEnded()) != 0 {
		t.Fatalf("old outcome survived: %+v / %+v", row.Background, row.BackgroundEnded())
	}
}

func TestAgyBackgroundSessionListMerge(t *testing.T) {
	n, herdr, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	dir := n.agyDataRoot()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	logPath := filepath.Join(dir, "brain", agyTestID, ".system_generated", "tasks", "task-1.log")
	logURI := agyBackgroundLogURI(logPath)
	agyTestFile(t, dir, agyTestID, `{"step_index":0,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"schedule"}]}`+"\n"+
		agyBackgroundStep(1, "RUNNING", stamp, 1, "Cron: */30 * * * * Check status", logURI))

	const sid = "agy-" + agyTestID
	n.runtimes = newRuntimeRegistry()
	h := n.runtimes.add(nil)
	h.ready, h.hello.Runtime = true, agyRuntimeID
	setRuntimeRow := func(status string, running bool) {
		t.Helper()
		// Decode the companion's flat wire fields before normalizing as runtime.sessions does.
		var row proto.Session
		raw := fmt.Sprintf(`{"session_id":%q,"cwd":%q,"model":"gemini-test","status":%q,"background_ended":[{"id":%q,"kind":"workflow","status":"completed","ended_at":%q,"output_file":"/unrelated.log"}]}`,
			sid, home, status, agyTestID+"/task-2", stamp)
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			t.Fatal(err)
		}
		if running {
			row.Background = []proto.BackgroundTask{{ID: agyTestID + "/task-1", Kind: "monitor", OutputFile: "/unrelated.log"}}
		}
		n.runtimes.setSessions(h, normalizeRuntimeSessions(h, []proto.Session{row}))
	}
	setRuntimeRow(proto.StatusIdle, true)
	rows := listed(t, n)
	acp := rows[sid]
	if len(acp.Background) != 1 || acp.Background[0].Kind != "monitor" || acp.Background[0].OutputFile != logPath ||
		len(acp.BackgroundEnded()) != 1 || acp.BackgroundEnded()[0].Kind != "workflow" || acp.BackgroundEnded()[0].OutputFile != "" {
		t.Fatalf("ACP fields after normalization: %+v / %+v", acp.Background, acp.BackgroundEnded())
	}

	setRuntimeRow(proto.StatusIdle, false) // The companion sees a transcript without an ACP process.
	pane := agyTestAgent(agyTestPane, home, agyTestID)
	herdr.reply("session.snapshot", snapshot(nil, nil, []any{pane}, nil))
	rows = listed(t, n)
	row := rows[agyTestRow]
	if _, duplicate := rows[sid]; duplicate || row.Model != "gemini-test" || len(row.Background) != 1 || row.Background[0].Kind != "monitor" || row.Loop != nil {
		t.Fatalf("live pane merge: pane=%+v, duplicate=%v", row, duplicate)
	}

	setRuntimeRow(proto.StatusOffline, true)
	pane["agent_status"] = proto.StatusOffline
	herdr.reply("session.snapshot", snapshot(nil, nil, []any{pane}, nil))
	rows = listed(t, n)
	if len(rows[sid].Background) != 0 || len(rows[agyTestRow].Background) != 0 {
		t.Fatalf("stopped sessions show running work: %+v / %+v", rows[sid].Background, rows[agyTestRow].Background)
	}
}

func TestAgyBackgroundTaskLogRejectsEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	path := agyTestFile(t, dir, agyTestID, "")
	logPath := filepath.Join(filepath.Dir(filepath.Dir(path)), "tasks", "task-1.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.log")
	agyTestWrite(t, outside, "unrelated file")
	if err := os.Symlink(outside, logPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	brain, err := agyBrain(dir, agyTestID)
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	uri := agyBackgroundLogURI(logPath)
	if path := agyTaskLog(uri, dir, agyTestID, "task-1", brain); path != "" {
		t.Fatalf("accepted a symlink outside the conversation: %s", path)
	}
}

func TestAgyBackgroundOneShotTimersAndReconciliation(t *testing.T) {
	n := &Node{home: t.TempDir()}
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	dir := n.agyDataRoot()
	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	t0 := base.Format(time.RFC3339)
	t1 := base.Add(30 * time.Second).Format(time.RFC3339)
	t2 := base.Add(60 * time.Second).Format(time.RFC3339)
	t3 := base.Add(90 * time.Second).Format(time.RFC3339)

	// 1) task-1 (command), task-2 (one-shot timer with TimerCondition on task-1),
	//    task-3 (one-shot timer firing via its own sender message),
	//    task-4 (one-shot timer with future dueAt resolved dynamically at applyAt),
	//    task-5 (cron with MaxIterations=2), task-6 & task-7 (two commands ended in one SYSTEM_MESSAGE),
	//    task-8 (reconciled via manage_task list "No background tasks are currently running.").
	planner := fmt.Sprintf(`{"step_index":0,"type":"PLANNER_RESPONSE","created_at":%q,"tool_calls":[`+
		`{"name":"run_command","args":{"CommandLine":"\"sleep 5\""}},`+
		`{"name":"schedule","args":{"DurationSeconds":"240","Prompt":"\"Check task-1\"","TimerCondition":%q}},`+
		`{"name":"schedule","args":{"DurationSeconds":30,"Prompt":"\"Wake up\""}},`+
		`{"name":"schedule","args":{"DurationSeconds":"6000","Prompt":"\"Long timer\""}},`+
		`{"name":"schedule","args":{"CronExpression":"\"*/2 * * * *\"","MaxIterations":"2","Prompt":"\"Poll MR\""}},`+
		`{"name":"run_command","args":{"CommandLine":"\"cmd6\""}},`+
		`{"name":"run_command","args":{"CommandLine":"\"cmd7\""}},`+
		`{"name":"run_command","args":{"CommandLine":"\"cmd8\""}}`+
		`]}`+"\n", t0, agyTestID+"/task-1")
	file := agyTestFile(t, dir, agyTestID, planner+
		agyBackgroundStep(1, "RUNNING", t0, 1, "sleep 5", "")+
		agyBackgroundStep(2, "RUNNING", t0, 2, "Timer: 4m, Prompt: Check task-1", "")+
		agyBackgroundStep(3, "RUNNING", t0, 3, "Timer: 30s, Prompt: Wake up", "")+
		agyBackgroundStep(4, "RUNNING", t0, 4, "Timer: 1h 40m, Prompt: Long timer", "")+
		agyBackgroundStep(5, "RUNNING", t0, 5, "Cron: */2 * * * *, Prompt: Poll MR", "")+
		agyBackgroundStep(6, "RUNNING", t0, 6, "cmd6", "")+
		agyBackgroundStep(7, "RUNNING", t0, 7, "cmd7", "")+
		agyBackgroundStep(8, "RUNNING", t0, 8, "cmd8", ""))

	// task-1 finishes -> cancels task-2 via TimerCondition; task-3 fires via sender message; task-5 triggers once (1/2).
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":9,"type":"SYSTEM_MESSAGE","created_at":%q,"content":%q}`+"\n", t1,
		fmt.Sprintf("<SYSTEM_MESSAGE>\n[Message] timestamp=%[2]s sender=%[1]s/task-1 content=Task id \"%[1]s/task-1\" finished with result: ok\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=%[2]s sender=%[1]s/task-3 content=Wake up\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=%[2]s sender=%[1]s/task-5 content=Poll MR\n</SYSTEM_MESSAGE>", agyTestID, t1)))

	// Bundled SYSTEM_MESSAGE finishes task-6 and cancels task-7 in the same step; task-5 hits MaxIterations (2/2).
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":10,"type":"SYSTEM_MESSAGE","created_at":%q,"content":%q}`+"\n", t2,
		fmt.Sprintf("<SYSTEM_MESSAGE>\n[Message] timestamp=%[2]s sender=%[1]s/task-6 content=Task id \"%[1]s/task-6\" finished\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=%[2]s sender=%[1]s/task-7 content=Task id \"%[1]s/task-7\" was canceled\n</SYSTEM_MESSAGE>\n<SYSTEM_MESSAGE>\n[Message] timestamp=%[2]s sender=%[1]s/task-5 content=Poll MR\n</SYSTEM_MESSAGE>", agyTestID, t2)))

	meta, ok := n.agyTranscriptMeta(agyTestID)
	if !ok {
		t.Fatal("missing transcript meta")
	}
	var row proto.Session
	row.Status = proto.StatusIdle
	// Before task-4's 6000s dueAt: only task-4 and task-8 are still running.
	meta.background.applyAt(&row, base.Add(5*time.Minute))
	if len(row.Background) != 2 || row.Background[0].ID != agyTestID+"/task-4" || row.Background[1].ID != agyTestID+"/task-8" {
		t.Fatalf("expected task-4 and task-8 running, got: %+v", row.Background)
	}
	// After task-4's dueAt (cached meta without touching transcript on disk!): task-4 transitions to completed.
	meta.background.applyAt(&row, base.Add(2*time.Hour))
	if len(row.Background) != 1 || row.Background[0].ID != agyTestID+"/task-8" {
		t.Fatalf("expected cached due timer task-4 to expire at applyAt, got: %+v", row.Background)
	}

	// manage_task list reports no running tasks -> reconciles task-4 and task-8.
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":11,"type":"GENERIC","status":"DONE","created_at":%q,"content":"Created At: %s\nCompleted At: %s\nNo background tasks are currently running."}`+"\n", t3, t3, t3))
	n.mergeAgyTranscript(&row, agyTestID)
	if len(row.Background) != 0 {
		t.Fatalf("expected all tasks reconciled after empty manage_task list, got: %+v", row.Background)
	}
}

func TestAgyBackgroundServerRestartStopsOpenTasks(t *testing.T) {
	n := &Node{home: t.TempDir()}
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	dir := n.agyDataRoot()
	base := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	t0 := base.Format(time.RFC3339)
	t1 := base.Add(30 * time.Second).Format(time.RFC3339)
	tRestart := base.Add(60 * time.Second).Format(time.RFC3339)
	tAfter := base.Add(90 * time.Second).Format(time.RFC3339)

	planner := fmt.Sprintf(`{"step_index":0,"type":"PLANNER_RESPONSE","created_at":%q,"tool_calls":[`+
		`{"name":"run_command","args":{"CommandLine":"\"done-cmd\""}},`+
		`{"name":"run_command","args":{"CommandLine":"\"long-cmd\""}},`+
		`{"name":"schedule","args":{"CronExpression":"\"*/5 * * * *\"","Prompt":"\"Poll\""}}`+
		`]}`+"\n", t0)
	file := agyTestFile(t, dir, agyTestID, planner+
		agyBackgroundStep(1, "RUNNING", t0, 1, "done-cmd", "")+
		agyBackgroundStep(2, "RUNNING", t0, 2, "long-cmd", "")+
		agyBackgroundStep(3, "RUNNING", t0, 3, "Cron: */5 * * * *, Prompt: Poll", ""))

	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":4,"type":"SYSTEM_MESSAGE","created_at":%q,"content":%q}`+"\n", t1,
		fmt.Sprintf("<SYSTEM_MESSAGE>\n[Message] timestamp=%s sender=%s/task-1 content=Task id \"%s/task-1\" finished with result: ok\n</SYSTEM_MESSAGE>", t1, agyTestID, agyTestID)))
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":5,"type":"SYSTEM_MESSAGE","created_at":%q,"content":%q}`+"\n", tAfter,
		fmt.Sprintf("<SYSTEM_MESSAGE>\n[Message] timestamp=%s sender=system priority=MESSAGE_PRIORITY_LOW content=[Notice] All your subagents and background tasks have been stopped due to server restart. If you want a subagent to continue working, it needs to be revived by sending it a new message.\n</SYSTEM_MESSAGE>", tRestart)))
	agyTestAppend(t, file, fmt.Sprintf(`{"step_index":6,"type":"PLANNER_RESPONSE","created_at":%q,"tool_calls":[{"name":"run_command","args":{"CommandLine":"\"post-restart\""}}]}`+"\n", tAfter)+
		agyBackgroundStep(7, "RUNNING", tAfter, 4, "post-restart", ""))

	var row proto.Session
	row.Status = proto.StatusIdle
	n.mergeAgyTranscript(&row, agyTestID)
	if len(row.Background) != 1 || row.Background[0].ID != agyTestID+"/task-4" {
		t.Fatalf("expected only post-restart task-4 running, got: %+v", row.Background)
	}
	bgEnded := row.BackgroundEnded()
	ended := make(map[string]proto.BackgroundTask, len(bgEnded))
	for _, task := range bgEnded {
		ended[task.ID] = task
	}
	restartTime := base.Add(60 * time.Second)
	if task := ended[agyTestID+"/task-1"]; task.Status != proto.WorkflowCompleted || !task.EndedAt.Equal(base.Add(30*time.Second)) {
		t.Fatalf("expected task-1 to remain completed at t1, got: %+v", task)
	}
	for _, id := range []string{agyTestID + "/task-2", agyTestID + "/task-3"} {
		task, ok := ended[id]
		if !ok || task.Status != proto.WorkflowKilled || !task.EndedAt.Equal(restartTime) {
			t.Fatalf("expected %s killed at restart time %s, got: %+v", id, restartTime, task)
		}
	}
}
