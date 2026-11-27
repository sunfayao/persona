package node

import (
	"testing"
	"time"
)

func TestAdoptSpareMCPPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, agent      string
		started, current bool
		want             bool
	}{
		{"legacy unchanged", "claude", false, false, true},
		{"canonical unchanged", "claude", true, true, true},
		{"hub upgraded", "claude", false, true, false},
		{"hub downgraded", "claude", true, false, false},
		{"codex legacy unchanged", "codex", false, false, true},
		{"codex canonical unchanged", "codex", true, true, true},
		{"codex hub upgraded", "codex", false, true, false},
		{"codex hub downgraded", "codex", true, false, false},
		{"runtime without a namespace policy", "dsh", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := assistantNode(t)
			n.hubPersonaContracts.Store(tc.current)
			p := &assistantProc{id: "spare", agent: tc.agent, launcher: tc.agent, cwd: n.home, born: time.Now(), personaMCPOnly: tc.started,
				fingerprint: n.executionFingerprint(tc.agent, modelExecution{})}
			n.spare = p
			got := n.adoptSpare(n.home, tc.agent)
			if (got == p) != tc.want {
				t.Fatalf("adopted=%v, want %v", got == p, tc.want)
			}
			if tc.want {
				if n.spare != nil || n.procs[p.id] != p {
					t.Fatal("matching spare did not become the conversation")
				}
			} else if n.spare != p || len(n.procs) != 0 {
				t.Fatal("mismatched spare became a conversation")
			}
		})
	}
}
