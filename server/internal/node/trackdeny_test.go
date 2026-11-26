package node

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/identity"
)

// The tracks a manager names (assistant.prepare deny_dirs, #13104) are denied to the assistant's own conversations,
// with their Claude project directories, for reading and writing, except to a conversation in that track; the rules go
// on the claude command line (#3282). A conversation in a track is told as that track's; a name outside the state
// directory or with a bad slug is dropped; a later list from the same directory replaces the earlier one.
func TestDenyDirs(t *testing.T) {
	n, home := testNode(t)
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { identity.SetTrackDirs(".persona/assistant", nil) })
	assistant := filepath.Join(home, ".persona", "assistant")
	finance := filepath.Join(home, ".persona", "tracks", "qa-finance")
	n.setDenyDirs(".persona/assistant", []denyDir{{".persona/tracks/qa-finance", "qa-finance"}, {"../elsewhere", "qa-x"}, {"/etc", "qa-y"},
		{".persona", "qa-z"}, {".persona/tracks/BAD", "BAD"}})
	if got := namedDenyDirs(); !slices.Equal(got, []string{finance}) {
		t.Fatalf("kept %q, want only %s", got, finance)
	}
	if got := identity.SpaceAt(filepath.Join(finance, "notes")); got != "qa-finance" {
		t.Fatalf("a folder in the track is told as %q", got)
	}
	rules := n.trackDenyRules(assistant)
	project := filepath.Join(home, ".claude", "projects", nonAlnum.ReplaceAllString(finance, "-"))
	for _, want := range []string{"Read(/" + finance + "/**)", "Edit(/" + finance + "/**)", "Read(/" + project + "/**)", "Edit(/" + project + "/**)"} {
		if !slices.Contains(rules, want) {
			t.Fatalf("missing %s in %q", want, rules)
		}
	}
	for _, r := range rules {
		if strings.Contains(r, "/.persona/assistant") || strings.Contains(r, "-assistant/") {
			t.Fatalf("the assistant's own directory denied: %s", r)
		}
	}
	if rules := n.trackDenyRules(finance); slices.ContainsFunc(rules, func(r string) bool { return strings.Contains(r, "qa-finance") }) {
		t.Fatalf("a conversation of the track is denied its own: %q", rules)
	}

	flags := n.claudeAssistantMCPAndTools("sess-123", assistant, true)
	i := slices.Index(flags, "--disallowedTools")
	if i < 0 || !slices.Contains(flags[i+1:], "Read(/"+finance+"/**)") || slices.Contains(flags[i+1:], "--disallowedTools") {
		t.Fatalf("flags %q", flags)
	}
	if flags := n.claudeAssistantMCPAndTools("sess-123", finance, true); slices.ContainsFunc(flags, func(f string) bool { return strings.HasPrefix(f, "Read(") }) {
		t.Fatalf("a track conversation's flags deny its own files: %q", flags)
	}

	n.setDenyDirs(".persona/assistant", []denyDir{})
	if rules := n.trackDenyRules(assistant); slices.ContainsFunc(rules, func(r string) bool { return strings.Contains(r, "qa-finance") }) {
		t.Fatalf("an emptied list still denies: %q", rules)
	}
	// What is named is forgotten; the default layout (tracks/<slug>) is still told by its shape (identity.SpaceAt).
	if got := identity.TrackDirs(); len(got) != 0 {
		t.Fatalf("an emptied list still names %v", got)
	}
}
