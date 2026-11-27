package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A Codex thread's row names the permission mode its latest turn ran in (harness/codex PermissionMode), so the apps
// can say it runs with Full access: none before a turn, Full access for Codex's own, its own approval otherwise.
func TestCodexRowsNameTheModeTheirLatestTurnRanIn(t *testing.T) {
	n, _, cwd := codexHistoryNode(t)
	if s := byID(listSessions(t, n))[startThread]; s.PermissionMode != "" {
		t.Fatalf("before any turn: mode %q", s.PermissionMode)
	}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(cwd), ".codex", "sessions", "*", "*", "*", "rollout-*-"+startThread+".jsonl"))
	if len(paths) != 1 {
		t.Fatalf("rollouts: %v", paths)
	}
	turn := func(at time.Time, approval, sandbox string) {
		t.Helper()
		line, _ := json.Marshal(map[string]any{"timestamp": at.UTC().Format(time.RFC3339Nano), "type": "turn_context",
			"payload": map[string]any{"cwd": cwd, "model": "gpt-6", "approval_policy": approval, "sandbox_policy": map[string]any{"type": sandbox}}})
		f, err := os.OpenFile(paths[0], os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
		n.mu.Lock()
		delete(n.metas, paths[0]) // a growing rollout is read again only after metaMinInterval
		n.mu.Unlock()
	}
	now := time.Now()
	turn(now.Add(time.Second), "never", "danger-full-access")
	if s := byID(listSessions(t, n))[startThread]; s.PermissionMode != "bypassPermissions" {
		t.Fatalf("full access: mode %q", s.PermissionMode)
	}
	turn(now.Add(2*time.Second), "on-request", "workspace-write")
	if s := byID(listSessions(t, n))[startThread]; s.PermissionMode != "default" {
		t.Fatalf("its own approval: mode %q", s.PermissionMode)
	}
}

// A Codex in a terminal pane says it too, over the mode it was started with.
func TestCodexPaneRowNamesItsFullAccess(t *testing.T) {
	n, _, _ := effortPane(t)
	writeRollout(t, n.home, remoteRaw, n.home, time.Now(), time.Now(),
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"approval_policy":"never","sandbox_policy":{"type":"danger-full-access"}}}`)
	if s := paneRow(t, n, remoteRaw); s.PermissionMode != "bypassPermissions" {
		t.Fatalf("pane row mode %q", s.PermissionMode)
	}
}
