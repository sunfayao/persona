package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const testLauncherKey = "sk-fake-0123456789abcdefghijKLMNOPQRST"

func readKeyResponses(t *testing.T, path string) (cfg map[string]any, approved, rejected []string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	r, _ := cfg["customApiKeyResponses"].(map[string]any)
	for _, v := range r["approved"].([]any) {
		approved = append(approved, v.(string))
	}
	for _, v := range r["rejected"].([]any) {
		rejected = append(rejected, v.(string))
	}
	return cfg, approved, rejected
}

// A managed launcher's key goes into Claude Code's approved keys (out of rejected, where answering the default No put
// it), the rest of the config stays as it was, and the file keeps its mode.
func TestLaunchApprovesTheLaunchersKeyForClaude(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	suffix := claudeKeySuffix(testLauncherKey)
	if len(suffix) != 20 || suffix != testLauncherKey[len(testLauncherKey)-20:] {
		t.Fatalf("Claude names a key by its last 20 characters: %q", suffix)
	}
	start := `{"hasCompletedOnboarding": true, "projects": {"/w": {"allowedTools": ["Bash"]}},
  "customApiKeyResponses": {"approved": ["other-key-suffix-0001"], "rejected": ["` + suffix + `", "old-rejected-suffix-1"]}}`
	if err := os.WriteFile(path, []byte(start), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "ANTHROPIC_BASE_URL=http://gw.example.com", "ANTHROPIC_API_KEY=" + testLauncherKey + "\n"}
	if err := approveClaudeAPIKey(env); err != nil {
		t.Fatal(err)
	}
	cfg, approved, rejected := readKeyResponses(t, path)
	if !slices.Equal(approved, []string{"other-key-suffix-0001", suffix}) {
		t.Errorf("approved: %v", approved)
	}
	if !slices.Equal(rejected, []string{"old-rejected-suffix-1"}) {
		t.Errorf("rejected: %v", rejected)
	}
	if cfg["hasCompletedOnboarding"] != true || cfg["projects"].(map[string]any)["/w"] == nil {
		t.Errorf("the rest of the config changed: %v", cfg)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}

	// Approved already: the file is not written again.
	before, _ := os.ReadFile(path)
	if err := approveClaudeAPIKey(env); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("an approved key rewrote the config")
	}
	if left, _ := filepath.Glob(filepath.Join(home, ".claude.json.persona-*")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

// The config is found where Claude Code looks: CLAUDE_CONFIG_DIR, the legacy .config.json, a symlink's target. With no
// key, or no config yet (Claude's own first-run onboarding), nothing is written.
func TestLaunchFindsClaudesConfigAndLeavesItAloneWithoutAKey(t *testing.T) {
	home, profile := t.TempDir(), t.TempDir()
	suffix := claudeKeySuffix(testLauncherKey)

	if err := approveClaudeAPIKey([]string{"HOME=" + home, "ANTHROPIC_API_KEY=" + testLauncherKey}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatal("a config was created where there was none")
	}

	inProfile := filepath.Join(profile, ".claude.json")
	_ = os.WriteFile(inProfile, []byte(`{}`), 0o644)
	_ = os.Chmod(inProfile, 0o644) // past the umask
	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{}`), 0o600)
	if err := approveClaudeAPIKey([]string{"HOME=" + home, "CLAUDE_CONFIG_DIR=" + profile, "ANTHROPIC_API_KEY=" + testLauncherKey}); err != nil {
		t.Fatal(err)
	}
	if _, approved, _ := readKeyResponses(t, inProfile); !slices.Equal(approved, []string{suffix}) {
		t.Errorf("CLAUDE_CONFIG_DIR's config: %v", approved)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".claude.json")); string(b) != `{}` {
		t.Errorf("the home config changed with CLAUDE_CONFIG_DIR set: %s", b)
	}
	if st, _ := os.Stat(inProfile); st.Mode().Perm() != 0o644 {
		t.Errorf("mode %v, want 0644 kept", st.Mode().Perm())
	}

	legacyHome := t.TempDir()
	_ = os.MkdirAll(filepath.Join(legacyHome, ".claude"), 0o700)
	legacy := filepath.Join(legacyHome, ".claude", ".config.json")
	_ = os.WriteFile(legacy, []byte(`{"theme":"dark"}`), 0o600)
	_ = os.WriteFile(filepath.Join(legacyHome, ".claude.json"), []byte(`{}`), 0o600)
	if got := claudeGlobalConfig([]string{"HOME=" + legacyHome}); got != legacy {
		t.Errorf("a non-empty legacy .config.json is the one Claude reads: %s", got)
	}
	if got := claudeGlobalConfig([]string{"HOME=" + legacyHome, "CLAUDE_CODE_CUSTOM_OAUTH_URL=https://x.example.com"}); got != legacy {
		t.Errorf("legacy wins over the custom-oauth name too: %s", got)
	}
	_ = os.WriteFile(legacy, nil, 0o600)
	if got := claudeGlobalConfig([]string{"HOME=" + legacyHome}); got != filepath.Join(legacyHome, ".claude.json") {
		t.Errorf("an empty legacy file with .claude.json present is not used: %s", got)
	}

	linkHome, dotfiles := t.TempDir(), t.TempDir()
	target := filepath.Join(dotfiles, "claude.json")
	_ = os.WriteFile(target, []byte(`{}`), 0o600)
	if err := os.Symlink(target, filepath.Join(linkHome, ".claude.json")); err != nil {
		t.Fatal(err)
	}
	if err := approveClaudeAPIKey([]string{"HOME=" + linkHome, "ANTHROPIC_API_KEY=" + testLauncherKey}); err != nil {
		t.Fatal(err)
	}
	if _, approved, _ := readKeyResponses(t, target); !slices.Equal(approved, []string{suffix}) {
		t.Errorf("the symlink's target: %v", approved)
	}
	if st, _ := os.Lstat(filepath.Join(linkHome, ".claude.json")); st.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a file")
	}

	before, _ := os.ReadFile(target)
	if err := approveClaudeAPIKey([]string{"HOME=" + linkHome, "ANTHROPIC_AUTH_TOKEN=tok"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(target); string(after) != string(before) {
		t.Error("written without ANTHROPIC_API_KEY")
	}
}
