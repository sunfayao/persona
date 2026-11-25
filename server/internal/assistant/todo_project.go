package assistant

import (
	"context"
	"strings"

	"github.com/nwverse/persona/server/internal/db"
)

// A todo's project (#2581, docs/assistant/todos.md "Project"): a label the owner or the assistant files it under, usually
// an area of the user's work. The apps group open todos by it. It is kept as the text given, trimmed.

// todoProject is the label name names, trimmed; "" for "".
func (s *Service) todoProject(ctx context.Context, name string) (string, error) {
	return strings.TrimSpace(name), nil
}

// setTodoProject puts project (nil: unchanged; "" clears it) into e as the slug it names.
func (s *Service) setTodoProject(ctx context.Context, project *string, e *db.TodoEdit) error {
	if project == nil {
		return nil
	}
	slug, err := s.todoProject(ctx, *project)
	if err != nil {
		return err
	}
	e.Project = &slug
	return nil
}
