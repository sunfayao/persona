package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// ryokanDecision is a complete needs_user review, the fixtures' "Which ryokan?", which can be undone.
func ryokanDecision() map[string]any {
	return map[string]any{"verdict": "needs_user", "why_owner": "priority", "reason": "Which ryokan?", "recommend": "Ryokan Hanami",
		"options": []map[string]string{
			{"label": "Ryokan Hanami", "leads_to": "Four minutes from Gion, with a late check-in"},
			{"label": "Kaede", "leads_to": "The cheapest, twenty minutes out"},
			{"label": "Tsuki", "leads_to": "The quietest, at the top of the budget"},
		},
		"cost_of_waiting": "Rooms for that weekend sell out"}
}

// viewRound is the time in the id of the ask to-do id's row lists with prefix: the round the replay answers.
func viewRound(t *testing.T, s *Service, id, prefix string) string {
	t.Helper()
	for _, ask := range rowAsks(t, s, id) {
		if round, ok := strings.CutPrefix(ask, prefix); ok {
			return round
		}
	}
	t.Fatalf("to-do %s lists no %s ask: %v", id, prefix, rowAsks(t, s, id))
	return ""
}

func rowAsks(t *testing.T, s *Service, id string) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/todos/"+id, nil))
	var out struct{ Todo listedTodo }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Todo.View == nil {
		t.Fatalf("GET /api/v1/todos/%s: %d %s", id, rec.Code, rec.Body)
	}
	var ids []string
	for _, a := range out.Todo.View.Asks {
		ids = append(ids, a.ID)
	}
	return ids
}

// Conversation answers must target a task in that conversation and reach its agent.
func TestConversationRouteAnswerUnchanged(t *testing.T) {
	s, f, todo := newDecisionAnswerServiceOn(t, todoTestDB(t))
	waitsForReview(t, s, f, todo.ID, "Three ryokan fit the dates.")
	if _, err := reviewErr(s, todo.ID, ryokanDecision()); err != nil {
		t.Fatal(err)
	}
	const prefix = "todo:decision@"
	round := viewRound(t, s, todo.ID, prefix)
	post := func(conversation string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]string{"todo_id": todo.ID, "text": "Hanami", "asked_at": round})
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/assistant/conversations/"+conversation+"/route", strings.NewReader(string(raw))))
		return rec
	}
	rec := post("another-conversation")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), i18n.Err("assistant-tasks.route_no_task").Error()) {
		t.Fatalf("another conversation: %d %s", rec.Code, rec.Body)
	}
	f.setSessions("box")
	if rec := post("conv1"); rec.Code != http.StatusConflict {
		t.Fatalf("no Agent: %d %s", rec.Code, rec.Body)
	}
	rows, err := s.todoDB().ListTodoDecisions(context.Background(), todo.ID, true)
	if err != nil || len(rows) != 0 || !slices.Contains(rowAsks(t, s, todo.ID), prefix+round) {
		t.Fatalf("the conversation route took words no Agent received: %+v, %v", rows, err)
	}
	s.waitWorkEvents()
}
