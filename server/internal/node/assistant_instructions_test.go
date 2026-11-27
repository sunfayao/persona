package node

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// assistant.prepare writes CLAUDE.md and AGENTS.md each in the words of the agent that reads it; a manager from before
// agents_md gets both files written with claude_md, as before.
func TestAssistantPrepareWritesEachAgentsInstructions(t *testing.T) {
	t.Setenv("PERSONA_SKILLS_DIR", t.TempDir())
	t.Setenv("PERSONA_PLUGIN_SKILLS_DIR", t.TempDir())
	n, _ := testNode(t)
	n.dir, n.bin, n.logger = t.TempDir(), "/opt/example/personad", log.New(io.Discard, "", 0)
	prepare := func(params map[string]string) (claude, agents string) {
		t.Helper()
		params["dir"] = ".persona/assistant"
		raw, _ := json.Marshal(params)
		out, err := n.handle(t.Context(), "assistant.prepare", raw)
		if err != nil {
			t.Fatal(err)
		}
		cwd := out.(map[string]any)["cwd"].(string)
		read := func(name string) string {
			b, err := os.ReadFile(filepath.Join(cwd, name))
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
		return read("CLAUDE.md"), read("AGENTS.md")
	}
	for _, tc := range []struct {
		name                 string
		params               map[string]string
		wantClaude, wantRest string
	}{
		{"each its own", map[string]string{"claude_md": "for Claude", "agents_md": "for the others"}, "for Claude", "for the others"},
		{"an older manager", map[string]string{"claude_md": "for everyone"}, "for everyone", "for everyone"},
		{"with the work guide", map[string]string{"claude_md": "full", "agents_md": "full for the others", "work_guide_md": "short",
			"agents_work_guide_md": "short for the others"}, "short", "short for the others"},
		{"an older manager with the work guide", map[string]string{"claude_md": "full", "work_guide_md": "short"}, "short", "short"},
	} {
		claude, agents := prepare(tc.params)
		if claude != tc.wantClaude || agents != tc.wantRest {
			t.Errorf("%s: CLAUDE.md %q, AGENTS.md %q", tc.name, claude, agents)
		}
	}
}

// Antigravity reads AGENTS.md again at every turn, also while the manager prepares another conversation's message in
// the same directory: the file is replaced whole when it changes, and left alone when it does not.
func TestAssistantPrepareReplacesInstructionsWhole(t *testing.T) {
	t.Setenv("PERSONA_SKILLS_DIR", t.TempDir())
	t.Setenv("PERSONA_PLUGIN_SKILLS_DIR", t.TempDir())
	n, _ := testNode(t)
	n.dir, n.bin, n.logger = t.TempDir(), "/opt/example/personad", log.New(io.Discard, "", 0)
	prepare := func(agents string) os.FileInfo {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"dir": ".persona/assistant", "claude_md": "for Claude", "agents_md": agents})
		out, err := n.handle(t.Context(), "assistant.prepare", raw)
		if err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(filepath.Join(out.(map[string]any)["cwd"].(string), "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	first := prepare("v1")
	if again := prepare("v1"); !os.SameFile(first, again) {
		t.Fatal("the same instructions were written again")
	}
	if changed := prepare("v2"); os.SameFile(first, changed) {
		t.Fatal("changed instructions were written into the file a reader may have open, not replaced")
	}
}

// A Claude conversation resumes in a fresh process when its CLAUDE.md changed, and only then: AGENTS.md, which it
// does not read, may change on its own now (another launcher for the main chat).
func TestInstructionsStaleReadsOnlyItsOwnFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("CLAUDE.md", "for Claude")
	write("AGENTS.md", "for the others")
	claude := &assistantProc{agent: "claude", cwd: dir, instructions: instructionsDigest(dir, "claude")}
	codex := &assistantProc{agent: "codex", cwd: dir, instructions: instructionsDigest(dir, "codex")}
	if claude.instructions == "" || claude.instructions == codex.instructions {
		t.Fatalf("digests: %q, %q", claude.instructions, codex.instructions)
	}
	write("AGENTS.md", "for the others, changed")
	if claude.instructionsStale() || codex.instructionsStale() {
		t.Fatal("a changed AGENTS.md made a process stale")
	}
	write("CLAUDE.md", "for Claude, changed")
	if !claude.instructionsStale() {
		t.Fatal("Claude kept the process that read the old CLAUDE.md")
	}
	if instructionsDigest(t.TempDir(), "claude") != "" {
		t.Fatal("a directory without instructions has a digest")
	}
}
