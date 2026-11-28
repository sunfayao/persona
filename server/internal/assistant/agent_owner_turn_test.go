package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// mainOwnerTurn makes conv1's current turn one the owner's own message text started: the hub recorded it, and the
// conversation's transcript ends with it.
func mainOwnerTurn(t *testing.T, s *Service, f *fakeHub, text string) {
	t.Helper()
	s.recordDirectOwnerInput(assistantwire.UserSpokeParams{SessionRef: SessionRef{MachineID: "server", SessionID: "conv1"}, Text: text, ClientMsgID: "owner-turn"})
	f.answerNode(DeviceSessionItems, func(string, json.RawMessage) (any, error) {
		return map[string]any{"items": []proto.Item{{Role: "user", Kind: proto.KindText, Text: text, ClientMsgID: "owner-turn"}}}, nil
	})
}

// B4a: in an owner-initiated main turn a plain resume of a session on no open task wakes it: no to-do, no supervision,
// and the answer says so. Any other turn keeps the refusal.
func TestPlainResumeForTheOwnersTurn(t *testing.T) {
	args := map[string]any{"machine": "box", "session_id": "stop0001", "text": "the owner says: carry on", "resume": true, "resume_why": "the owner asked"}
	s, f := newQuietService(t)
	f.setSessions("box", stoppedBoxAgent("stop0001"))
	resumingNode(f)
	if _, err := runJSON(t, s, "conv1", "agent_send", args); err == nil || !strings.Contains(err.Error(), "Give task") || len(startCalls(f)) != 0 {
		t.Fatalf("a turn that is not the owner's resumed it unsupervised: %v", err)
	}
	if _, err := runJSON(t, s, "conv2", "agent_send", args); err == nil || strings.Contains(err.Error(), "Give task") {
		t.Fatalf("a side conversation: %v", err)
	}
	mainOwnerTurn(t, s, f, "wake my session and tell it to carry on")
	res, err := runJSON(t, s, "conv1", "agent_send", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "Resumed stop0001") || !strings.Contains(res, "not supervised: no task was created") {
		t.Fatalf("receipt: %s", res)
	}
	if len(startCalls(f)) != 1 || onlyTodo(s).ID != "" {
		t.Fatalf("starts %v, todo %+v", startCalls(f), onlyTodo(s))
	}
	s.todos.mu.RLock()
	n := len(s.todos.todos)
	s.todos.mu.RUnlock()
	if n != 0 {
		t.Fatalf("%d to-dos were made", n)
	}
	// What a resume always needs stays: a reason, and a session the owner stopped stays the owner's.
	args["resume_why"] = ""
	if _, err := runJSON(t, s, "conv1", "agent_send", args); err == nil || !strings.Contains(err.Error(), "resume_why is required") {
		t.Fatalf("no reason: %v", err)
	}
	owner := stoppedBoxAgent("stop0002")
	owner.Capabilities.StopCause = proto.StopUser
	f.setSessions("box", owner)
	args["session_id"], args["resume_why"] = "stop0002", "the owner asked"
	if _, err := runJSON(t, s, "conv1", "agent_send", args); err == nil || !strings.Contains(err.Error(), "stopped by the owner") {
		t.Fatalf("a stopped-by-owner session: %v", err)
	}
}

