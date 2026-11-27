package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistant"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
)

// The in-process hub supplies the single test machine and routes device calls to
// the real node handler. No model or hub service is needed by the Claude fixture.
type schedulePromptHub struct {
	node    *Node
	offline bool
	locale  i18n.Lang
}

func (h schedulePromptHub) Hello(context.Context) (assistant.Hello, error) {
	id := h.node.machineID
	return assistant.Hello{Assistant: assistant.Placement{MachineID: id, Machines: []string{id}, Launcher: "claude"}, Locale: string(h.locale)}, nil
}

func (h schedulePromptHub) DeviceCall(ctx context.Context, p assistant.DeviceCall) (json.RawMessage, error) {
	if p.Device == assistant.DeviceAll && p.Method == assistant.DeviceDevicesList {
		d := assistant.Device{ID: h.node.machineID, Name: "Fixture", Home: h.node.home, DataDir: h.node.dir,
			Online: !h.offline, Reachable: !h.offline, ModelConfiguration: true}
		return json.Marshal(map[string]any{"devices": []assistant.Device{d}})
	}
	if p.Device != h.node.machineID {
		return nil, fmt.Errorf("test machine does not exist: %s", p.Device)
	}
	// The hub normally answers this from the node's session inventory.
	if p.Method == assistant.DeviceSessionGet {
		var req struct {
			ID string `json:"session_id"`
		}
		if err := json.Unmarshal(p.Params, &req); err != nil {
			return nil, err
		}
		for _, session := range h.node.sessionList(ctx, true) {
			if session.SessionID == req.ID {
				session.Assistant = h.node.isAssistantSession(req.ID)
				return json.Marshal(map[string]any{"ok": true, "session": session})
			}
		}
		return json.RawMessage(`{"ok":false}`), nil
	}
	result, err := h.node.handle(ctx, p.Method, p.Params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (schedulePromptHub) CredentialRevoked(context.Context, string) (bool, error) { return false, nil }
func (schedulePromptHub) AppCall(context.Context, assistant.AppCall) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}
func (schedulePromptHub) NotifyRequest(context.Context, assistant.NotifyRequest) (assistant.NotifyResult, error) {
	return assistant.NotifyResult{}, nil
}
func (schedulePromptHub) Broadcast(context.Context, assistant.Broadcast) {}
func (schedulePromptHub) Snapshot(context.Context, assistant.Snapshot) (assistant.SnapshotResult, error) {
	return assistant.SnapshotResult{}, nil
}
func (schedulePromptHub) InboxChanged(context.Context, assistantwire.InboxChanged) {}
func (schedulePromptHub) Tell(string, any) error                                   { return nil }

func TestScheduledInboxSourceRefusesSelectedFilesWithoutChangingTask(t *testing.T) {
	f := cliTestBash(t)
	f.n.machineID = "inbox-fixture"
	cwd := resolved(t, t.TempDir())
	writeFile(t, filepath.Join(cwd, "notes.txt"), "Synthetic notes")
	selection := fmt.Sprintf(`<persona-context kind="file" path="notes.txt" machine="%s" cwd="%s" launcher="claude">Notes</persona-context>`, f.n.machineID, cwd)
	raw, err := nodeCall(t, f.n, "assistant.send", map[string]any{"cwd": cwd, "launcher": "claude", "text": "Start a fixture chat"})
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &source); err != nil || source.SessionID == "" {
		t.Fatalf("start fixture: %s (%v)", raw, err)
	}
	f.idle(t, source.SessionID)

	service := assistant.New(assistant.Options{Hub: schedulePromptHub{node: f.n}})
	result := service.Tool(t.Context(), assistant.ToolCall{
		Name: "schedule_followup", MachineID: f.n.machineID, SessionID: source.SessionID,
		Arguments: json.RawMessage(`{"at":"30m","title":"Review the fixture","prompt":"Review notes later"}`),
	})
	if result.IsError {
		t.Fatalf("schedule the existing follow-up: %+v", result)
	}
	call := func(method, route string, fields map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		service.ServeHTTP(rec, httptest.NewRequest(method, route, bytes.NewReader(raw)).WithContext(t.Context()))
		return rec
	}
	getTask := func() assistant.Task {
		t.Helper()
		rec := call(http.MethodGet, "/api/v1/assistant/tasks", nil)
		var list struct{ Tasks []assistant.Task }
		if err := json.Unmarshal(rec.Body.Bytes(), &list); rec.Code != http.StatusOK || err != nil {
			t.Fatalf("follow-ups: %d %s (%v)", rec.Code, rec.Body, err)
		}
		index := slices.IndexFunc(list.Tasks, func(task assistant.Task) bool { return task.Title == "Review the fixture" })
		if index < 0 {
			t.Fatal("saved follow-up missing")
		}
		return list.Tasks[index]
	}
	task := getTask()
	route := "/api/v1/assistant/tasks/" + task.ID
	original := selection + "\n\nReview the selected notes."
	if rec := call(http.MethodPut, route, map[string]any{"structured_prompt": original}); rec.Code != http.StatusOK {
		t.Fatalf("ordinary source could not save a file selection: %d %s", rec.Code, rec.Body)
	}

	// An existing saved selection is checked again when the source acquires the Inbox policy.
	state, ok := f.n.state.get(source.SessionID)
	if !ok {
		t.Fatal("source has no Node state")
	}
	state.Origin, state.OriginKnown = originInbox, true
	f.n.state.put(source.SessionID, state)
	if rec := call(http.MethodPut, route, map[string]any{"structured_prompt": selection + "\n\nEdited prompt"}); rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "file_unavailable") {
		t.Errorf("Inbox source did not refuse the edit with the file error: %d %s", rec.Code, rec.Body)
	}
	if kept := getTask(); kept.Prompt != original || !kept.PromptSelections || kept.SessionID != source.SessionID || kept.Title != task.Title {
		t.Errorf("refused edit changed the saved task: %+v", kept)
	}
	if rec := call(http.MethodPost, route+"/run", nil); rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "file_unavailable") {
		t.Errorf("Inbox source did not refuse to run the saved selection: %d %s", rec.Code, rec.Body)
	}
	if inputs := emptyStartUserInputs(t, f.file("stdin.jsonl")); len(inputs) != 1 {
		t.Fatalf("file selection reached a turn: %+v", inputs)
	}

	const plain = "Review the fixture without a file"
	if rec := call(http.MethodPut, route, map[string]any{"prompt": plain}); rec.Code != http.StatusOK {
		t.Fatalf("ordinary prompt save: %d %s", rec.Code, rec.Body)
	}
	rec := call(http.MethodPost, route+"/run", nil)
	var run struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &run); rec.Code != http.StatusOK || err != nil || run.SessionID != source.SessionID {
		t.Fatalf("ordinary prompt did not continue its Inbox source: %d %s (%v)", rec.Code, rec.Body, err)
	}
	f.idle(t, run.SessionID)
	if inputs := emptyStartUserInputs(t, f.file("stdin.jsonl")); len(inputs) != 2 {
		t.Fatalf("expected one initial and one ordinary follow-up turn, got %d", len(inputs))
	}
}

