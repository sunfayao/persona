package node

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// instructionsFile is the file agent reads its instructions from in its directory at start: CLAUDE.md for Claude,
// AGENTS.md for Codex. assistant.prepare writes each in the words of the agent that reads it, so one can change while
// the other does not.
func instructionsFile(agent string) string {
	if agent == "claude" {
		return "CLAUDE.md"
	}
	return "AGENTS.md"
}

// instructionsDigest is a digest of the instructions agent reads in dir at start (instructionsFile), "" when there
// are none.
func instructionsDigest(dir, agent string) string {
	if dir == "" {
		return ""
	}
	name := instructionsFile(agent)
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00", name, len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// instructionsStale: the instructions in its directory changed since it started (the hub wrote a new CLAUDE.md, e.g.
// after an upgrade with new prompts, #3235): it has to resume in a fresh process to read them. Claude only: Codex's
// app-server is left as it is, and an Antigravity conversation runs on its runtime, which reads AGENTS.md again at
// every turn (assistant_runtime.go).
func (p *assistantProc) instructionsStale() bool {
	return p.agent == "claude" && p.cwd != "" && instructionsDigest(p.cwd, p.agent) != p.instructions
}
