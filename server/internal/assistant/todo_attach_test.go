package assistant

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestAttachUnassignedTodoAgents(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, database *db.DB) {
		first := e.create("Release")
		second := e.create("Review")
		active := proto.Session{SessionID: "run-a", Title: "First agent", Agent: "codex"}
		other := proto.Session{SessionID: "run-b", Title: "Second agent", Agent: "claude"}
		archive := proto.Session{SessionID: "gone", Title: "Archived agent", Agent: "codex", Archived: true}
		e.sessions("m1", active, other, archive)
		attach := func(todoID, sessionID string) string {
			return "/api/v1/todos/" + todoID + "/agents/m1/" + sessionID + "/attach"
		}
		e.must("PUT", attach(first.ID, "run-a"), "")
		e.must("PUT", attach(first.ID, "run-b"), "")
		find := func(a TodoAgent) bool { return a.SessionID == "run-a" }
		before := e.get(first.ID).Agents
		firstLink := before[slices.IndexFunc(before, find)].LinkedAt
		e.must("PUT", attach(first.ID, "run-a"), "")
		record := e.get(first.ID)
		index := slices.IndexFunc(record.Agents, find)
		if len(record.Agents) != 2 || index < 0 || !record.Agents[index].LinkedAt.Equal(firstLink) {
			t.Fatalf("repeat changed the roster: %+v", record.Agents)
		}
		if w := e.do("PUT", attach(second.ID, "run-a"), ""); w.Code != http.StatusConflict ||
			!strings.Contains(w.Body.String(), "todo_agent_link_conflict") {
			t.Fatalf("attachment moved a link: %d %s", w.Code, w.Body)
		}
		if w := e.doAs(roleExtension, "PUT", attach(first.ID, "run-b"), ""); w.Code != http.StatusForbidden {
			t.Fatalf("restricted role could attach: %d %s", w.Code, w.Body)
		}
		if w := e.do("PUT", attach(first.ID, "gone"), ""); w.Code != http.StatusNotFound {
			t.Fatalf("attachment accepted an archived agent: %d %s", w.Code, w.Body)
		}
		if database != nil {
			reloadTodos(t, e.s)
		}
		if got := e.todoOf("m1", "run-a"); got != first.ID {
			t.Fatalf("attachment changed the original owner to %q", got)
		}
		e.must("DELETE", "/api/v1/todos/"+first.ID+"/agents/m1/run-a", "")
		e.must("PUT", attach(first.ID, "run-a"), "")
		if got := e.todoOf("m1", "run-a"); got != first.ID {
			t.Fatalf("could not reattach an agent after unlinking it: %q", got)
		}
		e.must("PUT", "/api/v1/todos/"+second.ID+"/agents/m1/run-a", "")
		if got := e.todoOf("m1", "run-a"); got != second.ID {
			t.Fatalf("the explicit move route no longer moves ownership: %q", got)
		}
	})
}
