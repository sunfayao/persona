package assistant

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/db"
)

// A todo's chat thread (todo_thread.go, #5279): bound, rebound, unbound; one per todo and one todo per thread; the
// apps get it with the todo and are told when it changes.

func threadOf(t *testing.T, e *todoEnv, id string) *TodoThread {
	t.Helper()
	got := e.get(id)
	if got == nil {
		t.Fatalf("todo %s is not listed", id)
	}
	return got.Thread
}

func TestTodoThreadRequests(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		id := e.create("发版").ID
		// A read-only device cannot write, whatever it sends; an unknown todo is 404.
		for _, c := range []struct{ method, path string }{{"PUT", "/api/v1/todos/" + id + "/thread"}, {"DELETE", "/api/v1/todos/" + id + "/thread"}} {
			if w := e.doAs("answer", c.method, c.path, `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`); w.Code != http.StatusForbidden {
				t.Fatalf("%s as a read-only role: %d", c.method, w.Code)
			}
		}
		e.must("PUT", "/api/v1/todos/"+id+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`)
		if w := e.doAs("answer", "DELETE", "/api/v1/todos/"+id+"/thread", ""); w.Code != http.StatusForbidden || threadOf(t, e, id) == nil {
			t.Fatalf("a read-only role unbound it: %d", w.Code)
		}
		if w := e.do("PUT", "/api/v1/todos/todo_gone/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`); w.Code != http.StatusNotFound {
			t.Fatalf("bind an unknown todo: %d", w.Code)
		}
		if w := e.do("DELETE", "/api/v1/todos/todo_gone/thread", ""); w.Code != http.StatusNotFound {
			t.Fatalf("unbind an unknown todo: %d", w.Code)
		}

		// Bad fields are 400 and leave the binding as it was.
		long := strings.Repeat("x", threadFieldMax+1)
		for _, body := range []string{`{`, `[]`, `{}`, `{"channel":"feishu","chat_id":"oc_test"}`, `{"channel":"  ","chat_id":"oc_test","thread_id":"t"}`,
			`{"channel":"feishu","chat_id":"","thread_id":"t"}`, `{"channel":"feishu","chat_id":"oc_test","thread_id":" \t"}`,
			`{"channel":"` + long + `","chat_id":"oc_test","thread_id":"t"}`, `{"channel":"feishu","chat_id":"` + long + `","thread_id":"t"}`,
			`{"channel":"feishu","chat_id":"oc_test","thread_id":"` + long + `"}`, `{"channel":1,"chat_id":"oc_test","thread_id":"t"}`} {
			if w := e.do("PUT", "/api/v1/todos/"+id+"/thread", body); w.Code != http.StatusBadRequest {
				t.Fatalf("%.60s: %d %s", body, w.Code, w.Body)
			}
		}
		if got := threadOf(t, e, id); got == nil || got.ThreadID != "omt_test_1" {
			t.Fatalf("after the refused binds: %+v", got)
		}
		// The title is a label: clipped (with an ellipsis, as the decisions' texts are), not refused.
		e.must("PUT", "/api/v1/todos/"+id+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1","title":"`+strings.Repeat("标", threadTitleMax+50)+`"}`)
		if got := threadOf(t, e, id); got == nil || len([]rune(got.Title)) != threadTitleMax+1 || !strings.HasSuffix(got.Title, "…") {
			t.Fatalf("long title: %+v", got)
		}
	})
}

// The binding is in the database: an assistant that starts afterwards has it, and a thread a todo holds there is taken
// even for an assistant that did not see the write.
func TestTodoThreadSurvivesRestart(t *testing.T) {
	database := todoTestDB(t)
	e := newTodoEnv(t, database)
	a, b := e.create("发版").ID, e.create("别的事").ID
	e.must("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1","title":"Release"}`)

	again := &todoEnv{t: t, s: newServiceOn(t, e.f, database), f: e.f}
	if got := threadOf(t, again, a); got == nil || got.Title != "Release" || got.ThreadID != "omt_test_1" {
		t.Fatalf("after a restart: %+v", got)
	}
	if got := threadOf(t, again, b); got != nil {
		t.Fatalf("the other todo has %+v", got)
	}

	// The first assistant's memory is stale (the second one unbinds and another todo binds the thread): the store
	// decides, and the refused bind leaves memory as it was.
	again.must("DELETE", "/api/v1/todos/"+a+"/thread", "")
	again.must("PUT", "/api/v1/todos/"+b+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`)
	if w := e.do("PUT", "/api/v1/todos/"+a+"/thread", `{"channel":"feishu","chat_id":"oc_test","thread_id":"omt_test_1"}`); w.Code != http.StatusConflict {
		t.Fatalf("a thread another todo holds in the store: %d %s", w.Code, w.Body)
	}
	if got := threadOf(t, e, a); got == nil || got.ThreadID != "omt_test_1" {
		t.Fatalf("memory after the refused bind: %+v", got)
	}
}
