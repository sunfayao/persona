package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/connector"
	larkconnector "github.com/sunfayao/persona/server/internal/connector/larkcli"
	"github.com/sunfayao/persona/server/internal/lark"
)

type botBlockedReadStore struct {
	Store
	entered chan bool
	release chan struct{}
	once    atomic.Bool
	calls   atomic.Int32
}

func (s *botBlockedReadStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == botSettingsKey {
		s.calls.Add(1)
	}
	if key == botSettingsKey && s.once.CompareAndSwap(false, true) {
		_, bounded := ctx.Deadline()
		s.entered <- bounded
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-s.release:
			return false, errors.New("synthetic release")
		}
	}
	return s.Store.Setting(ctx, key, out)
}

func TestBotLoadReadKeepsEarlierCallerDeadline(t *testing.T) {
	w := newBotLoadWorld(t, true, true)
	st := &botBlockedReadStore{Store: w.store, entered: make(chan bool, 1), release: make(chan struct{})}
	w.h.store = st
	w.store.recover()
	w.advance()
	ctx, cancel := context.WithTimeout(w.ctx, 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := w.h.loadBotSettings(ctx); err == nil {
		t.Fatal("canceled read succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("earlier caller deadline lost: %v", elapsed)
	}
	w.h.botAPI()
	if st.calls.Load() != 1 {
		t.Fatal("cancellation bypassed cooldown")
	}
	w.advance()
	if api, _ := w.h.botAPI(); api == nil {
		t.Fatal("canceled read did not recover on retry")
	}
}

func TestBotLoadLiveCLIProfileRecovery(t *testing.T) {
	w := newBotLoadWorld(t, true, false)
	bin, err := filepath.Abs(filepath.Join("..", "larkcli", "testdata", "fake-lark-cli.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("LARKCLI_FAKE_SCENARIO", "success")
	t.Setenv("LARKCLI_FAKE_LOG", log)
	w.h.cfg.Lark = lark.Config{CLI: &lark.CLIConfig{Bin: bin}}
	if err := w.store.botStore.PutSetting(w.ctx, botSettingsKey, FeishuBotSettings{Enabled: false, LarkCLIProfile: "other"}); err != nil {
		t.Fatal(err)
	}
	w.h.connectors = w.h.newConnectors(nil)
	w.h.startConnectors(w.ctx)
	c := w.h.connectors.Get("larkcli").(*larkconnector.Connector)
	if _, err := c.Call(w.ctx, "larkcli_chats", json.RawMessage(`{}`)); err == nil {
		t.Error("live connector executed CLI while saved identity was unread")
	}
	if raw, _ := os.ReadFile(log); len(raw) > 0 {
		t.Errorf("unknown identity invoked CLI: %s", raw)
	}
	if _, err := w.h.assistantLark(w.ctx); !errors.Is(err, errBotSettingsUnread) {
		t.Errorf("assistant exposed an unknown CLI profile: %v", err)
	}
	w.store.recover()
	w.advance()
	w.h.botAPI()
	eventually(t, time.Second, "live connector profile replacement", func() bool { c := currentRecoveryCLI(w.h); return c != nil && c.Profile() == "other" })
	if cfg, err := w.h.assistantLark(w.ctx); err != nil || cfg.CLI == nil || cfg.CLI.Profile != "other" {
		t.Fatalf("assistant profile did not recover: %+v %v", cfg, err)
	}
	if _, err := c.Call(w.ctx, "larkcli_chats", json.RawMessage(`{}`)); err == nil {
		t.Error("retained placeholder ran with the recovered account and old Inbox cache")
	}
	for _, tool := range []struct{ name, args string }{{"larkcli_chats", `{}`}, {"larkcli_messages", `{"chat_id":"oc_example"}`}, {"larkcli_search", `{"query":"example","kind":"messages"}`}} {
		current := w.h.connectors.Get("larkcli").(connector.ReadTools)
		if _, err := current.Call(w.ctx, tool.name, json.RawMessage(tool.args)); err != nil {
			t.Errorf("%s: %v", tool.name, err)
		}
	}
	raw, _ := os.ReadFile(log)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.HasPrefix(line, "--profile other ") {
			t.Errorf("recovered CLI invocation used the wrong profile: %q", line)
		}
	}
	eventually(t, time.Second, "live connector cache to refresh to the saved profile", func() bool {
		c := currentRecoveryCLI(w.h)
		return c != nil && c.Profile() == "other"
	})
}

func TestBotLoadManagedCLIRecoverySynchronizes(t *testing.T) {
	w := newBotLoadWorld(t, true, false)
	bin, err := filepath.Abs(filepath.Join("..", "larkcli", "testdata", "fake-lark-cli.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "init.log")
	t.Setenv("LARKCLI_FAKE_SCENARIO", "config_different")
	t.Setenv("LARKCLI_FAKE_INIT_LOG", log)
	w.h.cfg.Lark = lark.Config{CLI: &lark.CLIConfig{Bin: bin}}
	sealed, err := w.h.sealKey("synthetic-recovery-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.store.botStore.PutSetting(w.ctx, botSettingsKey, FeishuBotSettings{Enabled: false, AppID: "cli_recovered", SecretEnc: sealed}); err != nil {
		t.Fatal(err)
	}
	w.h.connectors = w.h.newConnectors(nil)
	w.h.startConnectors(w.ctx)
	if _, err := os.Stat(log); err == nil {
		t.Fatal("unknown identity initialized the managed CLI")
	}
	w.store.recover()
	w.advance()
	w.h.botAPI()
	eventually(t, 2*time.Second, "managed CLI synchronization and replacement", func() bool {
		c := currentRecoveryCLI(w.h)
		return c != nil && c.Profile() == managedLarkCLIProfile
	})
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "--app-id cli_recovered") || !strings.Contains(string(raw), "stdin=synthetic-recovery-secret") || strings.Contains(strings.SplitN(string(raw), "|", 2)[0], "synthetic-recovery-secret") {
		t.Fatalf("managed profile not synchronized safely: %s", raw)
	}
	if api, _ := w.h.botAPI(); api != nil {
		t.Fatal("CLI recovery enabled the disabled bot")
	}
}
