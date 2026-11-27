package node

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/proto"
)

func activeRolloutEvent(at time.Time, typ string) string {
	return `{"timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `","type":"event_msg","payload":{"type":"` + typ + `"}}`
}

// Regression for #12389 (Part 1): a Persona-bound Codex pane whose host omits agent_session must stay bound and
// join the app-server as a writable session even when Codex updates the pane title before session_index indexes it.
func TestPersonaBoundCodexPaneStaysWritableUnderUnindexedTitle(t *testing.T) {
	t.Run("keeps_uncontradicted_bound_pane_writable", func(t *testing.T) {
		n, f, home := herdrNode(t)
		n.codices = newCodexRegistry()
		n.codices.scan = func() []codexProcess { return nil }
		n.configureCodex(proto.CodexConfig{})

		cwd := mkdir(t, filepath.Join(home, "repo"))
		codexHome := codex.CanonicalHome(filepath.Join(n.home, ".codex"), n.home)
		now := time.Now().UTC()
		writeRollout(t, n.home, remoteRaw, cwd, now, now, activeRolloutEvent(now, "task_started"))

		rf, _ := connectRemoteFixture(t, n, codexHome, "discovered-09b03a5e", remoteRaw)
		rf.mu.Lock()
		rf.active = true
		rf.mu.Unlock()

		n.bindPane("w1:p1", remoteRaw)
		n.state.put(remoteRaw, sessionState{Pane: "w1:p1", Cwd: cwd})
		// Host reports agent=="codex" without agent_session, with an unindexed "<title> | <repo>" pane title.
		f.reply("session.snapshot", snapshot(nil, nil, []any{
			agentOf("codex", "", proto.StatusWorking, "w1:p1", "w1:t1", "w1", cwd, "Unindexed summary | repo"),
		}, nil))

		listSessions(t, n)
		n.joinCodexPanes(context.Background())
		waitFor(t, "bound codex pane joins app-server", func() bool {
			rows := rowsOf(n, remoteRaw)
			return len(rows) == 1 && rows[0].SourceKind == "" && rows[0].CodexRoute == codexRouteAppServer && rows[0].Capabilities != nil && rows[0].Capabilities.Send
		})
		got := rowsOf(n, remoteRaw)
		if len(got) != 1 {
			t.Fatalf("rows %+v", got)
		}
		s := got[0]
		if s.SourceKind != "" || s.CodexRoute != codexRouteAppServer || s.PaneID != "w1:p1" || s.Host != "herdr" || s.ReadOnlyReason != "" {
			t.Fatalf("unexpected row: %+v", s)
		}
		if s.Capabilities == nil || !s.Capabilities.Send || !s.Capabilities.Interrupt {
			t.Fatalf("expected writable capabilities, got %+v", s.Capabilities)
		}
	})

	t.Run("moved_binding_does_not_claim_different_pane", func(t *testing.T) {
		n, f, home := herdrNode(t)
		n.codices = newCodexRegistry()
		n.codices.scan = func() []codexProcess { return nil }
		n.configureCodex(proto.CodexConfig{})

		cwd := mkdir(t, filepath.Join(home, "repo"))
		now := time.Now().UTC()
		writeRollout(t, n.home, remoteRaw, cwd, now, now, activeRolloutEvent(now, "task_started"))

		n.bindPane("w1:p1", remoteRaw)
		n.state.put(remoteRaw, sessionState{Pane: "w1:p9", Cwd: cwd})
		f.reply("session.snapshot", snapshot(nil, nil, []any{
			agentOf("codex", "", proto.StatusWorking, "w1:p1", "w1:t1", "w1", cwd, "Unindexed summary | repo"),
		}, nil))

		listSessions(t, n)
		if a, ok := n.agent(remoteRaw); ok {
			t.Fatalf("stale binding in moved pane should not map to %q: %+v", remoteRaw, a)
		}
	})
}

// Regression for #12389 (Part 2): when an external or unjoined Codex rollout has a freshly written open turn
// between 30s lsof scans, it must report StatusWorking (not StatusOffline/stoppedReason) while staying read-only.
func TestCodexHistoryActiveRolloutReportsWorkingBetweenScans(t *testing.T) {
	n, _, home := herdrNode(t)
	n.codices = newCodexRegistry()
	n.codices.scan = func() []codexProcess { return nil }
	n.configureCodex(proto.CodexConfig{})
	now := time.Now().UTC()
	cwd := mkdir(t, filepath.Join(home, "repo"))

	// Recent open turn (10s ago) without liveRollouts: stays read-only but must not say offline/stopped.
	writeRollout(t, n.home, remoteRaw, cwd, now, now.Add(-10*time.Second), activeRolloutEvent(now.Add(-10*time.Second), "task_started"))
	// Stale unfinished turn (2h ago): still treated as stopped/resumable history.
	staleAt := now.Add(-2 * time.Hour)
	writeRollout(t, n.home, remoteOther, cwd, staleAt, staleAt, activeRolloutEvent(staleAt, "task_started"))

	activeRows := rowsOf(n, remoteRaw)
	if len(activeRows) != 1 {
		t.Fatalf("active rows: %+v", activeRows)
	}
	active := activeRows[0]
	if active.SourceKind != "codex_history" || active.Status != proto.StatusWorking {
		t.Fatalf("active fallback row = %+v, want codex_history/working", active)
	}
	if active.Capabilities == nil || active.Capabilities.Send || active.Capabilities.Terminal {
		t.Fatalf("active external row capabilities = %+v, want read-only non-terminal", active.Capabilities)
	}
	if active.ReadOnlyReason == stoppedReason {
		t.Fatalf("active row must not use stopped ReadOnlyReason: %q", active.ReadOnlyReason)
	}

	staleRows := rowsOf(n, remoteOther)
	if len(staleRows) != 1 {
		t.Fatalf("stale rows: %+v", staleRows)
	}
	stale := staleRows[0]
	if stale.Status != proto.StatusOffline || stale.Capabilities == nil || !stale.Capabilities.Terminal || stale.ReadOnlyReason != stoppedReason {
		t.Fatalf("stale row = %+v, want offline resumable", stale)
	}
}
