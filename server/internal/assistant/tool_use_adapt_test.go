package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/tooluse"
	"github.com/sunfayao/persona/server/internal/proto"
)

// adaptRecord gives s a tool record on a clock the test sets, with no store.
func adaptRecord(s *Service, now *time.Time) {
	s.toolUse = tooluse.New(tooluse.Host{Now: func() time.Time { return *now }, Logf: s.logf})
}

// asMainRuntime makes runtime the main chat's (callerRuntime), as the launcher setting says.
func asMainRuntime(t *testing.T, s *Service, runtime string) {
	t.Helper()
	setSettings(t, s, func(v *Settings) { v.Launcher = runtime })
	s.runtimeOfCalls.mu.Lock()
	s.runtimeOfCalls.at = time.Time{}
	s.runtimeOfCalls.mu.Unlock()
}

// With enough recorded use the main chat's runtime has its own core tools, and an assistant conversation's list marks
// the most called of them that fit the runtime's budget: of twelve tools of 5,000 bytes, called less and less, eight
// on Antigravity (43,750 bytes), eleven on Claude Code (58,000), all on Codex, which loads every tool. A runtime with
// no ranking keeps the static list, which has none of them.
func TestAdaptiveCoreToolsFitTheRuntimesBudget(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	ctx := context.Background()
	now := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	adaptRecord(s, &now)
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	var tools []proto.Tool
	for i := range 12 {
		name := fmt.Sprintf("t%02d", i+1)
		pad := 5000 - tooluse.DeclaredSize(name, "", schema)
		tools = append(tools, proto.Tool{Name: name, Description: strings.Repeat("x", pad), InputSchema: schema})
	}
	for _, runtime := range []string{"agy", "claude", "codex"} {
		for i, tool := range tools {
			for range 60 - 4*i {
				s.toolUse.Record(tooluse.Call{Tool: tool.Name, Runtime: runtime, OK: true})
			}
		}
	}
	for _, tc := range []struct {
		runtime string
		core    int
	}{{"agy", 8}, {"claude", 11}, {"codex", 12}, {"pi", 0}} {
		asMainRuntime(t, s, tc.runtime)
		if _, err := s.toolUse.RefreshCore(ctx, tc.runtime, coreTools); err != nil {
			t.Fatal(err)
		}
		size := 0
		for i, tool := range s.withToolLoading(ctx, "", tools, true) {
			rank := 0
			if i < tc.core {
				rank = i + 1
				size += tooluse.DeclaredSize(tool.Name, tool.Description, tool.InputSchema)
			}
			if tool.CoreRank != rank || tool.DeferLoading != (rank == 0) {
				t.Errorf("%s: %s ranked %d, deferred %v; want rank %d", tc.runtime, tool.Name, tool.CoreRank, tool.DeferLoading, rank)
			}
		}
		if budget := coreBudget(tc.runtime); budget > 0 && size > budget {
			t.Errorf("%s: %d bytes of core tools, over its budget of %d", tc.runtime, size, budget)
		}
	}
}

// A new ranking wakes every tool watch, and the revision of an assistant conversation's tool list carries it, so a
// running conversation is sent its new core tools; an agent's list, which has no core tools, does not change.
func TestAdaptiveCoreToolsReachRunningConversations(t *testing.T) {
	s := newFleetTest(t).s
	ctx := context.Background()
	now := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	adaptRecord(s, &now)
	if got := s.coreToolsRevision(ctx, true); got != "" {
		t.Fatalf("the static list's revision: %q", got)
	}
	for range 300 {
		s.toolUse.Record(tooluse.Call{Tool: "agent_read", Runtime: "claude", OK: true})
	}
	base, _ := s.mcpChanges.snapshot()
	s.adaptToolUse(ctx, false)
	next, _ := s.mcpChanges.snapshot()
	if next == base {
		t.Fatal("a new ranking woke no tool watch")
	}
	revision := s.coreToolsRevision(ctx, true)
	if !strings.HasPrefix(revision, ":core=") || !strings.HasSuffix(s.toolsCursor(ctx, "mac", "o1", true, next), revision) {
		t.Fatalf("revision %q, cursor %q", revision, s.toolsCursor(ctx, "mac", "o1", true, next))
	}
	if got := s.coreToolsRevision(ctx, false); got != "" {
		t.Fatalf("an agent's revision: %q", got)
	}
	s.adaptToolUse(ctx, false)
	if again, _ := s.mcpChanges.snapshot(); again != next {
		t.Fatal("a ranking not yet due woke the tool watches")
	}
	s.adaptToolUse(ctx, true)
	if loaded, _ := s.mcpChanges.snapshot(); loaded == next {
		t.Fatal("a stored ranking just loaded woke no tool watch")
	}
}
