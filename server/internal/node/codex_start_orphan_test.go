package node

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

func TestFailedCodexStartDoesNotArchiveAfterCloseFailure(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	path := writeArchiveRollout(t, home, startThread)
	id := n.registerCodex(home, startThread, path)
	bus, _ := connectRemoteFixture(t, n, home, "app", startThread)
	launch := &abandonedCodexLauncher{close: func(context.Context, host.PaneRef) error { return errors.New("close refused") }}
	start := abandonedCodexPane(launch, "w1:p2")
	start.session = id
	if err := n.closeFailedCodexStart(context.Background(), start); err == nil {
		t.Fatal("failed close reported success")
	}
	if bus.count("thread/archive") != 0 || archiveIn(t, home, startThread) {
		t.Fatal("archived a thread whose pane is still open")
	}
}

func TestFailedCodexIdentityRejectsAnOldOrOtherPanesThread(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "another pane", true: "old thread"}[old], func(t *testing.T) {
			n, f := stalledCodexNode(t)
			began := time.Now()
			at := began
			if old {
				at = began.Add(-time.Hour)
			}
			cwd := filepath.Join(filepath.Dir(n.dir), "proj")
			writeWaitRollout(t, filepath.Dir(n.dir), cwd, startThread, at, "方向 · 合成测试 · 说明")
			f.reply("agent.get", map[string]any{"agent": agentOf("codex", startThread, proto.StatusBlocked, "w1:p2", "w1:t2", "w1", cwd, "")})
			if !old {
				n.bindPane("w1:p9", startThread)
			}
			if got := n.failedCodexSession(context.Background(), "w1:p2", began); got != "" {
				t.Fatalf("unsafe orphan identity: %s", got)
			}
		})
	}
}
