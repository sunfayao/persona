package node

import (
	"testing"
)

// Run Agent Here with a Codex launcher (#5504): Codex starts in the shell pane the user picked, with no first message
// (codex_waiting.go). The pane is the user's own: a start that fails leaves it open and queues nothing to close later,
// and one that comes up answers with the waiting row of that pane, whose first message becomes its thread, with the
// user's shell history above Codex on the screen.

// Codex launchers say the node starts them in a pane the user picked; Claude Code's need no word for it.
func TestCodexLaunchersRunHere(t *testing.T) {
	list, _, _, _ := mergeLaunchers(launcherBegin + "\x1d\x1d/usr/local/bin/codex\n" + launcherEnd) // plain codex on the PATH
	aliases, _ := parseCodexAliases("cx\x1fcodex\x1f-m\x1fgpt-6\x1f")                               // a ~/.zshrc alias running codex
	list = append(list, aliases...)
	var codex int
	for _, l := range list {
		if l.Agent == "codex" {
			codex++
			if !l.RunHere || !l.PromptOptional {
				t.Errorf("%s: run_here %v, prompt_optional %v", l.Name, l.RunHere, l.PromptOptional)
			}
		} else if l.RunHere {
			t.Errorf("%s (%s) says run_here", l.Name, l.Agent)
		}
	}
	if codex != 2 {
		t.Fatalf("Codex launchers listed: %+v", list)
	}
}
