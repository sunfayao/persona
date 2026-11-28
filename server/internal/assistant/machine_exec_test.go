package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/machineexec"
	"github.com/sunfayao/persona/server/internal/proto"
)

// machine_exec's wiring in the root (machine_exec.go); what the tool decides is assistant/machineexec's tests.

// execHub is a fake hub with the conversations' computer "server" and "mac" (named "Mac mini"), whose node answers
// machine.exec; asked are the params it got.
func execHub() (*fakeHub, *[]proto.MachineExecParams) {
	f := newFakeHub().withMachine()
	f.sessions["mac"] = nil
	f.devices = []Device{{ID: "server", Name: "Server", Online: true, Reachable: true}, {ID: "mac", Name: "Mac mini", Online: true, Reachable: true}}
	asked := &[]proto.MachineExecParams{}
	f.answerNode(proto.MethodMachineExec, func(machine string, raw json.RawMessage) (any, error) {
		var p proto.MachineExecParams
		_ = json.Unmarshal(raw, &p)
		*asked = append(*asked, p) // under the fake's lock
		return proto.MachineExecResult{ExitCode: 0, DurationMS: 5, Stdout: "up 3 days\n"}, nil
	})
	return f, asked
}

// The main conversation runs a command on another computer: its node is asked with the caller named for the hub's
// audit, and its answer is the tool's JSON.
func TestMachineExecRunsFromTheMainConversation(t *testing.T) {
	f, asked := execHub()
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.Main = "main-1" })
	res := s.Tool(context.Background(), ToolCall{Name: "machine_exec", MachineID: "server", SessionID: "main-1",
		Arguments: json.RawMessage(`{"machine":"Mac mini","command":"uptime"}`)})
	if text := resultText(res); res.IsError || text != `{"machine":"Mac mini","exit_code":0,"duration_ms":5,"stdout":"up 3 days\n"}` {
		t.Fatalf("result %s", text)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(*asked) != 1 || (*asked)[0].Caller != "main conversation main-1 on server" || (*asked)[0].TimeoutMS != 20000 {
		t.Fatalf("the node was asked %+v", *asked)
	}
}

// A call from outside the bounds of the conversations' tools (a session on a computer no conversation of this manager
// runs on: an Agent's) is refused plainly, whatever path it came by, and no node is asked.
func TestMachineExecRefusesAnAgent(t *testing.T) {
	f, asked := execHub()
	s := newTestService(t, f)
	res := s.Tool(context.Background(), ToolCall{Name: "machine_exec", MachineID: "mac", SessionID: "agent-1",
		Arguments: json.RawMessage(`{"machine":"server","command":"cat /etc/hosts"}`)})
	if text := resultText(res); !res.IsError || !strings.Contains(text, "an Agent runs commands in its own terminal") {
		t.Fatalf("result %s", text)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(*asked) != 0 {
		t.Fatalf("the node was asked %+v", *asked)
	}
}

// assistant.machine_exec is a persona_settings key: on for every computer without a card until changed, and changed
// only as the registry's settings are, through the approval card's apply; a partial value is refused.
func TestMachineExecSettingIsChangedOnTheApprovalCard(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	ctx := context.Background()
	if v, err := s.machineExecSetting(ctx); err != nil || !v.Enabled || v.Confirm || v.Machines == nil || len(v.Machines) != 0 {
		t.Fatalf("default %+v, %v", v, err)
	}
	var setting ownSetting
	for _, o := range s.ownSettings() {
		if o.Key == machineexec.SettingKey {
			setting = o
		}
	}
	want := json.RawMessage(`{"enabled":true,"confirm":true,"machines":["mac"]}`)
	change, err := setting.set(ctx, want)
	if err != nil || change.apply == nil || !strings.Contains(change.card("en"), `"confirm":true`) {
		t.Fatalf("set: %+v, %v", change, err)
	}
	if v, _ := s.machineExecSetting(ctx); v.Confirm {
		t.Fatal("changed before the card was approved")
	}
	if _, err := change.apply(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.machineExecSetting(ctx); !v.Enabled || !v.Confirm || len(v.Machines) != 1 || v.Machines[0] != "mac" {
		t.Fatalf("after the card %+v", v)
	}
	if change, err := setting.set(ctx, want); err != nil || change.apply != nil {
		t.Fatalf("the same value again: %+v, %v", change, err)
	}
	if _, err := setting.set(ctx, json.RawMessage(`{"confirm":false}`)); err == nil {
		t.Fatal("a partial value was taken")
	}
}

// The approval card shows the computer, with its id when another computer has its name, the command as written and its
// directory, and waits ten minutes.
func TestMachineExecCardShowsTheCommandAsWritten(t *testing.T) {
	f, _ := execHub()
	f.devices[0].Name = "Mac mini"
	s := newTestService(t, f)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := machineexec.Confirmation{Caller: machineexec.Caller{Machine: "server", Session: "main-1", Class: "main"},
		Machine: machineexec.Machine{ID: "mac", Name: "Mac mini"}, Command: "rm -rf ~/tmp/x && echo 'ok'", Cwd: "~/src",
		Timeout: 30 * time.Second}
	ix := s.machineExecInteraction(context.Background(), c, now)
	if ix.Kind != proto.IxPermission || ix.ToolName != "machine_exec" || ix.MachineID != "server" || ix.SessionID != "main-1" ||
		!ix.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("card %+v", ix)
	}
	if !strings.Contains(ix.Summary, "Mac mini (mac)") || !strings.Contains(ix.Summary, "\n$ rm -rf ~/tmp/x && echo 'ok'\n") ||
		!strings.HasSuffix(ix.Summary, "~/src") {
		t.Fatalf("summary %q", ix.Summary)
	}
	var input map[string]any
	if err := json.Unmarshal(ix.ToolInput, &input); err != nil || input["command"] != c.Command || input["timeout_seconds"] != 30.0 {
		t.Fatalf("input %s", ix.ToolInput)
	}
}
