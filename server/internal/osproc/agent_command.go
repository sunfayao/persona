package osproc

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// IsClaudeCommand: the command line runs Claude Code — its native binary, or its script under node / bun.
func IsClaudeCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch ProgramName(argv[0]) {
	case "claude":
		return true
	case "node", "bun":
		return len(argv) > 1 && strings.Contains(argv[1], "claude")
	}
	return false
}

// PaneEnvRE finds a host's pane variable (host.PaneEnv) in a process's environment, NUL- (Linux) or space-separated
// (ps -E).
func PaneEnvRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[\x00\s])` + regexp.QuoteMeta(name) + `=([^\x00\s]+)`)
}

// ProgramName is the command name used by process scanners.
func ProgramName(argv0 string) string {
	name := filepath.Base(argv0)
	if runtime.GOOS == "windows" {
		return strings.TrimSuffix(strings.ToLower(name), ".exe")
	}
	return name
}

// AgentOfCommand identifies the agent from its command line.
func AgentOfCommand(argv []string) string {
	if IsClaudeCommand(argv) {
		return "claude"
	}
	if len(argv) == 0 {
		return ""
	}
	switch ProgramName(argv[0]) {
	case "codex":
		return "codex"
	case "traex", "traecli":
		return "traex"
	case "node", "bun":
		if len(argv) > 1 && (ProgramName(argv[1]) == "codex" || strings.Contains(filepath.ToSlash(argv[1]), "/codex/")) {
			return "codex"
		}
	}
	return ""
}
