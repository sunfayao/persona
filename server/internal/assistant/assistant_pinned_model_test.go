package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestProtectedQuotaRuntimeResumeIsLeftAlone(t *testing.T) {
	s, f := workService(t)
	f.withMachine()
	setSettings(t, s, func(v *Settings) { v.MachineID, v.MainMachineID, v.Main = "server", "server", "s1" })
	reset := time.Now().Add(time.Hour)
	c := limitTurn("s1", reset, true)
	c.Assistant, c.UpdatedAt = true, time.Now()
	f.setSessions("server", c)
	ctx := context.Background()
	if !s.protectedQuotaWait(ctx, "server", c) {
		t.Fatal("fixture is not a protected quota wait")
	}
	s.triageTurn(ctx, s.newWorkView(ctx), "server", c)
	e, tracked := s.workEntry([2]string{"server", "s1"})
	if !tracked || e.Work.On != proto.WaitExternal || e.WakeAt != nil {
		t.Fatalf("runtime-owned quota wait: %+v", e)
	}
	s.workTick(ctx, reset.Add(time.Hour))
	if len(f.sentToAgents()) != 0 {
		t.Fatal("interrupted the runtime's own quota continuation")
	}
}

func TestProtectedOrdinaryManagerSendKeepsExistingParams(t *testing.T) {
	s, f := workService(t)
	f.withMachine()
	setSettings(t, s, func(v *Settings) { v.MachineID, v.MainMachineID, v.Main = "server", "server", "s1" })
	if !s.protectedModelConversation(context.Background(), "server", "s1") {
		t.Fatal("fixture is not the protected main conversation")
	}
	if err := s.sendToSession(context.Background(), "server", "s1", "ordinary manager message"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.agentSent) != 1 || f.agentSent[0]["text"] != "ordinary manager message" {
		t.Fatalf("ordinary manager send changed: %v", f.agentSent)
	}
	// Service.call already attests is_main and supervision metadata independently of model selection.
	for _, field := range []string{"model", "launcher"} {
		if _, added := f.agentSent[0][field]; added {
			t.Fatalf("ordinary send resolved model field %s", field)
		}
	}
}