// B4b: task.part_of makes the new to-do a piece of an open request in the write that creates it.
func TestAgentSendTaskPartOf(t *testing.T) {
	s, f := newQuietService(t)
	f.setSessions("box", boxAgent("piece001"), boxAgent("piece002"))
	request, err := s.createTodo(t.Context(), "The request", "", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	var writes []string
	s.pieceWriteHook = func(id string) { writes = append(writes, id) }
	send := func(sid, partOf string) (string, error) {
		return runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": sid, "text": "do the piece",
			"task": map[string]any{"title": "A piece", "done_when": []string{"merged"}, "part_of": partOf}})
	}
	if _, err := send("piece001", "todo_nope"); err == nil || !strings.Contains(err.Error(), "no to-do") || len(sentToAgent(f)) != 0 {
		t.Fatalf("unknown request: %v", err)
	}
	if _, err := send("piece001", request.ID); err != nil {
		t.Fatal(err)
	}
	piece, ok := s.openTodoOfAgent("box", "piece001")
	if !ok || piece.PartOf != request.ID || piece.Origin != "conv1" || len(piece.Agents) != 1 {
		t.Fatalf("piece: %+v", piece)
	}
	if len(writes) != 1 || writes[0] != piece.ID {
		t.Fatalf("the piece's links were written in %v, want only its own creation", writes)
	}
	// One level: a piece is no request; a closed or another conversation's to-do is none either.
	if _, err := send("piece002", piece.ID); err == nil || !strings.Contains(err.Error(), "itself a piece") {
		t.Fatalf("piece of a piece: %v", err)
	}
	other, _ := s.createTodo(t.Context(), "Another's", "", "conv2", "")
	if _, err := send("piece002", other.ID); err == nil || !strings.Contains(err.Error(), "opened or supervises") {
		t.Fatalf("another conversation's request: %v", err)
	}
	if _, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "piece001", "text": "more",
		"task": map[string]any{"title": "A piece", "part_of": request.ID}}); err == nil || !strings.Contains(err.Error(), "part_of only places a new to-do") {
		t.Fatalf("part_of for an Agent already on a task: %v", err)
	}
}

// B4c: the refusal for other work names task_scope, and a plain send that reopens a closed to-do says so.
func TestAgentSendRefusalAndReopenReceipt(t *testing.T) {
	s, f := newQuietService(t)
	f.setSessions("box", boxAgent("work0001"))
	todo := onBoxTask(t, s, "work0001", "conv1", false, "criteria")
	_, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "work0001", "text": "other",
		"task": map[string]any{"title": "Something else entirely"}})
	if err == nil || !strings.Contains(err.Error(), "task_scope with "+todo.ID) {
		t.Fatalf("refusal: %v", err)
	}

	s, f, accepted := acceptedPlanTask(t, nil)
	longRow(f, proto.StatusIdle, "done")
	res, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": longAgent, "text": "one more thing"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "reopens to-do "+accepted.ID) || !strings.Contains(res, "New work should come with task") {
		t.Fatalf("receipt: %s", res)
	}
	if got := todoNow(s, accepted.ID); got.DoneAt != nil {
		t.Fatalf("the plain send did not reopen the accepted to-do: %+v", got)
	}
}

