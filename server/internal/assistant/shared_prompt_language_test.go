package assistant

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/prompts"
)

// Delegate's explicit Chinese aliases predate the Inbox contract; other language names use the existing English
// fallback while the supplied rule stays in its original language.
func TestSharedRulesHonorDelegateLanguageAliases(t *testing.T) {
	const rule = "No envíes sin confirmación. 原文 #42"
	legacy := prompts.RenderAgentDelegate(prompts.AgentDelegate{Rules: rule})
	for _, l := range []string{"zh-Hans", "简体中文"} {
		if text := prompts.RenderAgentDelegate(prompts.AgentDelegate{Language: l, Rules: rule}); text != legacy {
			t.Fatalf("legacy alias %s changed the policy", l)
		}
	}
	for _, tc := range []struct{ language, heading string }{
		{"English", "The user's own rules (take precedence over the defaults above):"},
		{"日本語", "ユーザー自身のルール（上の既定の判断より優先）："},
		{"Français", "The user's own rules (take precedence over the defaults above):"},
	} {
		if text := prompts.RenderAgentDelegate(prompts.AgentDelegate{Language: tc.language, Rules: rule}); !strings.HasSuffix(text, tc.heading+"\n"+rule) {
			t.Fatalf("rules in %s: %q", tc.language, text)
		}
	}
}
