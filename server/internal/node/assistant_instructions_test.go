package node

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// Antigravity reads a rule file only up to 24,000 bytes, and About me and the extensions' instructions come last: with
// agents_md_split the node keeps AGENTS.md within it, cut before the section that would not fit, and writes the rest
// to GEMINI.md, which Antigravity reads beside it; once the text fits again, GEMINI.md goes. CLAUDE.md stays whole.
func TestAssistantPrepareSplitsAgentsMDOverItsReadersLimit(t *testing.T) {
	t.Setenv("PERSONA_SKILLS_DIR", t.TempDir())
	t.Setenv("PERSONA_PLUGIN_SKILLS_DIR", t.TempDir())
	n, _ := testNode(t)
	n.dir, n.bin, n.logger = t.TempDir(), "/opt/example/personad", log.New(io.Discard, "", 0)
	prepare := func(agents string, split int) (cwd string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"dir": ".persona/assistant", "claude_md": agents, "agents_md": agents, "agents_md_split": split})
		out, err := n.handle(t.Context(), "assistant.prepare", raw)
		if err != nil {
			t.Fatal(err)
		}
		return out.(map[string]any)["cwd"].(string)
	}
	read := func(cwd, name string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(cwd, name))
		return string(b), err == nil
	}
	section := func(title string, n int) string {
		return "## " + title + "\n" + strings.Repeat("一条规则。\n", n) + "\n"
	}
	head, about, lark := "# Persona assistant\n\n"+section("Working with the user", 900), section("About the user", 700), section("Lark", 60)
	long := head + about + lark // over 24,000 bytes, with About me across the limit
	cwd := prepare(long, 24_000)
	agents, _ := read(cwd, "AGENTS.md")
	gemini, ok := read(cwd, "GEMINI.md")
	if agents != head || !ok || gemini != about+lark {
		t.Fatalf("AGENTS.md %d bytes, GEMINI.md %d bytes (%v): want the cut before About me", len(agents), len(gemini), ok)
	}
	if claude, _ := read(cwd, "CLAUDE.md"); claude != long {
		t.Fatalf("CLAUDE.md is %d bytes, want it whole (%d)", len(claude), len(long))
	}
	if cwd = prepare(head+lark, 24_000); func() bool { _, ok := read(cwd, "GEMINI.md"); return ok }() {
		t.Fatal("GEMINI.md stayed after the instructions fit AGENTS.md again")
	}
	if cwd = prepare(long, 0); func() bool { a, _ := read(cwd, "AGENTS.md"); _, ok := read(cwd, "GEMINI.md"); return a != long || ok }() {
		t.Fatal("without agents_md_split (a runtime that reads AGENTS.md whole, an older manager) AGENTS.md was not written whole")
	}
	for _, tc := range []struct {
		md           string
		max          int
		agents, rest string
	}{
		{"short", 100, "short", ""},
		{"line one\nline two\n", 12, "line one\n", "line two\n"},   // no section fits: the last whole line
		{"## a\nx\n## b\ny\n", 12, "## a\nx\n", "## b\ny\n"},       // before the section that would not fit
		{strings.Repeat("x", 30), 10, strings.Repeat("x", 30), ""}, // nothing to cut on
	} {
		if agents, rest := splitRules(tc.md, tc.max); agents != tc.agents || rest != tc.rest {
			t.Errorf("splitRules(%q, %d) = %q, %q; want %q, %q", tc.md, tc.max, agents, rest, tc.agents, tc.rest)
		}
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
