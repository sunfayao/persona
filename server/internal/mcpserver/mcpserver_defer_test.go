package mcpserver

import (
	"context"
	"reflect"
	"testing"
)

// An assistant tool the manager deferred (#7317) gets no alwaysLoad marker, so Claude Code keeps it behind ToolSearch;
// the others of the same list keep theirs (#5838).
func TestToolsListDeferredAssistantToolHasNoAlwaysLoad(t *testing.T) {
	t.Setenv("PERSONA_ASSISTANT", "1")
	fakeNode(t, func(int) ([]byte, error) {
		return []byte(`{"tools":[{"name":"ask_user","description":"ask"},{"name":"inbox_move","description":"move","defer_loading":true}]}`), nil
	})
	res, rerr := handle(context.Background(), "test", listRequest())
	if rerr != nil {
		t.Fatal(rerr)
	}
	tools := res.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %+v", tools)
	}
	if meta, ok := tools[0]["_meta"].(map[string]any); !ok || meta["anthropic/alwaysLoad"] != true {
		t.Fatalf("a tool not deferred lacks alwaysLoad metadata: %+v", tools[0])
	}
	if _, present := tools[1]["_meta"]; present || tools[1]["name"] != "inbox_move" {
		t.Fatalf("a deferred assistant tool is still marked alwaysLoad: %+v", tools[1])
	}
}

// listedMeta is each listed tool's _meta, by name, for the node's tools JSON.
func listedMeta(t *testing.T, nodeTools string) map[string]map[string]any {
	t.Helper()
	fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":` + nodeTools + `}`), nil })
	res, rerr := handle(context.Background(), "test", listRequest())
	if rerr != nil {
		t.Fatal(rerr)
	}
	out := map[string]map[string]any{}
	for _, tool := range res.(map[string]any)["tools"].([]map[string]any) {
		meta, _ := tool["_meta"].(map[string]any)
		out[tool["name"].(string)] = meta
	}
	return out
}

// A runtime with no ToolSearch that declares only what fits its budget (Antigravity's proxy) reads the manager's
// choice from Persona's own marks: its core tools are persona/core with their rank, whatever assistant.load_all_tools
// says, ahead of them a command gate's tool the node adds (rank 0), and with that setting on the others are
// persona/fill. Claude Code's alwaysLoad stays as it was, and a deferred tool has no _meta at all. An older manager
// ranks nothing: then no tool gets a Persona mark, a gate's neither, and the runtime keeps its own rule. An agent's
// list has no _meta.
func TestToolsListMarksCoreToolsForRuntimesWithoutToolSearch(t *testing.T) {
	t.Setenv("PERSONA_ASSISTANT", "1")
	always := map[string]any{"anthropic/alwaysLoad": true}
	gate := `{"name":"chat_api","description":"Run ex","shell":["ex","scripts/ex.sh"]}`
	for _, tc := range []struct {
		name, tools string
		want        map[string]map[string]any
	}{
		{"load_all_tools off", `[{"name":"agent_read","core_rank":1},{"name":"ask_user","core_rank":21},` +
			`{"name":"inbox_move","defer_loading":true},` + gate + `]`, map[string]map[string]any{
			"agent_read": {"anthropic/alwaysLoad": true, "persona/core": true, "persona/rank": 1},
			"ask_user":   {"anthropic/alwaysLoad": true, "persona/core": true, "persona/rank": 21},
			"inbox_move": nil,
			"chat_api":   {"anthropic/alwaysLoad": true, "persona/core": true, "persona/rank": 0},
		}},
		{"load_all_tools on", `[{"name":"agent_read","core_rank":1},{"name":"inbox_move","fill":true},` + gate + `]`,
			map[string]map[string]any{
				"agent_read": {"anthropic/alwaysLoad": true, "persona/core": true, "persona/rank": 1},
				"inbox_move": {"anthropic/alwaysLoad": true, "persona/fill": true},
				"chat_api":   {"anthropic/alwaysLoad": true, "persona/core": true, "persona/rank": 0},
			}},
		{"an older manager", `[{"name":"agent_read"},{"name":"inbox_move","defer_loading":true},` + gate + `]`,
			map[string]map[string]any{"agent_read": always, "inbox_move": nil, "chat_api": always}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := listedMeta(t, tc.tools); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("_meta by tool:\n got %v\nwant %v", got, tc.want)
			}
		})
	}
	t.Setenv("PERSONA_ASSISTANT", "0")
	got := listedMeta(t, `[{"name":"todo_get","core_rank":3},{"name":"inbox_move","fill":true}]`)
	if want := (map[string]map[string]any{"todo_get": nil, "inbox_move": nil}); !reflect.DeepEqual(got, want) {
		t.Fatalf("an agent's tools carry _meta: %v", got)
	}
}
