package assistant

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/sunfayao/persona/server/internal/db"
)

// A piece that changes its request (todo_update part_of to another request, or "" to clear it) while a sibling's after
// names it is refused, naming the sibling: otherwise the sibling waits for a piece of another request, which after
// never names. Every combination of the piece's place, its own after,
// who waits for it and the edit goes through the real tools (and the apps' PATCH), in memory and in the store, and
// after each one every after still names pieces of its own request. Ids and titles are placeholders.

// afterInOwnRequest fails t when some to-do's after names a to-do that is not a piece of the same request, as memory
// has them and, reloaded, as the store does.
func afterInOwnRequest(t *testing.T, s *Service) {
	t.Helper()
	check := func(where string) {
		t.Helper()
		s.todos.mu.RLock()
		defer s.todos.mu.RUnlock()
		for _, td := range s.todos.todos {
			for _, a := range td.After {
				if other, ok := s.todos.todos[a]; ok && (td.PartOf == "" || other.PartOf != td.PartOf) {
					t.Fatalf("%s: %s (piece of %q) waits for %s (piece of %q)", where, td.ID, td.PartOf, a, other.PartOf)
				}
			}
		}
	}
	check("memory")
	reloadTodos(t, s)
	check("after a reload")
}

// The way out the refusal names: take the to-do out of the waiting sibling's after, then move it (or clear it). The
// sibling can then send its own after back, and a task_plan start that would move it is left out while it is waited
// for.
func TestTodoPiecesMoveAfterTheWaitIsTaken(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		s := e.s
		for _, title := range []string{"Move house", "Sell the car", "Book the van", "Pack the kitchen", "Clean the flat"} {
			pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": title})
		}
		r, r2 := mustTodo(t, s, "Move house"), mustTodo(t, s, "Sell the car")
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": mustTodo(t, s, "Book the van").ID, "part_of": r.ID})
		x := mustTodo(t, s, "Book the van")
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": mustTodo(t, s, "Pack the kitchen").ID, "part_of": r.ID, "after": []string{x.ID}})
		y := mustTodo(t, s, "Pack the kitchen")
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": mustTodo(t, s, "Clean the flat").ID, "part_of": r.ID})
		z := mustTodo(t, s, "Clean the flat")

		// A task started on x that would move it: the link is left out, as for any link that no longer holds.
		if out, err := s.startPieces(context.Background(), PlanTask{PartOf: r2.ID}, x); err != nil || out.PartOf != r.ID {
			t.Fatalf("a start moved a piece that is waited for: %+v", out)
		}
		pieceRefused(t, s, "conv1", "todo_update", map[string]any{"id": x.ID, "part_of": r2.ID}, fmt.Sprintf("%q waits for this to-do", y.ID))

		// Y sends its own after back with one more prerequisite: accepted, x is still its sibling.
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": y.ID, "after": []string{x.ID, z.ID}})
		// Taken out of Y's after, x moves; Y keeps the rest.
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": y.ID, "after": []string{z.ID}})
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": x.ID, "part_of": r2.ID})
		if got, _ := s.todoNow(x.ID); got.PartOf != r2.ID {
			t.Fatalf("moved: %+v", got)
		}
		if got, _ := s.todoNow(y.ID); !slices.Equal(got.After, []string{z.ID}) {
			t.Fatalf("Y: %+v", got)
		}
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": y.ID, "after": []string{z.ID}}) // its after sent back

		// The same for clearing: Z waited for by Y, refused until Y lets go.
		pieceRefused(t, s, "conv1", "todo_update", map[string]any{"id": z.ID, "part_of": ""}, fmt.Sprintf("%q waits for this to-do", y.ID))
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": y.ID, "after": []string{}})
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": z.ID, "part_of": ""})
		if got, _ := s.todoNow(z.ID); got.PartOf != "" || len(got.After) != 0 {
			t.Fatalf("cleared: %+v", got)
		}
		afterInOwnRequest(t, s)
	})
}

// waitingFor counts only the to-dos that would stop being siblings: one already in the request the to-do moves to (a
// link an older hub could leave) is no reason to refuse, and moving there mends it.
func TestWaitingFor(t *testing.T) {
	book := map[string]*Todo{
		"x":  {ID: "x", PartOf: "r", After: []string{"x"}},
		"y1": {ID: "y1", PartOf: "r", After: []string{"x"}},
		"y2": {ID: "y2", PartOf: "r2", After: []string{"w", "x"}},
		"w":  {ID: "w", PartOf: "r2"},
		"z":  {ID: "z", PartOf: "r"},
	}
	for _, c := range []struct {
		partOf string
		want   []string
	}{{"r2", []string{"y1"}}, {"r", []string{"y2"}}, {"", []string{"y1", "y2"}}, {"r3", []string{"y1", "y2"}}} {
		if got := waitingFor(book, "x", c.partOf); !slices.Equal(got, c.want) {
			t.Errorf("part_of %q: %v, want %v", c.partOf, got, c.want)
		}
	}
}
