package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/db"
)

func TestTodoResponsibleAPIAndPersistence(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, database *db.DB) {
		// Create with a session responsible party (kind lowercased, fields trimmed).
		w := e.must("POST", "/api/v1/todos", `{"title":"Monitor rollout","responsible":{"kind":" Session ","name":" deploy-watch ","machine_id":" host-1 ","session_id":" sess-9 "}}`)
		var created struct{ Todo Todo }
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode create: %v (%s)", err, w.Body)
		}
		wantSession := TodoResponsible{
			Kind:      db.ResponsibleSession,
			Name:      "deploy-watch",
			MachineID: "host-1",
			SessionID: "sess-9",
		}
		if created.Todo.Responsible == nil || *created.Todo.Responsible != wantSession {
			t.Fatalf("created responsible = %+v, want %+v", created.Todo.Responsible, wantSession)
		}

		// GET single and LIST both include responsible.
		w = e.must("GET", "/api/v1/todos/"+created.Todo.ID, "")
		var fetched struct{ Todo Todo }
		if err := json.Unmarshal(w.Body.Bytes(), &fetched); err != nil || fetched.Todo.Responsible == nil || *fetched.Todo.Responsible != wantSession {
			t.Fatalf("GET single = %s (%v)", w.Body, err)
		}
		if got := e.get(created.Todo.ID); got == nil || got.Responsible == nil || *got.Responsible != wantSession {
			t.Fatalf("GET list = %+v", got)
		}

		// MCP todo_get and todo_list expose responsible, formatting hints without stray slashes.
		getOut, err := e.s.toolTodoGet(context.Background(), "", "", mustMarshalJSON(t, map[string]any{"id": created.Todo.ID}))
		if err != nil {
			t.Fatalf("toolTodoGet: %v", err)
		}
		if !strings.Contains(getOut, `responsible: session "deploy-watch" (host-1/sess-9)`) {
			t.Fatalf("toolTodoGet missing responsible: %s", getOut)
		}
		listOut, err := e.s.toolTodoList(context.Background(), mustMarshalJSON(t, map[string]any{}))
		if err != nil {
			t.Fatalf("toolTodoList: %v", err)
		}
		if !strings.Contains(listOut, "responsible session: deploy-watch") {
			t.Fatalf("toolTodoList missing responsible: %s", listOut)
		}

		// Single-hint cases in toolTodoGet avoid stray slashes like "(gpu-1/)" or "(/s-ext)".
		e.must("PATCH", "/api/v1/todos/"+created.Todo.ID, `{"responsible":{"kind":"session","name":"deploy-watch","machine_id":"gpu-1"}}`)
		getMachineOnly, err := e.s.toolTodoGet(context.Background(), "", "", mustMarshalJSON(t, map[string]any{"id": created.Todo.ID}))
		if err != nil {
			t.Fatalf("toolTodoGet machine-only: %v", err)
		}
		if !strings.Contains(getMachineOnly, `responsible: session "deploy-watch" (machine gpu-1)`+"\n") {
			t.Fatalf("toolTodoGet machine-only = %s", getMachineOnly)
		}
		e.must("PATCH", "/api/v1/todos/"+created.Todo.ID, `{"responsible":{"kind":"session","name":"deploy-watch","session_id":"s-ext"}}`)
		getSessionOnly, err := e.s.toolTodoGet(context.Background(), "", "", mustMarshalJSON(t, map[string]any{"id": created.Todo.ID}))
		if err != nil {
			t.Fatalf("toolTodoGet session-only: %v", err)
		}
		if !strings.Contains(getSessionOnly, `responsible: session "deploy-watch" (session s-ext)`+"\n") {
			t.Fatalf("toolTodoGet session-only = %s", getSessionOnly)
		}
		e.must("PATCH", "/api/v1/todos/"+created.Todo.ID, `{"responsible":{"kind":"session","name":"deploy-watch","machine_id":"host-1","session_id":"sess-9"}}`)

		// PATCH omitting responsible preserves it.
		e.must("PATCH", "/api/v1/todos/"+created.Todo.ID, `{"notes":"watching metrics"}`)
		if got := e.get(created.Todo.ID); got == nil || got.Responsible == nil || *got.Responsible != wantSession {
			t.Fatalf("after notes-only PATCH = %+v", got)
		}

		// PATCH updates responsible to a person (without machine/session hints).
		w = e.must("PATCH", "/api/v1/todos/"+created.Todo.ID, `{"responsible":{"kind":"person","name":"  Jordan  "}}`)
		var updated struct{ Todo Todo }
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
			t.Fatalf("decode patch person: %v", err)
		}
		wantPerson := TodoResponsible{Kind: db.ResponsiblePerson, Name: "Jordan"}
		if updated.Todo.Responsible == nil || *updated.Todo.Responsible != wantPerson {
			t.Fatalf("updated responsible = %+v, want %+v", updated.Todo.Responsible, wantPerson)
		}
		getNoHints, err := e.s.toolTodoGet(context.Background(), "", "", mustMarshalJSON(t, map[string]any{"id": created.Todo.ID}))
		if err != nil {
			t.Fatalf("toolTodoGet no-hints: %v", err)
		}
		if !strings.Contains(getNoHints, `responsible: person "Jordan"`+"\n") {
			t.Fatalf("toolTodoGet no-hints = %s", getNoHints)
		}

		// Reloading from Postgres preserves responsible.
		if database != nil {
			reloaded := New(Options{Hub: e.f, Store: func() Store { return database }, Logf: testLogf(t)})
			got, ok, err := reloaded.todo(context.Background(), created.Todo.ID)
			if err != nil || !ok || got.Responsible == nil || *got.Responsible != wantPerson {
				t.Fatalf("reloaded responsible = %+v (ok=%v err=%v), want %+v", got.Responsible, ok, err, wantPerson)
			}
		}

		// Validation errors on POST and PATCH return 400 and leave stored state unchanged.
		badPayloads := []string{
			`{"responsible":{"kind":"agent","name":"bot"}}`,
			`{"responsible":{"kind":"session","name":""}}`,
			`{"responsible":{"kind":"person","name":"   "}}`,
			`{"responsible":"ops"}`,
			`{"responsible":123}`,
			`{"responsible":{}}`,
			`{"responsible":{"kind":"session","name":"` + strings.Repeat("a", responsibleNameMax+1) + `"}}`,
			`{"responsible":{"kind":"session","name":"ops","machine_id":"` + strings.Repeat("m", responsibleHintMax+1) + `"}}`,
			`{"responsible":{"kind":"session","name":"ops","session_id":"` + strings.Repeat("s", responsibleHintMax+1) + `"}}`,
		}
		for _, body := range badPayloads {
			if w := e.do("PATCH", "/api/v1/todos/"+created.Todo.ID, body); w.Code != http.StatusBadRequest {
				t.Fatalf("PATCH %s = %d %s, want 400", body, w.Code, w.Body)
			}
		}
		if got := e.get(created.Todo.ID); got == nil || got.Responsible == nil || *got.Responsible != wantPerson {
			t.Fatalf("invalid PATCH mutated responsible: %+v", got)
		}
		if w := e.do("POST", "/api/v1/todos", `{"title":"Bad","responsible":{"kind":"bogus","name":"x"}}`); w.Code != http.StatusBadRequest {
			t.Fatalf("POST invalid responsible = %d %s, want 400", w.Code, w.Body)
		}

		// Read-only role cannot PATCH responsible.
		if w := e.doAs("readonly", "PATCH", "/api/v1/todos/"+created.Todo.ID, `{"responsible":null}`); w.Code != http.StatusForbidden {
			t.Fatalf("read-only PATCH = %d %s, want 403", w.Code, w.Body)
		}

		// PATCH responsible: null clears it cleanly.
		w = e.must("PATCH", "/api/v1/todos/"+created.Todo.ID, `{"responsible":null}`)
		var cleared struct{ Todo Todo }
		if err := json.Unmarshal(w.Body.Bytes(), &cleared); err != nil {
			t.Fatalf("decode clear: %v", err)
		}
		if cleared.Todo.Responsible != nil {
			t.Fatalf("cleared responsible = %+v, want nil", cleared.Todo.Responsible)
		}
		if strings.Contains(w.Body.String(), `"responsible"`) {
			t.Fatalf("cleared JSON should omit responsible: %s", w.Body)
		}
		if got := e.get(created.Todo.ID); got == nil || got.Responsible != nil {
			t.Fatalf("GET list after clear = %+v", got)
		}
		if database != nil {
			reloaded := New(Options{Hub: e.f, Store: func() Store { return database }, Logf: testLogf(t)})
			got, ok, err := reloaded.todo(context.Background(), created.Todo.ID)
			if err != nil || !ok || got.Responsible != nil {
				t.Fatalf("reloaded after clear = %+v (ok=%v err=%v), want nil", got.Responsible, ok, err)
			}
		}
	})
}

func mustMarshalJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
