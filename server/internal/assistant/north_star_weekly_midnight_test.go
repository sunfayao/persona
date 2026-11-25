package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// Every timestamp contributes once to every weekly row. Private marker strings belong
// in the source tables only, including the fields that a read-only report must omit.
func seedWeeklyRows(t *testing.T, database *db.DB, instants []time.Time) {
	t.Helper()
	ctx := context.Background()
	setNorthStarWeeklyOwnConfig(t, `{"agents":{"review":{"launcher":"fixture"},"dispatch":{"enabled":true}}}`)
	mustDo(t, database.PutSetting(ctx, northStarSinceKey, northStarSince{At: instants[0].AddDate(0, 0, -40), Build: "fixture"}))
	const path = "/synthetic/private/path"
	older := instants[0].AddDate(0, 0, -60)
	mustDo(t, database.PutSetting(ctx, agentArchivesKey, map[string]agentArchive{
		"weekly-private-archive": {Archived: true, Session: proto.Session{UpdatedAt: older}},
	}))
	var dispatches []dispatchTask
	for i, at := range instants {
		prefix := fmt.Sprintf("weekly-private-%d-", i)
		todo := prefix + "todo"
		mustDo(t, database.CreateTodo(ctx, db.TodoRow{ID: todo, Title: prefix + "title",
			CreatedAt: at.Add(-time.Hour), UpdatedAt: at}))
		tasks, err := json.Marshal([]PlanTask{{TodoID: todo, WorkspaceResolved: &TaskWorkspaceResolved{Path: path, Created: true}}})
		mustDo(t, err)
		mustDo(t, database.CreateTaskPlan(ctx, db.TaskPlanRow{ID: prefix + "plan", Conversation: "main-chat",
			Tasks: tasks, State: "started", CreatedAt: at, UpdatedAt: at}))
		run := prefix + "run"
		review, err := json.Marshal(AutoReview{ID: run, State: "clean", StartedAt: at, EndedAt: &at})
		mustDo(t, err)
		mustDo(t, database.PutTodoSupervision(ctx, db.TodoSupervisionRow{TodoID: todo, DoneWhen: []string{}, AutoReview: review}))
		mustDo(t, database.LinkTodoReviewer(ctx, todo, prefix+"machine", prefix+"reviewer", prefix+"title", path, "", run, at))
		_, err = database.LinkTodoAgentWithSource(ctx, todo, prefix+"machine", prefix+"worker", prefix+"title", path, "claude", "", "worker", prefix+"leader", at)
		mustDo(t, err)
		brief := prefix + "brief"
		_, err = database.CreateTaskBrief(ctx, db.TaskBriefRow{ID: brief, Brief: json.RawMessage(`{}`), CreatedAt: at, UpdatedAt: at})
		mustDo(t, err)
		_, err = database.LinkTaskBriefTodo(ctx, db.TaskBriefTodoRow{TodoID: todo, BriefID: brief, Continues: prefix + "previous", LinkedAt: at})
		mustDo(t, err)
		_, _, err = database.ReviewDoneTodo(ctx, todo, prefix+"reason", true, at)
		mustDo(t, err)
		dispatches = append(dispatches, dispatchTask{Session: prefix + "dispatch", Cwd: path, At: at})
		for _, event := range []db.WorkEvent{
			{At: at, MachineID: prefix + "machine", SessionID: "main-chat", Kind: notifyEvent, Detail: map[string]any{"todo": todo}},
			{At: at, MachineID: prefix + "machine", SessionID: "main-chat", Kind: sendEvent, Detail: map[string]any{
				"op": "resume", "origin": originManager, "role": roleMain, "outcome": outcomeFailed, "reason": "conversation_not_found"}},
			{At: at, Kind: todoReopenedEvent, Detail: map[string]any{"todo": todo, "after": 3600}},
		} {
			mustDo(t, database.AddWorkEvent(ctx, event))
		}
	}
	mustDo(t, database.PutSetting(ctx, dispatchSettingKey, dispatches))
}
