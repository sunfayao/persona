package prompts

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The tool inventory is English only: whatever the language, a tool's description and schema are the same bytes, except
// that a language name printed with {{lang}} (the language an argument is written in) follows the owner's language.
func TestMCPToolsAreLanguageIndependent(t *testing.T) {
	for name := range mcpTools() {
		base := Tool(name)
		for lang, display := range map[string]string{"zh-Hans": "Simplified Chinese", "en": "English", "ja": "Japanese"} {
			got := Tool(name, lang)
			want := strings.ReplaceAll(base.Description, "Simplified Chinese", display)
			wantSchema := strings.ReplaceAll(string(base.InputSchema), "Simplified Chinese", display)
			if got.Description != want || string(got.InputSchema) != wantSchema {
				t.Errorf("%s/%s differs from the default definition by more than the language name", name, lang)
			}
		}
	}
}

func withoutDescriptions(v any) any {
	switch x := v.(type) {
	case map[string]any:
		delete(x, "description")
		for k, value := range x {
			x[k] = withoutDescriptions(value)
		}
	case []any:
		for i, value := range x {
			x[i] = withoutDescriptions(value)
		}
	}
	return v
}

func TestMCPLanguagesKeepSchemasAndSourceContracts(t *testing.T) {
	for name := range mcpTools() {
		var original any
		if err := json.Unmarshal(Tool(name).InputSchema, &original); err != nil {
			t.Fatal(err)
		}
		for _, lang := range []string{"en", "ja"} {
			tool := Tool(name, lang)
			var localized any
			if err := json.Unmarshal(tool.InputSchema, &localized); err != nil {
				t.Fatalf("%s/%s: %v", name, lang, err)
			}
			if tool.Name != name || !reflect.DeepEqual(withoutDescriptions(localized), withoutDescriptions(original)) {
				t.Errorf("%s/%s: schema contract changed", name, lang)
			}
		}
	}
	for _, lang := range []string{"en", "ja"} {
		for _, prefix := range []string{"Status:", "Stop:", "Answer:"} {
			if !strings.Contains(Tool("delegate_task", lang).Description, prefix) {
				t.Errorf("%s lost %s", lang, prefix)
			}
		}
		for _, name := range []string{"task_plan", "task_scope", "agent_send", "agent_supervise", "task_review"} {
			if strings.Contains(string(Tool(name, lang).InputSchema), "Chinese") {
				t.Errorf("%s/%s still requires Chinese output", name, lang)
			}
		}
	}
	for _, literal := range []string{"{{since}}", "{{since_date}}"} {
		if !strings.Contains(Tool("subscription_sources", "en").Description, literal) {
			t.Errorf("lost recipe literal %s", literal)
		}
	}
}

func TestMCPLocalizationPreservesAuthoredToolsAndAliases(t *testing.T) {
	for _, name := range []string{"task_plan", "unknown_tool"} {
		authored := MCPTool{Name: name, Description: "An extension's own definition", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)}
		if got := LocalizeTool(authored, "ja"); !reflect.DeepEqual(got, authored) {
			t.Fatal("rewrote authored tool")
		}
	}
	changedSchema := Tool("task_plan")
	changedSchema.InputSchema = json.RawMessage(`{"type":"object","properties":{"external_only":{"type":"string"}}}`)
	if got := LocalizeTool(changedSchema, "ja"); !reflect.DeepEqual(got, changedSchema) {
		t.Fatal("rewrote a source-specific schema")
	}
	leader := Tool("leader_task_plan")
	leader.Name = "task_plan"
	got := LocalizeTool(leader, "ja")
	if got.Name != "task_plan" || got.Description != Tool("leader_task_plan", "ja").Description {
		t.Fatal("lost restricted leader definition")
	}
	if again := LocalizeTool(got, "en"); again.Description != Tool("leader_task_plan", "en").Description {
		t.Fatal("failed language change")
	}
	raw := Tool("task_plan", "en")
	raw.InputSchema[0] = 'X'
	if Tool("task_plan", "en").InputSchema[0] != '{' {
		t.Fatal("caller mutated cached schema")
	}
}
