package assistant

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
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
