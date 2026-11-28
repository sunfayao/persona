package rulefiles

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Given is what a directory's node was last given: the CLAUDE.md and AGENTS.md ("" when the node writes the CLAUDE.md
// to both), the plugin skills (SkillDigests) and the denied track directories as JSON. The manager compares it with
// what it renders now to write only what changed. It lives in memory: a start of the manager writes every directory
// once (from what Book.Render says).
type Given struct {
	MD, AgentsMD, Skills, Deny string
	WorkGuide                  bool
	// SpaceMD is the digest of the SPACE.md a track's instructions ended with, as the node read it.
	SpaceMD string
}

// Given is what key (machine and directory, as prepareSpaceIf names it) last got; the zero Given before one was.
func (b *Book) Given(key string) Given {
	b.pmu.Lock()
	defer b.pmu.Unlock()
	return b.given[key]
}

// Remember notes that key's node got g.
func (b *Book) Remember(key string, g Given) {
	b.pmu.Lock()
	defer b.pmu.Unlock()
	b.given[key] = g
}

// Forget drops what key's node was given.
func (b *Book) Forget(key string) {
	b.pmu.Lock()
	defer b.pmu.Unlock()
	delete(b.given, key)
}

// ForgetAll drops what every node was given, as a start of the manager does.
func (b *Book) ForgetAll() {
	b.pmu.Lock()
	defer b.pmu.Unlock()
	clear(b.given)
}

// SkillDigests are the plugin skills of a plugins.assistant answer as they are compared, each one's plugin, name and
// digest; files says whether any comes with its files.
func SkillDigests(raw json.RawMessage) (digests string, files bool) {
	var skills []struct {
		Name   string                     `json:"name"`
		Plugin string                     `json:"plugin"`
		Digest string                     `json:"digest"`
		Files  map[string]json.RawMessage `json:"files"`
	}
	_ = json.Unmarshal(raw, &skills) // null from a manager without plugins: none
	var b strings.Builder
	for _, sk := range skills {
		fmt.Fprintf(&b, "%s/%s %s\n", sk.Plugin, sk.Name, sk.Digest)
		files = files || len(sk.Files) > 0
	}
	return b.String(), files
}

// AgyFileMax is how much of a rule file Antigravity reads: it cuts each one at 24,000 bytes, on a line boundary (the
// rules docs in agy: "Per-File Limit"), and the user's About me and the extensions' instructions come last.
const AgyFileMax = 24_000
