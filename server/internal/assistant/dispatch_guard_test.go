package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

const (
	guardMachine = "server"
	guardSession = "session-guarded"
	otherSession = "session-other"
	guardSecret  = "AKIA-SECRET-VALUE token=do-not-print"
)

func newGuardFakeHub() *fakeHub {
	f := newFakeHub().withMachine()
	f.hello.Locale = string(i18n.En)
	f.known[guardSession] = true
	f.known[otherSession] = true
	return f
}

func guardPolicy(t *testing.T, deny ...string) DispatchGuard {
	t.Helper()
	return DispatchGuard{
		Enabled: true,
		Targets: []DispatchGuardTarget{{
			MachineID: guardMachine,
			SessionID: guardSession,
			Deny:      deny,
		}},
	}
}

func putGuard(t *testing.T, s *Service, g DispatchGuard) {
	t.Helper()
	if err := s.store().PutSetting(context.Background(), dispatchGuardSetting, g); err != nil {
		t.Fatal(err)
	}
}

func guardNamesOf(tools []proto.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func TestDispatchGuardRefusalAndToolNarrowing(t *testing.T) {
	f := newGuardFakeHub()
	s := newTestService(t, f)
	all := guardNamesOf(s.Tools(context.Background()))
	for _, name := range dispatchGuardToolNames {
		if !slices.Contains(all, name) {
			t.Fatalf("global tool list is missing %q", name)
		}
	}
	putGuard(t, s, guardPolicy(t, dispatchGuardToolNames...))

	if err := s.guardRefusal(context.Background(), guardMachine, guardSession, "task_plan"); err == nil || err.Error() != dispatchGuardRefusalText {
		t.Fatalf("guarded task_plan: %v", err)
	}
	res := s.Tool(context.Background(), ToolCall{
		MachineID: guardMachine, SessionID: guardSession, Name: "agent_start",
		Arguments: []byte(`{"prompt":"` + guardSecret + `"}`),
	})
	if !res.IsError || !strings.Contains(res.Content[0].Text, dispatchGuardRefusalText) {
		t.Fatalf("guarded tool call: %+v", res)
	}
	text := res.Content[0].Text
	for _, hidden := range []string{guardSecret, guardMachine, guardSession, "agent_start", "task_plan"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("refusal leaked %q: %q", hidden, text)
		}
	}

	visible := guardNamesOf(s.ToolsFor(context.Background(), guardMachine, guardSession))
	if len(visible) != len(all)-len(dispatchGuardToolNames) {
		t.Fatalf("narrowed tools %d, want %d", len(visible), len(all)-len(dispatchGuardToolNames))
	}
	for _, name := range dispatchGuardToolNames {
		if slices.Contains(visible, name) {
			t.Fatalf("target still lists guarded tool %q", name)
		}
	}
	if got := guardNamesOf(s.ToolsFor(context.Background(), guardMachine, otherSession)); !slices.Equal(got, all) {
		t.Fatalf("another session's tool list changed")
	}
	if got := guardNamesOf(s.Tools(context.Background())); !slices.Equal(got, all) {
		t.Fatalf("the global tool list changed")
	}
}

func TestDispatchGuardDefaultsAndFallback(t *testing.T) {
	f := newGuardFakeHub()
	s := newTestService(t, f)
	all := guardNamesOf(s.Tools(context.Background()))
	ctx := context.Background()

	checkOff := func(name string) {
		t.Helper()
		if err := s.guardRefusal(ctx, guardMachine, guardSession, "task_plan"); err != nil {
			t.Fatalf("%s: guard refused: %v", name, err)
		}
		if got := guardNamesOf(s.ToolsFor(ctx, guardMachine, guardSession)); !slices.Equal(got, all) {
			t.Fatalf("%s: tool list narrowed", name)
		}
	}
	checkOff("no policy")

	g := guardPolicy(t, "task_plan")
	g.Enabled = false
	putGuard(t, s, g)
	checkOff("explicitly disabled")

	past := time.Now().Add(-time.Minute).UTC()
	expired := guardPolicy(t, "task_plan")
	expired.ExpiresAt = &past
	putGuard(t, s, expired)
	checkOff("expired policy")

	enabled := guardPolicy(t, "task_plan")
	putGuard(t, s, enabled)
	if err := s.guardRefusal(ctx, guardMachine, otherSession, "task_plan"); err != nil {
		t.Fatalf("another target was guarded: %v", err)
	}
	if err := s.guardRefusal(ctx, guardMachine, guardSession, "notify_user"); err != nil {
		t.Fatalf("a non-dispatch tool was guarded: %v", err)
	}
	if err := s.guardRefusal(ctx, guardMachine, guardSession, "task_plan"); err == nil {
		t.Fatal("an enabled target was not refused")
	}
}