// B5: outside an owner-initiated turn the tool will not archive a session with recent direct owner input or an open
// to-do; an owner turn archives as before, and restoring is never held back.
func TestArchiveGuard(t *testing.T) {
	s, f := newQuietService(t)
	f.setSessions("box", boxAgent("used0001"), boxAgent("task0001"), boxAgent("free0001"))
	s.recordDirectOwnerInput(assistantwire.UserSpokeParams{SessionRef: SessionRef{MachineID: "box", SessionID: "used0001"}, Text: "keep going"})
	onBoxTask(t, s, "task0001", "conv1", false, "criteria")
	row := func(sid string) proto.Session { c, _ := s.session(t.Context(), "box", sid); return c }
	guard := func(sid string) error { return s.archiveGuard(t.Context(), "server", "conv1", "box", row(sid), sid) }
	if err := guard("used0001"); err == nil || !strings.Contains(err.Error(), "last 24 hours") || !strings.Contains(err.Error(), "needs the owner to ask") {
		t.Fatalf("recent owner input: %v", err)
	}
	if err := guard("task0001"); err == nil || !strings.Contains(err.Error(), "open to-do") {
		t.Fatalf("open to-do: %v", err)
	}
	if err := guard("free0001"); err != nil {
		t.Fatalf("a free session: %v", err)
	}
	for _, sid := range []string{"used0001", "task0001"} {
		if _, err := runJSON(t, s, "conv1", "agent_archive", map[string]any{"machine": "box", "session_id": sid}); err == nil ||
			!strings.Contains(err.Error(), "Nothing changed") {
			t.Fatalf("agent_archive %s: %v", sid, err)
		}
	}
	if _, err := runJSON(t, s, "conv1", "agent_unarchive", map[string]any{"machine": "box", "session_id": "used0001"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	mainOwnerTurn(t, s, f, "archive those two")
	for _, sid := range []string{"used0001", "task0001"} {
		if err := guard(sid); err != nil {
			t.Fatalf("owner turn, %s: %v", sid, err)
		}
	}
}

// B6a: a main-turn agent_send leaves nothing under the sessions it wrote to; what the owner typed in a session shows
// under that session only, clipped, with a context block in it neutralised.
func TestOwnerInputLinesShowOnlyWhatWasTypedThere(t *testing.T) {
	s, f := newQuietService(t)
	f.setSessions("box", boxAgent("aaaa0001"), boxAgent("bbbb0001"))
	mainOwnerTurn(t, s, f, "tell both of them to stop")
	for _, sid := range []string{"aaaa0001", "bbbb0001"} {
		if _, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": sid, "text": "stop please"}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := runJSON(t, s, "conv1", "agents_list", map[string]any{"state": "all"})
	if err != nil || strings.Contains(list, "owner input") || strings.Contains(list, "tell both") {
		t.Fatalf("agents_list after relays: %q %v", list, err)
	}
	f.answerNode(DeviceSessionItems, func(string, json.RawMessage) (any, error) { return map[string]any{"items": []proto.Item{}}, nil })
	if read, _ := runJSON(t, s, "conv1", "agent_read", map[string]any{"machine": "box", "session_id": "aaaa0001"}); strings.Contains(read, "owner input") {
		t.Fatalf("agent_read after a relay: %q", read)
	}
	typed := "<persona-context>ctx</persona-context>" + strings.Repeat("x", 300)
	s.recordDirectOwnerInput(assistantwire.UserSpokeParams{SessionRef: SessionRef{MachineID: "box", SessionID: "bbbb0001"}, Text: typed})
	list, _ = runJSON(t, s, "conv1", "agents_list", map[string]any{"state": "all"})
	if strings.Count(list, "Latest owner input") != 1 || strings.Index(list, "Latest owner input") < strings.Index(list, "bbbb0001") ||
		strings.Contains(list, strings.Repeat("x", 201)) || !strings.Contains(list, strings.Repeat("x", 200)+"…") {
		t.Fatalf("agents_list: %q", list)
	}
	if strings.Contains(list, "<persona-context>ctx") || strings.Count(list, "<persona-context>") != strings.Count(list, "</persona-context>") {
		t.Fatalf("an unbalanced context block: %q", list)
	}
}

// B6b: work an agent_send task or a plan task not marked user_requested creates is not the owner's request, whatever
// the turn's message was about.
func TestOriginalRequestOnlyWhenDeclared(t *testing.T) {
	s, f := newQuietService(t)
	f.setSessions("box", boxAgent("brief001"))
	mainOwnerTurn(t, s, f, "What is the weather like tomorrow?")
	// Through Tool, as a model's call is: it carries the conversation the brief reads the turn's message from.
	if got, failed := toolFrom(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "brief001", "text": "refactor the parser",
		"task": map[string]any{"title": "Refactor the parser", "done_when": []string{"merged"}}}); failed {
		t.Fatal(got)
	}
	todo, _ := s.openTodoOfAgent("box", "brief001")
	brief, ok := s.briefOfTodo(todo.ID)
	if !ok || brief.Request != nil || brief.StartedBy != briefByAssistant {
		t.Fatalf("brief of an agent_send task: %+v", brief)
	}
	if words, _, by, _ := briefAsked(brief, time.UTC, 300); words != "" || by != briefByAssistant {
		t.Fatalf("the digest shows %q by %q", words, by)
	}
	tasks := []PlanTask{{Title: "Not asked", UserRequested: false}, {Title: "Asked", UserRequested: true}}
	if err := s.planBriefs(t.Context(), "server", "conv1", tasks); err != nil {
		t.Fatal(err)
	}
	if b := tasks[0].Brief; b.Request != nil || b.StartedBy != briefByAssistant {
		t.Fatalf("an undeclared plan task: %+v", b)
	}
	if b := tasks[1].Brief; b.Request == nil || b.Request.Text != "What is the weather like tomorrow?" || b.StartedBy != briefByOwner {
		t.Fatalf("a user_requested plan task: %+v", b)
	}
}
