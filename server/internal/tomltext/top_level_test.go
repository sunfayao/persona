package tomltext

import (
	"strings"
	"testing"
)

// Only top-level strings and bools are read; a key of the same name inside a table is not the setting.
func TestCodexTopLevel(t *testing.T) {
	t.Parallel()
	v := TopLevel(sampleCodexConfig)
	if v["model"] != "gpt-old" || v["model_reasoning_effort"] != "high" || v["hide_agent_reasoning"] != false {
		t.Fatalf("%v", v)
	}
	if _, ok := v["notify"]; ok {
		t.Fatal("an array is not a setting we read")
	}
}

// Setting replaces the line in place, adding goes before the first table, removing drops the line; everything else
// in the file stays byte for byte.
func TestCodexSetKeepsTheRest(t *testing.T) {
	t.Parallel()
	got := Set(sampleCodexConfig, "model", "gpt-5.5")
	want := `# my codex
model = "gpt-5.5"
model_reasoning_effort = 'high'
hide_agent_reasoning = false
notify = ["a", "b"]

[model_providers.example]
name = "example"
model = "not-top-level"

[projects."/home/me/code"]
trust_level = "trusted"
`
	if got != want {
		t.Fatalf("replace:\n%s", got)
	}
	got = Set(sampleCodexConfig, "sandbox_mode", "workspace-write")
	if want := `notify = ["a", "b"]
sandbox_mode = "workspace-write"

[model_providers.example]`; !strings.Contains(got, want) {
		t.Fatalf("add:\n%s", got)
	}
	got = Set(sampleCodexConfig, "hide_agent_reasoning", nil)
	if strings.Contains(got, "hide_agent_reasoning") || !strings.Contains(got, "model_reasoning_effort = 'high'\nnotify") {
		t.Fatalf("remove:\n%s", got)
	}
	if got := Set("", "model", `a "quoted" one`); got != "model = \"a \\\"quoted\\\" one\"\n" {
		t.Fatalf("empty file: %q", got)
	}
	if v := TopLevel(Set("", "model", `a "quoted" one`)); v["model"] != `a "quoted" one` {
		t.Fatalf("round trip: %v", v)
	}
}

const sampleCodexConfig = `# my codex
model = "gpt-old"   # the default one
model_reasoning_effort = 'high'
hide_agent_reasoning = false
notify = ["a", "b"]

[model_providers.example]
name = "example"
model = "not-top-level"

[projects."/home/me/code"]
trust_level = "trusted"
`
