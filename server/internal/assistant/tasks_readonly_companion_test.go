package assistant

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// readonlyCompanionService is readonlyTaskService with settings kept, so a tier can resolve to agy.
func readonlyCompanionService(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	f := newFakeHub()
	f.sessions["box"] = nil
	f.ownModels = true
	s := newServiceOn(t, f, &profileTaskStore{memStore: &memStore{}, plans: map[string]db.TaskPlanRow{}})
	saveTiers(t, s, map[string]ModelTier{"simple": {Launcher: "agy"}})
	return s, f
}

// A read-only agy task the computer's Antigravity companion runs starts there at once, pinned to it (host agy), in plan
// mode, whether it names the agy launcher or a tier that resolves to it, or names host agy itself; with
// assistant.readonly_mode prompt it keeps agy's Allow all and the first message's constraint, as Claude Code does,
// wherever agy runs. Without the companion, or with a terminal host named, it is refused when it is planned, as
// before; and a companion that went away fails the start instead of opening a pane (#13464).
func TestReadonlyAgyTaskStartsOnCompanion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		task      func(*PlanTask)
		launchers []proto.Launcher
		prompt    bool   // assistant.readonly_mode prompt
		host      string // the host the start names (default: agy)
		away      bool   // the node answers a start naming agy with agy_companion_offline
		refused   bool   // readonly_unsupported when planned
		mode      string
	}{
		{name: "agy launcher", task: func(p *PlanTask) { p.Launcher = "agy" }, launchers: companionLaunchers(), mode: "plan"},
		{name: "simple tier", task: func(p *PlanTask) { p.Tier = "simple" }, launchers: companionLaunchers(), mode: "plan"},
		{name: "prompt mode", task: func(p *PlanTask) { p.Launcher = "agy" }, launchers: companionLaunchers(), prompt: true,
			mode: "bypassPermissions"},
		{name: "no companion", task: func(p *PlanTask) { p.Launcher = "agy" }, launchers: paneLaunchers(), refused: true},
		{name: "herdr named", task: func(p *PlanTask) { p.Launcher, p.Host = "agy", proto.EngineHerdr }, launchers: companionLaunchers(),
			refused: true},
		{name: "companion went away", task: func(p *PlanTask) { p.Launcher = "agy" }, launchers: companionLaunchers(), away: true,
			mode: "plan"},
		// An agy the owner defined in the shell hides the companion from the list; naming host agy, as the refusal
		// says, still sends the task to it (the node refuses it there if none is connected).
		{name: "host agy named", task: func(p *PlanTask) { p.Launcher, p.Host = "agy", proto.HostAgy }, launchers: paneLaunchers(),
			mode: "plan"},
		// With assistant.readonly_mode prompt, agy takes read-only as an instruction wherever it runs: no companion needed.
		{name: "prompt mode without the companion", task: func(p *PlanTask) { p.Launcher = "agy" }, launchers: paneLaunchers(),
			prompt: true, host: "none", mode: "bypassPermissions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, f := readonlyCompanionService(t)
				f.launcherList = tc.launchers
				if tc.prompt {
					setSettings(t, s, func(v *Settings) { v.ReadonlyMode = readonlyPrompt })
				}
				if tc.away {
					f.refuseHost = map[string]error{proto.HostAgy: companionAwayErr}
				}
				task := readonlyTestTask("")
				tc.task(&task)
				_, err := s.toolTaskPlan(context.Background(), "readonly-chat", readonlyTaskArgs(t, task))
				synctest.Wait()
				if tc.refused {
					var re *rpc.Error
					if !errors.As(err, &re) || re.Code != "readonly_unsupported" {
						t.Fatalf("read-only agy plan: %v, want readonly_unsupported", err)
					}
					if hosts := startHosts(f); len(hosts) != 0 {
						t.Fatalf("a refused read-only agy task started: %q", hosts)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				starts := append([]map[string]any(nil), f.starts...)
				f.mu.Unlock()
				wantHost := any(proto.HostAgy)
				if tc.host == "none" {
					wantHost = nil
				}
				if len(starts) != 1 || starts[0]["launcher"] != "agy" || starts[0]["host"] != wantHost ||
					starts[0]["permission_mode"] != tc.mode {
					t.Fatalf("read-only agy start = %v, want one on host %v in %s", starts, wantHost, tc.mode)
				}
				// A companion that went away fails the task (no pane in its place); otherwise it runs.
				if todo := onlyTodo(s); tc.away != (todo.StartError != "" && todo.DoneAt != nil) {
					t.Fatalf("to-do = %+v (companion away: %v)", todo, tc.away)
				}
			})
		})
	}
}
