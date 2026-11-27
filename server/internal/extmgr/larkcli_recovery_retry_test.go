package extmgr

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	cliconnector "github.com/sunfayao/persona/server/internal/connector/larkcli"
	"github.com/sunfayao/persona/server/internal/lark"
)

// Registry.Remove/Add is an asynchronous replacement; polling may observe the gap.
func currentRecoveryCLI(h *Manager) *cliconnector.Connector {
	c, _ := h.connectors.Get("larkcli").(*cliconnector.Connector)
	return c
}

func recoveryCLI(t *testing.T, w *botLoadWorld) string {
	t.Helper()
	bin, err := filepath.Abs(filepath.Join("..", "larkcli", "testdata", "fake-lark-cli.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("LARKCLI_FAKE_LOG", log)
	t.Setenv("LARKCLI_FAKE_SCENARIO", "success")
	w.h.cfg.Lark = lark.Config{CLI: &lark.CLIConfig{Bin: bin}}
	w.h.connectors = w.h.newConnectors(nil)
	return log
}

type dualLarkReadStore struct {
	Store
	failed         atomic.Bool
	botReads       atomic.Int32
	connectorReads atomic.Int32
}

func (s *dualLarkReadStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == botSettingsKey {
		s.botReads.Add(1)
	}
	if key == connectorSettingsKey {
		s.connectorReads.Add(1)
	}
	if s.failed.Load() && (key == botSettingsKey || key == connectorSettingsKey) {
		return false, errors.New("synthetic dual-key storage outage")
	}
	return s.Store.Setting(ctx, key, out)
}
