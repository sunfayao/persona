package extmgr

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extension/host"
	"github.com/sunfayao/persona/server/internal/i18n"
)

// notesProgram is a generic extension running on its own on a unix socket, as the id and version it is started with.
type notesProgram struct {
	ln    net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func (n *notesProgram) stop() {
	n.ln.Close()
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, c := range n.conns {
		c.Close()
	}
	n.conns = nil
}

func shortHostTimings(t *testing.T) {
	first, retry, grace := host.RestartFirst, host.RemoteRetry, host.OfflineGrace
	host.RestartFirst, host.RemoteRetry, host.OfflineGrace = 50*time.Millisecond, 50*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { host.RestartFirst, host.RemoteRetry, host.OfflineGrace = first, retry, grace })
}

// An attached extension never upgrades itself from a market, even with a market origin: the manager has no program of
// it to replace.
func TestAttachedNeverUpgradesItself(t *testing.T) {
	p := &extension.Extension{Record: extension.Record{ID: "example-notes", Source: extension.SourceInstalled, Origin: "market:example/example-notes", Version: "1.0.0"}}
	if got := upgradesFrom(p); got != "example" {
		t.Fatalf("from its market: %q", got)
	}
	p.Record.Remote = &extension.Remote{RPCSocket: "/tmp/notes.sock"}
	if got := upgradesFrom(p); got != "" {
		t.Fatalf("attached: %q", got)
	}
	if _, _, _, ok := (&extensionUpgrades{}).candidateIn(p, "en"); ok {
		t.Fatal("an attached extension has an upgrade")
	}
	var v ExtensionView
	(&extensionUpgrades{}).decorate(t.Context(), &v, p)
	if v.AutoUpgrade != nil || v.Upgrade != nil {
		t.Fatalf("decorated: %+v", v)
	}
}

func TestAttachedLine(t *testing.T) {
	since := time.Date(2025, 1, 2, 10, 42, 0, 0, time.FixedZone("", 9*3600)) // printed in its own zone, not this machine's
	for _, tt := range []struct {
		lang      i18n.Lang
		a         *ExtensionAttached
		withError bool
		want      string
	}{
		{i18n.ZhHans, nil, true, ""},
		{i18n.ZhHans, &ExtensionAttached{Online: true, Since: since}, true, "；独立运行，在线（自 01-02 10:42:00）"},
		{i18n.ZhHans, &ExtensionAttached{Since: since, Error: "not answering"}, true, "；独立运行，离线（自 01-02 10:42:00）：not answering"},
		{i18n.ZhHans, &ExtensionAttached{Since: since, Error: "not answering"}, false, "；独立运行，离线（自 01-02 10:42:00）"},
		{i18n.ZhHans, &ExtensionAttached{Version: "1.1.0"}, false, "；独立运行，离线；程序自报版本 1.1.0"},
		{i18n.En, &ExtensionAttached{Online: true, Since: since}, true, "; runs on its own, online (since 01-02 10:42:00)"},
		{i18n.En, &ExtensionAttached{Since: since, Error: "not answering"}, true, "; runs on its own, offline (since 01-02 10:42:00): not answering"},
		{i18n.En, &ExtensionAttached{Version: "1.1.0"}, false, "; runs on its own, offline; the program reports version 1.1.0"},
		{i18n.Ja, &ExtensionAttached{Online: true, Since: since}, true, "；単独で実行、オンライン（01-02 10:42:00から）"},
	} {
		if got := attachedLine(tt.lang, tt.a, tt.withError); got != tt.want {
			t.Errorf("attachedLine(%s, %+v, %v) = %q, want %q", tt.lang, tt.a, tt.withError, got, tt.want)
		}
	}
}
