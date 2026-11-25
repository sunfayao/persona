package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// The To-dos view on the wire (todo_view_routes.go, #10226): every row of the list carries its view and the list its
// counts, one to-do reads as its row, and the places come with the same counts.

// The manager tells the hub it serves the view, the places and a to-do's timeline (todo_timeline.go).
func TestTodoViewBusHelloCapabilities(t *testing.T) {
	b := NewBusHub(BusOptions{Hub: "http://127.0.0.1:1", Token: "t"})
	p, _ := b.client.HelloParams().(map[string]any)
	caps, _ := p["capabilities"].([]string)
	for _, c := range []string{assistantwire.CapabilityTodoView, assistantwire.CapabilityTodoPlaces, assistantwire.CapabilityTodoTimeline, "todo_images"} {
		if !slices.Contains(caps, c) {
			t.Errorf("the manager does not name %s: %v", c, caps)
		}
	}
}

// todoViewRows makes a plain to-do, one a run works on, a closed one and a failed start, and answers their ids.
func todoViewRows(t *testing.T, e *todoEnv) (plain, worked, closed, failed string) {
	t.Helper()
	began := time.Now().UTC().Add(-time.Minute)
	e.sessions("server", proto.Session{SessionID: "s1", Title: "Hedge trimming", Status: proto.StatusWorking,
		State: proto.StateWorking, StateSince: began, UpdatedAt: began, Activity: "measuring the hedge"})
	plain, worked, closed, failed = e.create("Water the roses").ID, e.create("Prune the hedge").ID, e.create("Order compost").ID,
		e.create("Book the ferry").ID
	e.must("PUT", "/api/v1/todos/"+worked+"/agents/server/s1", "")
	e.must("PATCH", "/api/v1/todos/"+closed, `{"done":true}`)
	if err := e.s.failTodo(context.Background(), failed, "The computer is offline"); err != nil {
		t.Fatal(err)
	}
	return plain, worked, closed, failed
}

