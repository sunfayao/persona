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

// directionForTodo resolves the one current direction above a todo. A worker supervised by a task-leader Agent first
// goes through leaderAbove; a missing leader, a nested leader ref, the main conversation, or an ended/unknown leader is
// not a direction. No title, cwd, or map-order inference participates in this decision.
func (s *Service) directionForTodo(ctx context.Context, t Todo) (string, *direction, bool) {
	if t.Supervision == nil || t.Supervision.Supervisor == "" {
		return "", nil, false
	}
	lead := t.Supervision.Supervisor
	if s.agentSupervised(t) {
		above, _, ok := s.leaderAbove(t)
		if !ok || above.ID == "" {
			return "", nil, false
		}
		lead, ok = s.uniqueLeaderSupervisorAbove(t)
		if !ok || above.Supervision == nil || above.Supervision.Supervisor != lead {
			return "", nil, false
		}
	}
	cfg := s.settings(ctx)
	if lead == "" || lead == cfg.Main {
		return "", nil, false
	}
	found := false
	for _, id := range cfg.Leaders {
		if id == lead {
			found = true
			break
		}
	}
	if !found {
		return "", nil, false
	}
	d := s.directionOf(lead)
	if d == nil || d.EndedAt != nil {
		return "", nil, false
	}
	return lead, d, true
}

// uniqueLeaderSupervisorAbove validates leaderAbove instead of accepting its first map-order match. Historical and
// current leader todos may retain the same session; different conversation supervisors are ambiguous and confer no
// direction. Repeated rows for the same conversation still describe one direction.
func (s *Service) uniqueLeaderSupervisorAbove(t Todo) (string, bool) {
	if t.Supervision == nil {
		return "", false
	}
	machine, session, ok := parseLeaderRef(t.Supervision.Supervisor)
	if !ok {
		return "", false
	}
	machine = s.canonical(machine)
	candidate := ""
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	for _, lead := range s.todos.todos {
		for _, a := range lead.Agents {
			if a.MachineID != machine || a.SessionID != session || a.Source != sourceLeader {
				continue
			}
			if lead.Supervision == nil || lead.Supervision.Supervisor == "" {
				continue
			}
			supervisor := lead.Supervision.Supervisor
			if _, _, ref := parseLeaderRef(supervisor); ref {
				return "", false
			}
			if candidate != "" && candidate != supervisor {
				return "", false
			}
			candidate = supervisor
		}
	}
	return candidate, candidate != ""
}
