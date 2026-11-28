package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	approvalops "github.com/sunfayao/persona/server/internal/approval"
	"github.com/sunfayao/persona/server/internal/assistant/repository"
	"github.com/sunfayao/persona/server/internal/assistant/toolnotes"
	"github.com/sunfayao/persona/server/internal/assistant/tooluse"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Field notes (assistant/toolnotes): tips the user approved, at the end of a tool's description in the main
// conversation's tool list (withToolNotes on the mcp.tools path, toolNotesRevision in the tool watches' revision),
// proposed and reviewed with tool_note, looked over at the tool record's tick (toolUseLoop), and counted in the main
// conversation's snapshot (toolReview). The root lends the domain its store, cards, tools, computers, to-dos and the
// tool record's friction drafts.

func (s *Service) newToolNotes() *toolnotes.Domain {
	return toolnotes.New(toolnotes.Host{
		Load: func(ctx context.Context, key string, out any) (bool, error) {
			if st := s.store(); st != nil {
				return st.Setting(ctx, key, out)
			}
			return false, nil
		},
		Save: func(ctx context.Context, key string, v any) error {
			if st := s.store(); st != nil {
				return st.PutSetting(ctx, key, v)
			}
			return nil
		},
		Now:  time.Now,
		Logf: s.logf,
		Tools: func(ctx context.Context) []proto.Tool {
			return append(s.mcpTools(ctx, true), toolsNamed(assistantwire.A2AToolNames)...)
		},
		Runtime: s.callerRuntime,
		Machine: func(ctx context.Context, name string) (string, string, bool) {
			m, ok := s.machineByName(ctx, name)
			return m.ID, m.Name, ok
		},
		MachineName: s.machineName,
		Private: func(ctx context.Context) []string {
			host, _ := os.Hostname()
			names := []string{host}
			for _, m := range s.devices(ctx) {
				names = append(names, m.ID, m.Name, m.SSHHost)
			}
			return names
		},
		Drafts:      s.frictionDrafts,
		UpdateDraft: s.updateFrictionDraft,
		Confirm: func(ctx context.Context, c toolnotes.Card, op approvalops.Operation) (approvalops.Outcome, error) {
			now := time.Now().UTC()
			op.Replacement = c.Machine + "\x00" + c.Session + "\x00" + toolnotes.ToolName + "\x00" + c.Key
			return s.waitPreparedCard(ctx, proto.Interaction{ID: "ix_" + randomHex(6), MachineID: c.Machine, SessionID: c.Session,
				Kind: proto.IxPermission, ToolName: toolnotes.ToolName, Summary: c.Summary, ToolInput: c.Shown, SessionTitle: "Persona",
				CreatedAt: now, ExpiresAt: now.Add(settingCardWait)}, op)
		},
		Repository: s.savedRepository,
		FileTodo:   s.fileFixTodo,
		TodoDone: func(id string) bool {
			t, ok := s.todoNow(id)
			return ok && t.DoneAt != nil
		},
		Links: func(ctx context.Context, conversations []toolnotes.Evidence) []string {
			web := s.webBase(ctx)
			out := make([]string, 0, len(conversations))
			for _, c := range conversations {
				out = append(out, cmp.Or(sessionLink(web, c.Machine, c.Session, true), sessionPath(c.Machine, c.Session, true)))
			}
			return out
		},
		Changed: func(rendered bool) {
			if rendered {
				s.mcpChanges.notify()
			}
			s.snapshotOld()
		},
	})
}

// toolNote runs a tool_note call of conversation self on machine.
func (s *Service) toolNote(ctx context.Context, machine, self string, args json.RawMessage) (string, error) {
	return s.toolNotes.Tool(ctx, toolnotes.Call{Machine: machine, Session: self, Lang: s.lang(ctx), Zone: s.location(ctx)}, args)
}

// withToolNotes is an assistant conversation's tools with their field notes; any other conversation's as they are.
func (s *Service) withToolNotes(ctx context.Context, tools []proto.Tool, assistant bool) []proto.Tool {
	if !assistant || s.toolNotes == nil {
		return tools
	}
	return s.toolNotes.Annotate(ctx, tools)
}

