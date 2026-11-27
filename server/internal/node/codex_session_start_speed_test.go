package node

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// codexSpeedStart runs the real session.start path with a managed Codex whose
// app-server catalog contains tiersJSON for the selected model. The captured
// personad launch argv and fake herdr calls are the acceptance evidence.
func codexSpeedStart(t *testing.T, tiersJSON, speed string) (*Node, *fakeHerdr, []string, error) {
	t.Helper()
	withCodexLaunchers(t, map[string][]string{})
	n, f, _, _ := codexStartHerdr(t, 0)
	t.Setenv("HOME", n.home)
	t.Setenv("ZDOTDIR", n.home)
	writeFile(t, filepath.Join(n.home, ".zshrc"), "")
	n.bin = filepath.Join(n.home, "bin", "personad")
	models := `{"jsonrpc":"2.0","id":2,"result":{"data":[{"model":"example-big","displayName":"Example Big","hidden":false,"defaultReasoningEffort":"high","supportedReasoningEfforts":[{"reasoningEffort":"high"}],"serviceTiers":` + tiersJSON + `}],"nextCursor":null}}`
	writeFile(t, n.bin, `#!/bin/sh
case " $* " in *" app-server "*) ;; *) echo "unexpected personad invocation: $*" >&2; exit 1 ;; esac
read line
echo '{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex"}}'
read line
read line
echo '`+models+`'
sleep 60
`)
	if err := os.Chmod(n.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	n.setManaged([]proto.ManagedLauncher{{Name: "gw", Agent: "codex", BaseURL: "https://gateway.example.com", APIKey: "k", Model: "example-big"}})
	_, err := request(t, n, "session.start", map[string]any{
		"cwd": "~/proj", "launcher": "gw", "prompt": "hello", "speed": speed,
	})
	return n, f, slices.Clone(ran), err
}

func TestCodexSessionStartSpeedRequiresSelectedModelTier(t *testing.T) {
	for _, tc := range []struct {
		name, speed, tiers string
	}{
		{name: "fast missing from selected model", speed: "fast", tiers: `[{"id":"ultrafast","name":"Ultrafast"}]`},
		{name: "ultrafast missing from selected model", speed: "ultrafast", tiers: `[{"id":"priority","name":"Fast"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, f, _, err := codexSpeedStart(t, tc.tiers, tc.speed)
			if code := rpcCode(err); code != "agent_speed_model" {
				t.Errorf("refusal = %q (%v), want agent_speed_model", code, err)
			}
			if calls := f.trace("tab.create", "pane.send_input"); len(calls) != 0 {
				t.Errorf("an unadvertised tier reached startup: %v", calls)
			}
		})
	}
}
