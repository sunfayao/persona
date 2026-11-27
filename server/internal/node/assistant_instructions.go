package node

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// splitRules is AGENTS.md's text for an agent that reads a rule file only up to max bytes, cutting the rest (Antigravity,
// 24,000): whole when it fits or max is 0; else cut before the last section ("## ") that would not fit whole, or after
// the last line that fits when the first section alone does not, with the rest for GEMINI.md, which that agent reads
// as rules beside it. The cut is at a line's end, so both are whole UTF-8 text.
func splitRules(md string, max int) (agents, rest string) {
	if max <= 0 || len(md) <= max {
		return md, ""
	}
	head := md[:max]
	cut := strings.LastIndex(head, "\n## ")
	if cut <= 0 {
		cut = strings.LastIndex(head, "\n")
	}
	if cut <= 0 {
		return md, "" // a first line longer than max: nothing to cut on
	}
	return md[:cut+1], md[cut+1:]
}

// writeOrRemove writes md to path when it changed, or removes the file when md is empty (the instructions fit their
// own file again).
func writeOrRemove(path, md string) error {
	if md == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeIfChanged(path, []byte(md))
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
