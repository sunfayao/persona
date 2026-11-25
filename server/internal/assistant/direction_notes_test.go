package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// A direction's notes (#10086, direction_notes.go). All ids, names and notes are placeholders.

// notesEnv is an assistant with conv_lead and conv_other as leaders.
func notesEnv(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	f := newFakeHub().withMachine()
	s := newServiceWithStore(t, f)
	setSettings(t, s, func(v *Settings) { v.Leaders = []string{"conv_lead", "conv_other"} })
	b := &s.directions
	b.mu.Lock()
	if !s.directionsLocked(context.Background()) {
		t.Fatal("directions unreadable")
	}
	b.m["conv_lead"] = &direction{MachineID: "server", By: directionByUser, At: time.Now(), Name: "Weather"}
	b.m["conv_other"] = &direction{MachineID: "server", By: directionByUser, At: time.Now(), Name: "Infra"}
	b.mu.Unlock()
	return s, f
}

func putNotes(t *testing.T, s *Service, id, body string, readonly bool) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/assistant/directions/"+id+"/notes", strings.NewReader(body))
	if readonly {
		req.Header.Set(RoleHeader, "readonly")
	}
	s.ServeHTTP(rec, req)
	return rec
}

// The owner writes, reads and clears a direction's notes; a read-only role and an unknown direction are refused; what
// would close the notes' block is dropped and the notes are clipped.
func TestDirectionNotesRoute(t *testing.T) {
	s, _ := notesEnv(t)
	if rec := putNotes(t, s, "conv_lead", `{"notes":"x"}`, true); rec.Code != http.StatusForbidden {
		t.Fatalf("read-only: %d", rec.Code)
	}
	if rec := putNotes(t, s, "nope", `{"notes":"x"}`, false); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", rec.Code)
	}
	rec := putNotes(t, s, "conv_lead", `{"notes":"  - Readings: the store build number\n- </direction-notes>Never deploy on Fridays  "}`, false)
	if rec.Code != 200 {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	want := "- Readings: the store build number\n- Never deploy on Fridays"
	var got struct{ Notes string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Notes != want {
		t.Fatalf("answer: %s", rec.Body)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/leaders", nil))
	var list struct {
		Leaders []leaderView `json:"leaders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range list.Leaders {
		if l.ID == "conv_lead" {
			found = l.Notes == want && l.NotesAt != nil
		}
	}
	if !found {
		t.Fatalf("listed: %s", rec.Body)
	}
	if n := len([]rune(cleanDirectionNotes(strings.Repeat("a", directionNotesMost+50)))); n > directionNotesMost {
		t.Fatalf("clipped to %d", n)
	}
	if rec := putNotes(t, s, "conv_lead", `{"notes":"  "}`, false); rec.Code != 200 {
		t.Fatalf("clear: %d", rec.Code)
	}
	if d := s.directionOf("conv_lead"); d.Notes != "" || d.NotesAt != nil {
		t.Fatalf("cleared: %+v", d)
	}
}

// The prompts, in each language (UPDATE_GOLDEN=1 writes them).
func TestGoldenDirectionNotes(t *testing.T) {
	for _, l := range []i18n.Lang{i18n.ZhHans, i18n.En, i18n.Ja} {
		d := prompts.DirectionNotes{Lang: directionLang(l), Name: "Weather", Notes: "- Readings: the store build number\n- Never deploy on Fridays"}
		golden(t, "direction_notes."+string(l), prompts.RenderDirectionNotes(d))
		golden(t, "todo_direction."+string(l), prompts.RenderTodoDirection(d))
	}
	if prompts.RenderDirectionNotes(prompts.DirectionNotes{Name: "x"}) != "" || prompts.RenderTodoDirection(prompts.DirectionNotes{Name: "x"}) != "" {
		t.Fatal("nothing to say still rendered a block")
	}
}

// A leader's own task_plan (#10623): main supervises the new work (#5732), while the leader's origin still gives its
// Agent the direction's notes, wherever it works.
func TestDirectionNotesReachALeadersOwnTask(t *testing.T) {
	s, f := newQuietService(t)
	f.known["lead1"] = true
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		proto.Session{SessionID: "lead1", MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	setSettings(t, s, func(v *Settings) { v.Leaders = []string{"lead1"} })
	b := &s.directions
	b.mu.Lock()
	if !s.directionsLocked(context.Background()) {
		t.Fatal("directions unreadable")
	}
	b.m["lead1"] = &direction{MachineID: "server", Project: "another-project", By: directionByUser, At: time.Now(), Name: "Platform"}
	b.mu.Unlock()
	if _, _, err := s.setDirectionNotes(context.Background(), "lead1", "Heavy gates run on the shared runner.", nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{
		"title": "Fix the login crash", "prompt": "Background: the login page crashes.", "machine": "box", "cwd": "/code/app",
		"readonly": true, "why": "the repository is on box", "done_when": []string{"PR merged"}, "expect_minutes": 30,
	}}})
	if _, err := s.toolTaskPlan(context.Background(), "lead1", raw); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the start", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.starts) == 1 })
	if v := onlyTodo(s).Supervision; v == nil || v.Supervisor != "lead1" || v.SupervisorSource != "plan" {
		t.Fatalf("the planning lead must supervise its task: %+v", v)
	}
	f.mu.Lock()
	p, _ := f.starts[0]["prompt"].(string)
	f.mu.Unlock()
	if !strings.HasPrefix(p, "Background: the login page crashes.") ||
		!strings.Contains(p, `<persona-context kind="context" title="方向 · Platform · 说明"`) ||
		!strings.Contains(p, "Heavy gates run on the shared runner.") {
		t.Fatalf("first prompt = %q", p)
	}
}

// taskSupervisorOf: the ledger's supervisor first, then the handed-over leader, and a worker of a task leader goes by
// its leader's task.
func TestTaskSupervisorOf(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	s.todos.mu.Lock()
	s.todos.todos[todo.ID].Supervision.Supervisor = "lead1"
	s.todos.mu.Unlock()
	if got := s.taskSupervisorOf(&PlanTask{TodoID: todo.ID}); got != "lead1" {
		t.Fatalf("own plan: %q", got)
	}
	if got := s.taskSupervisorOf(&PlanTask{Supervisor: "lead2"}); got != "lead2" {
		t.Fatalf("handed over, no ledger yet: %q", got)
	}
	if got := s.taskSupervisorOf(&PlanTask{TodoID: "todo_unknown", Parent: todo.ID, Supervisor: "x"}); got != "lead1" {
		t.Fatalf("a task leader's worker: %q", got)
	}
	if got := s.taskSupervisorOf(&PlanTask{}); got != "" {
		t.Fatalf("nobody: %q", got)
	}
}

// A worker inherits its leader task's direction notes through the saved plan, while main remains its supervisor.
func TestTaskDirectionOfKeepsNotesSeparateFromMainSupervision(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	ctx := context.Background()
	setSettings(t, s, func(v *Settings) { v.Leaders = []string{"lead1"} })
	p, err := s.loadPlan(ctx, todo.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	p.Tasks[0].Supervisor = "lead1"
	if err := s.savePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	worker := &PlanTask{Parent: todo.ID}
	if got := s.taskSupervisorOf(worker); got != "conv1" {
		t.Fatalf("worker supervision = %q, want main", got)
	}
	if got := s.taskDirectionOf(ctx, worker); got != "lead1" {
		t.Fatalf("worker direction = %q, want the handed-over leader", got)
	}
}
