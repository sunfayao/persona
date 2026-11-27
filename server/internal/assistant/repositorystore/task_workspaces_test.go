package repositorystore

import (
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/repository"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/taskworkspace"
	"github.com/sunfayao/persona/server/internal/workspacenaming"
)

func backendPlan(t *testing.T, d *Store, op string, change func(*workspacenaming.Source)) workspacenaming.Plan {
	t.Helper()
	ctx := t.Context()
	set, err := d.TodoRepositories(ctx, "backend")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Snapshot("assistant", "backend", set.Revision, set.Repositories, []string{"web"})
	if err != nil {
		t.Fatal(err)
	}
	source := workspacenaming.Source{Manager: "assistant", TodoID: "backend", Title: "Example", MachineID: "computer", PlannedAt: time.Now().UTC(), Timezone: "UTC", Rules: workspacenaming.Defaults(), Layout: repository.LayoutRoot, Repositories: snapshot}
	if change != nil {
		change(&source)
	}
	preview := workspacenaming.Resolve(source, workspacenaming.NodeContext{Home: "/home/example"})
	if preview.Plan == nil {
		t.Fatal(preview)
	}
	if _, err = d.SaveWorkspaceNamingPlan(ctx, "backend", op, 0, *preview.Plan); err != nil {
		t.Fatal(err)
	}
	return *preview.Plan
}
func backendDB(t *testing.T) *Store {
	t.Helper()
	d := open(t)
	ctx := t.Context()
	one := int64(1)
	identity, _ := repository.Parse("https://github.com/example/web", "")
	if err := d.PutSetting(ctx, repository.CatalogKey, repository.Catalog{Revision: 1, Repositories: []repository.CatalogEntry{{ID: "web", Identity: identity}}}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateTodoWithRepositories(ctx, db.TodoRow{ID: "backend", Title: "Example", CreatedAt: time.Now(), UpdatedAt: time.Now()}, repository.CreateBindings{CatalogRevision: &one, Repositories: []repository.TaskInput{{RepoID: "web"}}}); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBackendWorkspaceFailedCorrectionKeepsNodeOwner(t *testing.T) {
	d := backendDB(t)
	ctx := t.Context()
	backendPlan(t, d, "op_first", nil)
	first, err := d.BeginTaskWorkspace(ctx, "backend", "op_first", "assistant")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.MutateTaskWorkspace(ctx, "backend", first.OperationID, 0, "cancel", ""); err != nil {
		t.Fatal(err)
	}
	backendPlan(t, d, "op_second", func(s *workspacenaming.Source) { s.Rules.BranchTemplate = "different/{task-id}" })
	next, err := d.BeginTaskWorkspace(ctx, "backend", "op_second", "assistant")
	if err != nil {
		t.Fatal(err)
	}
	rejected := *next
	rejected.NodeOperationID = first.OperationID
	rejected.State = "failed"
	rejected.Error = taskworkspace.Failure("resource_conflict", "prepare", next.OperationID, "", first.Plan.RootPath)
	next, err = d.SaveTaskWorkspaceResult(ctx, "backend", next.OperationID, next.Revision, rejected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.MutateTaskWorkspace(ctx, "backend", next.OperationID, 0, "cancel", ""); err != nil {
		t.Fatal(err)
	}
	backendPlan(t, d, "op_corrected", nil)
	corrected, err := d.BeginTaskWorkspace(ctx, "backend", "op_corrected", "assistant")
	if err != nil || corrected.PreviousOperationID != first.OperationID {
		t.Fatal("correction targeted an operation never admitted by Node", corrected, err)
	}
}
