package node

import (
	"strings"

	"github.com/sunfayao/persona/server/internal/osproc"
)

// agentImages are the programs an agent process runs as on Windows: Claude Code's native binary, Codex's, or either's
// script under node / bun (agentOfCmd decides from the command line).
var agentImages = map[string]bool{"claude": true, "codex": true, "node": true, "bun": true}

// envListing writes picked variables as an environment listing (NAME=value, NUL-separated) for the parsers shared
// with /proc and ps.
func envListing(env map[string]string) string {
	var b strings.Builder
	for k, v := range env {
		b.WriteString(k + "=" + v + "\x00")
	}
	return b.String()
}

// scanClaudeProcessesOS is scanClaudeProcesses from Windows' process table. The values are taken whole (a config
// directory under a home with a space in it is common here), not through the listing parsers.
func scanClaudeProcessesOS() map[int]procEnv {
	out := map[int]procEnv{}
	procs, _, err := listWinProcs(func(name string) bool { return agentImages[name] },
		"CLAUDE_CONFIG_DIR", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL")
	if err != nil {
		return out
	}
	for _, p := range procs {
		if !osproc.IsClaudeCommand(p.argv) {
			continue
		}
		pe := procEnv{ConfigDir: p.env["CLAUDE_CONFIG_DIR"], BaseURL: p.env["ANTHROPIC_BASE_URL"], Model: p.env["ANTHROPIC_MODEL"]}
		if pe != (procEnv{}) {
			out[p.pid] = pe
		}
	}
	return out
}
