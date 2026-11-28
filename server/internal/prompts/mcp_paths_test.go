package prompts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The MCP tools' descriptions are the same for every hub, read as they are (not templates): none may name a folder
// of the default layout, which is somewhere else once PERSONA_HOME moves it.
func TestMCPToolsDoNotNameTheDefaultFolders(t *testing.T) {
	for _, n := range ToolNames() {
		if d := Tool(n).Description; strings.Contains(d, "~/.persona") {
			t.Errorf("%s: the description names a default folder: %q", n, d)
		}
	}
}

// A description's long tail lives in persona-work's references, which every Assistant directory has under
// .claude/skills whether or not the work guide is on (assistantskills.Sync): each one a description points at is
// shipped, and the largest tools keep their pointer.
func TestMCPToolsPointAtShippedReferences(t *testing.T) {
	ref := regexp.MustCompile(`\.claude/skills/persona-work/references/([a-z0-9-]+\.md)`)
	for _, n := range ToolNames() {
		for _, m := range ref.FindAllStringSubmatch(Tool(n).Description, -1) {
			if _, err := os.Stat(filepath.Join("..", "assistantskills", "builtin", "persona-work", "references", m[1])); err != nil {
				t.Errorf("%s points at %s, which persona-work does not ship: %v", n, m[0], err)
			}
		}
	}
	for _, n := range []string{"task_plan", "task_review", "agent_start", "schedule_routine", "schedule_followup",
		"agent_send", "ask_user", "persona_settings", "agents_list"} {
		if !ref.MatchString(Tool(n).Description) {
			t.Errorf("%s no longer points at its reference", n)
		}
	}
}
