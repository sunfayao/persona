package assistant

import (
	"context"
	"fmt"
	"github.com/sunfayao/persona/server/internal/assistant/snapshotfit"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// tasksOn300 is fleetOf300 with n open tasks from conversation "c1", each on one of the machine's sessions.
func tasksOn300(logf func(string, ...any), n int) *Service {
	s := fleetOf300(logf)
	now := time.Now()
	for i := range n {
		putTodo(s, Todo{ID: fmt.Sprintf("t%02d", i), Title: "a task", Origin: "c1", CreatedAt: now, UpdatedAt: now,
			Agents: []TodoAgent{{MachineID: "mac", SessionID: fmt.Sprintf("s%03d", i), LinkedAt: now}}})
	}
	return s
}

// BenchmarkSnapshotTasks: the tasks part of the assistant's live snapshot (taken for the next message every 45 s),
// for 30 open tasks on a machine listing 300 sessions.
func BenchmarkSnapshotTasks(b *testing.B) {
	s := tasksOn300(b.Logf, 30)
	b.ReportAllocs()
	for b.Loop() {
		s.snapshotTasks(context.Background(), "")
	}
}

// The tasks in the snapshot read each machine's sessions once, however many tasks have an agent there.
func TestSnapshotTasksListsEachMachineOnce(t *testing.T) {
	s := tasksOn300(func(string, ...any) {}, 30)
	d := s.devs.(*fleetDevs)
	if got := snapshotfit.Scope(prompts.Snapshot{Tasks: s.snapshotTasks(context.Background(), "")}, "").Tasks; len(got) != snapshotfit.TaskLimit {
		t.Fatalf("%d tasks in the snapshot, want %d", len(got), snapshotfit.TaskLimit)
	}
	if n := d.machines["mac"].lists; n != 1 {
		t.Errorf("the machine's sessions were listed %d times for 30 tasks, want 1", n)
	}
}

// The supervision watch (every superviseTick) also reads each machine's sessions once for all the supervised tasks.
func TestSuperviseWatchListsEachMachineOnce(t *testing.T) {
	s := tasksOn300(func(string, ...any) {}, 0)
	now := time.Now()
	for i := range 10 {
		putTodo(s, Todo{ID: fmt.Sprintf("v%02d", i), Title: "a task", Origin: "c1", CreatedAt: now, UpdatedAt: now,
			Supervision: &Supervision{}, Agents: []TodoAgent{{MachineID: "mac", SessionID: fmt.Sprintf("s%03d", i), LinkedAt: now}}})
	}
	d := s.devs.(*fleetDevs)
	s.superviseWatch(context.Background(), now)
	if n := d.machines["mac"].lists; n != 1 {
		t.Errorf("the machine's sessions were listed %d times for 10 supervised tasks, want 1", n)
	}
}

// fleetOf300 is a manager whose one machine lists 300 finished sessions.
func fleetOf300(logf func(string, ...any)) *Service {
	f := newFakeHub()
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: f, Logf: logf, Devices: d})
	list := make([]proto.Session, 300)
	for i := range list {
		list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude",
			Status: proto.StatusDone, PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a session", LastText: "done"}
	}
	d.setSessions("mac", list)
	return s
}
