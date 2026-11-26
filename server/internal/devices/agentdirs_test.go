package devices

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// failingRulesStore fails reads of the Agent folder rules while fail is set, like a database away at connect time.
type failingRulesStore struct {
	dirsStore
	fail *bool
}

func (s failingRulesStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if *s.fail && strings.HasPrefix(key, "agent_dirs_hidden/") {
		return false, errors.New("database away")
	}
	return s.dirsStore.Setting(ctx, key, out)
}

// A node keeps the block list it was given, also across restarts. A manager that cannot read the rules when it
// configures the node (a cold cache and a failed read) says nothing of them (null): sending [] would unblock every
// folder on that computer. Only a successful read clears them, an empty rule list included.
func TestBlockedAgentDirsColdReadFailureKeepsTheNodesBlocks(t *testing.T) {
	_, st, m, _ := dirsHub(t)
	fail := true
	m.d.opts.Store = failingRulesStore{dirsStore{st}, &fail}
	f := connectDirsNode(t, m)
	ctx := context.Background()
	sent := func() string {
		f.mu.Lock()
		defer f.mu.Unlock()
		raw, ok := f.configure[len(f.configure)-1]["agent_dirs_blocked"]
		if !ok {
			return "(absent)"
		}
		return string(raw)
	}
	m.configureNode(ctx, m.peer, nil, true)
	if got := sent(); got != "null" {
		t.Fatalf("a failed rules read sent agent_dirs_blocked %s; the node would replace its blocks with it", got)
	}
	fail = false
	m.hiddenDirs.mu.Lock()
	m.hiddenDirs.failedAt = time.Time{} // the retry is due
	m.hiddenDirs.mu.Unlock()
	m.configureNode(ctx, m.peer, nil, true)
	if got := sent(); got != "[]" {
		t.Fatalf("a successful read of no rules sent agent_dirs_blocked %s, want [] (clears the node's blocks)", got)
	}
}
