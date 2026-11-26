package assistant

import (
	"context"

	"github.com/nwverse/persona/server/internal/db"
)

// markTodo writes one of the owner's marks on todo id: with the store through write, else on the memory row through
// edit. Taken under todoBook.writing as seeTodo is, and the apps are told. db.ErrTodoNotFound when it is not there.
func (s *Service) markTodo(ctx context.Context, id string, write func(todoStore) (*db.TodoRow, error), edit func(*db.TodoRow) error) (Todo, error) {
	s.todos.writing.Lock()
	cur, ok := s.todoNow(id)
	if !ok {
		s.todos.writing.Unlock()
		return Todo{}, db.ErrTodoNotFound
	}
	var row *db.TodoRow
	if st := s.todoDB(); st != nil {
		var err error
		if row, err = write(st); err != nil {
			s.todos.writing.Unlock()
			return Todo{}, err
		}
	} else {
		r := todoRowOf(cur)
		if err := edit(&r); err != nil {
			s.todos.writing.Unlock()
			return Todo{}, err
		}
		row = &r
	}
	out, _, applied := s.setTodoRow(id, *row, nil)
	s.todos.writing.Unlock()
	if !applied {
		return Todo{}, db.ErrTodoNotFound // deleted meanwhile
	}
	s.todosChanged(ctx) // the other devices read the list again: it moves there too
	return out, nil
}