// toolNotesRevision is the field notes' part of an assistant conversation's tool-list revision (toolsCursor).
func (s *Service) toolNotesRevision(ctx context.Context, assistant bool) string {
	if !assistant || s.toolNotes == nil {
		return ""
	}
	return s.toolNotes.Revision(ctx)
}

// toolReview is the main conversation's snapshot line of what waits in tool_note list; nil when nothing does.
func (s *Service) toolReview(ctx context.Context) *prompts.SnapshotToolReview {
	if s.toolNotes == nil {
		return nil
	}
	if drafts, due := s.toolNotes.Waiting(ctx); drafts+due > 0 {
		return &prompts.SnapshotToolReview{Drafts: drafts, NotesDue: due}
	}
	return nil
}

// frictionDrafts are the tool record's drafts, as the field notes read them.
func (s *Service) frictionDrafts(ctx context.Context) ([]toolnotes.Draft, error) {
	list, err := s.toolUse.Drafts(ctx)
	out := make([]toolnotes.Draft, 0, len(list))
	for _, d := range list {
		out = append(out, notesDraft(d))
	}
	return out, err
}

// updateFrictionDraft changes draft id through f, which may set its state, note and to-do.
func (s *Service) updateFrictionDraft(ctx context.Context, id string, f func(*toolnotes.Draft) error) error {
	err := s.toolUse.UpdateDraft(ctx, id, func(d *tooluse.Draft) error {
		x := notesDraft(*d)
		if err := f(&x); err != nil {
			return err
		}
		d.Status, d.NoteID, d.TodoID = x.Status, x.NoteID, x.TodoID
		return nil
	})
	if errors.Is(err, tooluse.ErrNoDraft) {
		return toolnotes.ErrNoDraft
	}
	return err
}

func notesDraft(d tooluse.Draft) toolnotes.Draft {
	evidence := make([]toolnotes.Evidence, 0, len(d.Evidence))
	for _, e := range d.Evidence {
		evidence = append(evidence, toolnotes.Evidence{Machine: e.Machine, Session: e.Session, At: e.At})
	}
	return toolnotes.Draft{ID: d.ID, Kind: d.Kind, Tool: d.Tool, Code: d.Code, Runtime: d.Runtime, Count: d.Count, FirstAt: d.FirstAt,
		LastAt: d.LastAt, Skeleton: d.Skeleton, RetrySkeleton: d.RetrySkeleton, Evidence: evidence, Text: d.Text, Status: d.Status,
		NoteID: d.NoteID, TodoID: d.TodoID}
}

// savedRepository is the repository saved in Persona at path (owner/name, any provider): a fix's to-do is bound
// to it.
func (s *Service) savedRepository(ctx context.Context, path string) (toolnotes.Repository, bool) {
	doc, err := s.repositoryDocument(ctx)
	if err != nil {
		return toolnotes.Repository{}, false
	}
	for _, r := range doc.Repositories {
		if strings.EqualFold(r.Path, path) {
			return toolnotes.Repository{ID: r.ID, Revision: doc.Revision}, true
		}
	}
	return toolnotes.Repository{}, false
}

// fileFixTodo makes a fix's to-do as todo_create makes one (the open to-do of the same title is it), bound to repo's
// default branch when repo is given.
func (s *Service) fileFixTodo(ctx context.Context, c toolnotes.Call, title, notes string, repo *toolnotes.Repository) (string, bool, error) {
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return "", false, err
	}
	if t, ok := s.openTodoLike(title, "", db.TodoEdit{}); ok {
		return t.ID, true, nil
	}
	if ok, err := s.confirmOutbound(ctx, c.Machine, c.Session, "todo_create", "", title, nil); err != nil || !ok {
		return "", false, cmp.Or(err, errors.New("the user did not allow it on the phone"))
	}
	var bindings []*repository.CreateBindings
	if repo != nil {
		bindings = append(bindings, &repository.CreateBindings{CatalogRevision: &repo.Revision,
			Repositories: []repository.TaskInput{{RepoID: repo.ID, RequestedRef: repository.RequestedRef{Kind: "default"}}}})
	}
	t, err := s.createPlainTodo(ctx, title, notes, db.TodoEdit{}, bindings...)
	return t.ID, false, err
}
