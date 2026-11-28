package assistant

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/closeout"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// The ceilings of what conversations carry in every request, set by the slim-down of 2026-11-28 to what it left.
// They only go down: a change that makes room lowers its line here; one that needs more removes as much elsewhere
// first. Raising a line to make this test pass is a padded baseline (AGENTS/09), not a fix.
const (
	// supervisionInstructionsBudget: the static supervision instructions every supervisor carries, in bytes. Each
	// signal's own rule comes with the supervision message that carries the signal.
	supervisionInstructionsBudget = 6_650
	// assistantToolsBudget: the tools an assistant conversation's own list can hold (toolNames), before the gates
	// leave out those of features that are off.
	assistantToolsBudget = 48
	// agentToolsBudget: the Agents' own tools an Agent always lists (listedAgentTools).
	agentToolsBudget = 4
	// coreToolBytesBudget: the static core tools declared up front, in wire bytes ({name, description, input_schema}
	// as JSON), the extensions' and the Lark tools aside.
	coreToolBytesBudget = 41_576
	// readonlyRulesBudget and writeRulesBudget: the framework rules an Agent with the Persona tools gets with a
	// read-only task, and with a task that changes things and has no delivery standard, in bytes.
	readonlyRulesBudget = 1_050
	writeRulesBudget    = 1_560
	// leaderToolsBudget and leaderToolBytesBudget: a to-do supervisor's own tools (leaderToolList), which it lists
	// beside the Agents' own in every request, and their wire bytes.
	leaderToolsBudget     = 5
	leaderToolBytesBudget = 9_539
)

func TestLeanBudgets(t *testing.T) {
	check := func(what string, got, budget int) {
		t.Helper()
		t.Logf("%s: %d (budget %d)", what, got, budget)
		if got > budget {
			t.Errorf("%s is %d, over its budget of %d: make room elsewhere instead of raising it", what, got, budget)
		}
	}
	check("static supervision instructions (bytes)", len(prompts.RenderSupervisionInstructions()), supervisionInstructionsBudget)
	check("assistant tools", len(toolNames), assistantToolsBudget)
	check("Agents' always-listed tools", len((&Service{}).listedAgentTools()), agentToolsBudget)

	core := 0
	for _, name := range coreTools {
		if strings.HasPrefix(name, "lark_") || strings.HasPrefix(name, "larkcli_") {
			continue
		}
		tool := prompts.Tool(name)
		b, err := json.Marshal(map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.InputSchema})
		if err != nil {
			t.Fatal(err)
		}
		core += len(b)
	}
	check("static core tools (wire bytes)", core, coreToolBytesBudget)

	leader := 0
	for _, tool := range leaderToolList() {
		b, err := json.Marshal(map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.InputSchema})
		if err != nil {
			t.Fatal(err)
		}
		leader += len(b)
	}
	check("a to-do supervisor's own tools", len(leaderToolList()), leaderToolsBudget)
	check("a to-do supervisor's own tools (wire bytes)", leader, leaderToolBytesBudget)

	handoff := prompts.RenderTaskHandoff(prompts.TaskHandoff{MCP: true, Language: "English"})
	readonly := handoff + "\n\n" + prompts.RenderAgentCloseoutBrief(prompts.AgentCloseoutBrief{
		Form: closeout.MarkdownReadonly("English"), Language: "English", Readonly: true})
	write := handoff + "\n\n" + prompts.RenderAgentCloseoutBrief(prompts.AgentCloseoutBrief{
		Form: closeout.MarkdownWrite("English"), Language: "English"})
	check("a read-only task's rules (bytes)", len(readonly), readonlyRulesBudget)
	check("a write task's rules (bytes)", len(write), writeRulesBudget)
}
