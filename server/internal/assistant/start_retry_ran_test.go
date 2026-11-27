package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// A retry on another launcher starts from the launcher the failed start ran, which the plan does not always name: a
// tier or a start-defaults policy may have chosen it (#10365). Machine and launcher names are placeholders.

// retryMoves reports whether conversation's feed records a move of todo's start from launcher from to launcher to.
func retryMoves(s *Service, conversation, todo, from, to string) bool {
	items, _, _ := s.feed(context.Background(), conversation, time.Time{})
	return slices.ContainsFunc(items, func(it FeedItem) bool {
		var b startRemedyBody
		_ = json.Unmarshal(it.Body, &b)
		return it.Kind == feedStartRemedy && it.Ref == todo && b.LauncherFrom == from && b.LauncherTo == to
	})
}

// A retry on another launcher that runs the same agent keeps the task's own permission mode: a Codex task planned in
// plan mode stays in plan mode on another Codex launcher, whatever the owner's default mode (#10365). Another agent
// settles it again (TestTaskRetrySuggestsNoManagedLauncherWhenPersonadMissing: Allow all on Claude Code).
func TestRetryOnAnotherLauncherOfTheSameAgentKeepsThePermissionMode(t *testing.T) {
	s, f := launcherRetryHub(t)
	ctx := context.Background()
	f.mu.Lock()
	f.launcherList = append(f.launcherList, proto.Launcher{Name: "cx2", Agent: "codex"})
	f.failStart = terminalRefusal(proto.CodeLauncherMissing, "cx")
	f.mu.Unlock()
	setSettings(t, s, func(v *Settings) { v.DefaultPermissionMode = "bypassPermissions" })
	res, err := s.toolTaskPlan(ctx, "conv1", json.RawMessage(`{"tasks":[{"title":"Plan the fix","prompt":"Plan the fix","machine":"box",
		"cwd":"/code/app","launcher":"cx","permission":"plan","why":"the code is on box"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.startPlan(ctx, planID(t, res), nil, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the start to fail", func() bool { return onlyTodo(s).StartError != "" && s.taskStarting.Load() == 0 })
	f.mu.Lock()
	f.failStart = nil
	f.mu.Unlock()
	todo := onlyTodo(s)
	if first := startsOf(f)[0]; first["launcher"] != "cx" || first["permission_mode"] != "plan" {
		t.Fatalf("first start %v", first)
	}
	if status, body := serveJSON(t, s, "POST", "/api/v1/todos/"+todo.ID+"/retry", "en", `{"launcher":"cx2"}`); status != 200 {
		t.Fatalf("retry: %d %v", status, body)
	}
	waitFor(t, "the second start to end", func() bool { return len(startsOf(f)) == 2 && s.taskStarting.Load() == 0 })
	if second := startsOf(f)[1]; second["launcher"] != "cx2" || second["permission_mode"] != "plan" {
		t.Fatalf("second start %v", second)
	}
	_, p, i, _ := s.remedyOf(ctx, todo.ID)
	if pt := p.Tasks[i]; pt.Launcher != "cx2" || pt.Permission != "plan" {
		t.Fatalf("plan task %+v", pt)
	}
}