func TestScheduledRuntimeSourceRejectsFileSelection(t *testing.T) {
	n, hub := runtimeNode(t)
	n.machineID = "runtime-fixture"
	assistantHub(hub)
	host := dialRuntime(t, n, assistantRuntimeHello())
	cwd := filepath.Join(n.home, ".persona", "assistant")
	const id = "session-a1"
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": id, "cwd": cwd, "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	service := assistant.New(assistant.Options{Hub: schedulePromptHub{node: n}})
	source, err := service.Start(t.Context(), assistant.StartParams{Launcher: "dsh", MachineID: n.machineID,
		Cwd: cwd, Text: "Review the notes later", Untitled: true})
	if err != nil || source.SessionID != id {
		t.Fatalf("start runtime source: %+v (%v)", source, err)
	}
	writeFile(t, filepath.Join(cwd, "notes.txt"), "Synthetic notes")

	uuid := newUUID()
	if err := host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": id, "cwd": cwd, "started_by_persona": true, "status": "idle"},
		{"session_id": uuid, "cwd": cwd, "started_by_persona": true, "status": "idle"},
	}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "both runtime conversations listed", func() bool {
		_, _, first := n.runtimes.rowOf(id)
		_, _, second := n.runtimes.rowOf(uuid)
		return first && second
	})

	const refusalKey = "assistant-schedule.file_runtime_unsupported"
	want := i18n.T(i18n.Default, refusalKey)
	selection := func(launcher string) string {
		return fmt.Sprintf(`<persona-context kind="file" path="notes.txt" machine="%s" cwd="%s" launcher="%s">Notes</persona-context>`, n.machineID, cwd, launcher)
	}
	for _, sid := range []string{id, uuid} {
		_, err := nodeCall(t, n, "assistant.send", map[string]any{
			"session_id": sid, "cwd": cwd, "text": selection("dsh"),
			"task_execution": true, "task_input": true, "resume_only": true,
		})
		if refusal := assertFileRefusalDelivery(t, err); !strings.Contains(refusal.Message, want) {
			t.Errorf("selected send on a runtime-owned conversation: %v", err)
		}
		_, err = nodeCall(t, n, "assistant.content", map[string]any{
			"session_id": sid, "cwd": cwd, "launcher": "claude", "text": selection("claude"), "task_execution": true,
		})
		if refusal := assertFileRefusalDelivery(t, err); !strings.Contains(refusal.Message, want) {
			t.Errorf("content approval on a runtime-owned conversation: %v", err)
		}
	}

	args := json.RawMessage(`{"at":"30m","title":"Review","prompt":"Review the notes"}`)
	if result := service.Tool(t.Context(), assistant.ToolCall{
		Name: "schedule_followup", Arguments: args, MachineID: n.machineID, SessionID: id,
	}); result.IsError {
		t.Fatalf("could not save an ordinary follow-up: %+v", result)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload)).WithContext(t.Context())
		req.Header.Set("Accept-Language", string(i18n.Default))
		w := httptest.NewRecorder()
		service.ServeHTTP(w, req)
		return w
	}
	getTask := func() assistant.Task {
		t.Helper()
		w := request(http.MethodGet, "/api/v1/assistant/tasks", nil)
		var list struct{ Tasks []assistant.Task }
		if err := json.Unmarshal(w.Body.Bytes(), &list); w.Code != 200 || err != nil {
			t.Fatalf("follow-ups: %d %s (%v)", w.Code, w.Body, err)
		}
		index := slices.IndexFunc(list.Tasks, func(task assistant.Task) bool { return task.Title == "Review" })
		if index < 0 {
			t.Fatal("saved follow-up missing")
		}
		return list.Tasks[index]
	}
	task := getTask()

	preview := request(http.MethodPost, "/api/v1/assistant/tasks/input-context", map[string]any{"id": task.ID, "catalog": true})
	var result struct {
		TaskInput *bool  `json:"task_input"`
		Error     string `json:"input_error"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &result); preview.Code != 200 || err != nil || result.TaskInput == nil || *result.TaskInput || !strings.Contains(result.Error, want) {
		t.Errorf("preview did not explain the unavailable picker: %d %s (%v)", preview.Code, preview.Body, err)
	}
	path := "/api/v1/assistant/tasks/" + task.ID
	save := request(http.MethodPut, path, map[string]any{"structured_prompt": selection("dsh")})
	if save.Code == 200 || !strings.Contains(save.Body.String(), want) {
		t.Fatalf("save did not refuse with the runtime reason: %d %s", save.Code, save.Body)
	}
	if kept := getTask(); kept.Prompt != task.Prompt || kept.PromptSelections {
		t.Fatal("file selection modified the saved task")
	}

	before := len(host.got("session.send"))
	run := request(http.MethodPost, path+"/run", nil)
	sends := host.got("session.send")
	if run.Code != 200 || len(sends) != before+1 || !strings.Contains(string(sends[before]), task.Prompt) || len(host.got("session.start")) != 1 {
		t.Fatalf("plain follow-up did not continue its runtime: %d %s", run.Code, run.Body)
	}
}
