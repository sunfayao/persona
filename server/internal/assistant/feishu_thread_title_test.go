package assistant

import (
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
)

func TestTopicTitleRenamesFromTheOriginalLink(t *testing.T) {
	database := todoTestDB(t)
	e := newThreadEnv(t, database)
	e.f.fakeModel(t)
	e.f.mu.Lock()
	e.f.hello.Locale = "en"
	e.f.todoTitleReply = "Review the garden map"
	e.f.mu.Unlock()
	const link = "https://docs.example.com/wiki/garden-map-2026"
	e.deliver(rootEvent("om_shared", link))

	todo := onlyTodo(e.s)
	initial := e.said("thread_open_done", i18n.Args{"title": "New to-do"})
	if notes := topicNotes(e.f); len(notes) != 1 || notes[0]["text"] != initial {
		t.Fatalf("first topic reply %+v, want %q", notes, initial)
	}
	row, err := database.GetTodo(t.Context(), todo.ID)
	if err != nil || row == nil || row.Title != "Review the garden map" || row.Notes != link {
		t.Fatalf("renamed to-do %+v, %v", row, err)
	}
	e.f.mu.Lock()
	defer e.f.mu.Unlock()
	if len(e.f.todoTitleAsked) != 1 || !strings.Contains(e.f.todoTitleAsked[0].User, link) {
		t.Fatalf("model did not receive the original URL: %+v", e.f.todoTitleAsked)
	}
}

func TestRequestFromAnotherChatKeepsItsOpeningWords(t *testing.T) {
	for _, home := range []string{askHome, ""} {
		t.Run(home, func(t *testing.T) {
			e := newAskEnv(t, nil)
			e.f.mu.Lock()
			e.home, e.f.hello.Locale = home, "en"
			e.f.mu.Unlock()
			e.ask(ownerAsk("om_request", "/ai Have a look!"))
			todo := onlyTodo(e.s)
			if todo.Title != "Have a look!" || todo.Notes != "Have a look!" || todo.Source == nil {
				t.Fatalf("original external request %+v", todo)
			}
		})
	}
}
