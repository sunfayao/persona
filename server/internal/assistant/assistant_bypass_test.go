package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestAssistantBypassDefaultPostgres(t *testing.T) {
	database := todoTestDB(t)
	f := newFakeHub().withMachine()
	s := newServiceOn(t, f, database)
	ctx := context.Background()
	for _, p := range []StartParams{{Main: true}, {Untitled: true}} {
		sess, err := s.Start(ctx, p)
		if err != nil || sess.PermissionMode != proto.ModeBypass || f.sends[len(f.sends)-1]["permission_mode"] != proto.ModeBypass {
			t.Fatalf("unconfigured start: %+v %v", sess, err)
		}
	}
	now := time.Now()
	task := Task{ID: "routine", Title: "Read status", Prompt: "Read local status", Repeat: repeatOnce, Enabled: true}
	setTasks(s, task)
	_, err := s.runTask(task, now, now, true, false)
	if err != nil || f.sends[len(f.sends)-1]["permission_mode"] != proto.ModeBypass {
		t.Fatalf("scheduled start mode: %v %v", f.sends, err)
	}
	w, _ := permissionSettingsRequest(t, s, "PUT", "/api/v1/assistant", "assistant", "full", `{"default_permission_mode":"plan"}`)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	reloaded := newServiceOn(t, f, database)
	sess, err := reloaded.Start(ctx, StartParams{Untitled: true})
	if err != nil || sess.PermissionMode != "plan" {
		t.Fatalf("manager selection after restart: %+v %v", sess, err)
	}
	sess, err = reloaded.Start(ctx, StartParams{PermissionMode: "default", Untitled: true})
	if err != nil || sess.PermissionMode != "default" {
		t.Fatalf("explicit selection: %+v %v", sess, err)
	}
}