func TestDispatchGuardFailsClosedOnReadError(t *testing.T) {
	f := newGuardFakeHub()
	st := &guardDownStore{memStore: &memStore{vals: map[string][]byte{}}}
	s := newServiceOn(t, f, st)
	st.down = true

	err := s.guardRefusal(context.Background(), guardMachine, guardSession, "conversation_send")
	if err == nil || err.Error() != dispatchGuardRefusalText {
		t.Fatalf("read error: %v", err)
	}
	visible := guardNamesOf(s.ToolsFor(context.Background(), guardMachine, guardSession))
	for _, name := range dispatchGuardToolNames {
		if slices.Contains(visible, name) {
			t.Fatalf("dispatch tool %q stayed visible while the guard could not be read", name)
		}
	}
}

func TestDispatchGuardInvalidEnabledPolicyFailsClosed(t *testing.T) {
	f := newGuardFakeHub()
	s := newTestService(t, f)
	putGuard(t, s, DispatchGuard{
		Enabled: true,
		Targets: []DispatchGuardTarget{{MachineID: guardMachine, SessionID: guardSession, Deny: []string{"not-a-tool"}}},
	})
	if err := s.guardRefusal(context.Background(), guardMachine, guardSession, "task_plan"); err == nil || err.Error() != dispatchGuardRefusalText {
		t.Fatalf("invalid enabled policy: %v", err)
	}
	visible := guardNamesOf(s.ToolsFor(context.Background(), guardMachine, guardSession))
	for _, name := range dispatchGuardToolNames {
		if slices.Contains(visible, name) {
			t.Fatalf("invalid enabled policy left %q visible", name)
		}
	}
}

func TestDispatchGuardNarrowsSessionMCPListOnlyForTarget(t *testing.T) {
	f := newGuardFakeHub()
	s := newTestService(t, f)
	putGuard(t, s, guardPolicy(t, dispatchGuardToolNames...))
	ctx := context.Background()

	visible := guardNamesOf(s.sessionMCPTools(ctx, guardMachine, guardSession, true, false))
	for _, name := range dispatchGuardToolNames {
		if slices.Contains(visible, name) {
			t.Fatalf("actual MCP list still includes guarded tool %q", name)
		}
	}
	other := guardNamesOf(s.sessionMCPTools(ctx, guardMachine, otherSession, true, false))
	global := guardNamesOf(s.mcpTools(ctx, true))
	for _, name := range dispatchGuardToolNames {
		if !slices.Contains(other, name) || !slices.Contains(global, name) {
			t.Fatalf("untargeted MCP list lost %q", name)
		}
	}
	res := s.mcpCall(ctx, guardMachine, guardSession, "task_plan", json.RawMessage(`{}`), true)
	if !res.IsError || !strings.Contains(res.Content[0].Text, dispatchGuardRefusalText) {
		t.Fatalf("hidden tool was callable through mcpCall: %+v", res)
	}
}

type guardDownStore struct {
	*memStore
	down bool
}

func (s *guardDownStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if s.down && key == dispatchGuardSetting {
		return false, errors.New("database credential failure must stay out of refusals")
	}
	return s.memStore.Setting(ctx, key, out)
}
