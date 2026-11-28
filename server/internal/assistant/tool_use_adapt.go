package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistant/toolnotes"
	"github.com/sunfayao/persona/server/internal/assistant/tooluse"
	"github.com/sunfayao/persona/server/internal/proto"
)

// What the tool record teaches (assistant/tooluse). toolUseLoop runs the friction dream, which drafts field notes and
// fix suggestions from repeated friction, and keeps the adaptive core tools of the main chat's runtime, which
// withToolLoading marks (coreRanks) and the tool-list revision carries (coreToolsRevision).

// The declaration budgets of the runtimes that load only their core tools up front, in bytes of tooluse.DeclaredSize.
// A ranked core set is cut to its runtime's budget; a runtime that loads every tool (Codex) has none, and the static
// list is never cut, as before.
const (
	// agyCoreBudget: Antigravity's MCP proxy declares at most 44,000 bytes (AGY_DECLARE_BUDGET), 248 of them
	// persona_call's.
	agyCoreBudget = 43_750
	// claudeCoreBudget: Claude Code sends every core tool in full with every request. The static list, as the main
	// conversation lists it with computer use, the browser and the Lark CLI on, declares 57.6 KB (2026-11-27), so a
	// ranked set costs a request no more than the static one did.
	claudeCoreBudget = 58_000
)

func coreBudget(runtime string) int {
	switch runtime {
	case "agy":
		return agyCoreBudget - toolnotes.MaxTotal // room for the field notes added after the marks (withToolNotes)
	case "claude":
		return claudeCoreBudget
	}
	return 0
}

// coreRanks are the core tools of a list withToolLoading ranks, with their CoreRank: all the static coreTools while
// the main chat's runtime has no ranking of its own (tooluse.Book.CoreOrder), else its ranking, cut to its budget.
func (s *Service) coreRanks(ctx context.Context, tools []proto.Tool) map[string]int {
	order, budget := coreTools, 0
	if s.toolUse != nil {
		runtime := s.callerRuntime(ctx)
		if ranked := s.toolUse.CoreOrder(runtime); ranked != nil {
			order, budget = ranked, coreBudget(runtime)
		}
	}
	listed := make(map[string]int, len(tools))
	for _, t := range tools {
		listed[t.Name] = 0
		if budget > 0 {
			listed[t.Name] = tooluse.DeclaredSize(t.Name, t.Description, t.InputSchema)
		}
	}
	return tooluse.CoreRanks(order, listed, budget)
}

// coreToolsRevision is what the tool-list revision (toolsCursor) of a list withToolLoading ranks adds for the adaptive
// core tools: a digest of the main chat's runtime and its ranking; "" while the static list applies. A ranking that
// changes unannounced thus reaches a running conversation at its watch's next heartbeat.
func (s *Service) coreToolsRevision(ctx context.Context, assistant bool) string {
	if !assistant || s.toolUse == nil {
		return ""
	}
	runtime := s.callerRuntime(ctx)
	ranked := s.toolUse.CoreOrder(runtime)
	if ranked == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(runtime + "\x00" + strings.Join(ranked, "\x00")))
	return ":core=" + hex.EncodeToString(sum[:8])
}

// adaptToolUse is toolUseLoop's step once the record is loaded (loaded) and at each tick: the friction dream, then the
// main chat's runtime's core tools computed again when they are due. Every tool watch is woken when they change, and
// when a stored ranking was just loaded: a list made before had the static ones.
func (s *Service) adaptToolUse(ctx context.Context, loaded bool) {
	if err := s.toolUse.Dream(ctx); err != nil {
		s.logf("tool use: friction dream: %v", err)
	}
	runtime := s.callerRuntime(ctx)
	changed, err := s.toolUse.RefreshCore(ctx, runtime, coreTools)
	if err != nil {
		s.logf("tool use: core tools: %v", err)
	}
	if changed || loaded && s.toolUse.CoreOrder(runtime) != nil {
		s.mcpChanges.notify()
	}
}
