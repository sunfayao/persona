package assistant

import (
	"context"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// kindFixture is the computer of #4659's report: Claude Code installed (a version reported), a Claude wrapper from
// the shell ("wrap"; the node then lists no plain claude) and a Codex alias, no managed launchers. The main chat runs
// on wrap with a model.
func kindFixture(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "server", Name: "box", Online: true, Reachable: true, ClaudeVersion: "2.1.322"}}
	f.launcherList = []proto.Launcher{{Name: "wrap"}, {Name: "cx", Agent: "codex"}}
	f.agentModels = map[string][]proto.AgentModel{"claude": {{ID: "claude-opus-5-5"}}}
	s := newTestService(t, f)
	// The last successful shell discovery establishes the missing wrapper's kind. Tests for a cold cache clear this.
	s.models.zshrc = map[string][]proto.Launcher{"server": {{Name: "wrap", Source: "zshrc"}}}
	s.models.zshrcSync = map[string]LauncherSync{}
	setModel(t, s, "assistant", ModelBinding{Provider: "launcher.wrap", Model: "claude-opus-5-5"})
	setSettings(t, s, func(v *Settings) { v.Main = "main" })
	f.setSessions("server", proto.Session{SessionID: "main", Assistant: true})
	return s, f
}

// A new Agent's default launcher that is hidden still starts (#4659): hiding takes it out of the pickers only.
func TestAgentStartKeepsAHiddenDefault(t *testing.T) {
	ctx := context.Background()
	s, _ := launcherFitHub(t)
	if err := s.putSetting(ctx, launchersHiddenKey, []string{"gateway"}); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"launcher": "gateway"}
	note, _, err := s.fitLauncher(ctx, Device{ID: "box", Name: "box", ClaudeVersion: "2.1.0"}, startPick{}, params)
	if err != nil || note != "" || params["launcher"] != "gateway" {
		t.Fatalf("hidden default: %v %q %v", params, note, err)
	}
}
