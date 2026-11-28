package node

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/herdr"
	"github.com/sunfayao/persona/server/internal/host"
)

func TestSubscriptionsCoverEachPane(t *testing.T) {
	subs := subscriptions([]string{"w1:p1", "w2:p3"})
	var status []string
	for _, s := range subs {
		if s["type"] == "pane.agent_status_changed" {
			status = append(status, s["pane_id"])
		} else if s["pane_id"] != "" {
			t.Fatalf("lifecycle subscription with a pane: %v", s)
		}
	}
	if len(status) != 2 || status[0] != "w1:p1" || status[1] != "w2:p3" {
		t.Fatalf("status subscriptions %v", status)
	}
}

func TestPanesChangedResubscribes(t *testing.T) {
	n := &Node{agents: map[string]host.Agent{"s1": {PaneID: "w1:p1"}}, events: newHerdrEvents()}
	cancelled := 0
	n.events.panes, n.events.resub = "w1:p1", func() { cancelled++ }
	n.panesChanged()
	if cancelled != 0 {
		t.Fatal("same panes must keep the subscription")
	}
	n.agents["s2"] = host.Agent{PaneID: "w2:p1"}
	n.panesChanged()
	if cancelled != 1 || !n.events.resubbed || n.events.panes != "w1:p1 w2:p1" {
		t.Fatalf("cancelled %d resubbed %v panes %q", cancelled, n.events.resubbed, n.events.panes)
	}
}

func TestHostDue(t *testing.T) {
	herdrSig := newWatcherSignals()
	n := &Node{
		agents:  map[string]host.Agent{"s1": {Agent: "claude", PaneID: "w1:p1"}},
		hostSig: map[string]*watcherSignals{host.Herdr: herdrSig},
	}
	now := time.Now()
	// Without a live stream herdr is polled every tick.
	if !n.hostDue(host.Herdr, now, false) {
		t.Fatal("without an event stream herdr is polled every tick")
	}
	herdrSig.setLive(herdrSig.watcherID())
	if n.hostDue(host.Herdr, now, false) {
		t.Fatal("with events, a fresh snapshot is enough")
	}
	if !n.hostDue(host.Herdr, now.Add(-slowSnapshot), false) {
		t.Fatal("the slow snapshot still runs")
	}
	if !n.hostDue(host.Herdr, now, true) {
		t.Fatal("the kicked host is due")
	}
	// a compatible CLI state lives in the title spinner, which sends no event: its host stays due.
	n.agents["t1"] = host.Agent{Agent: traexAgent, PaneID: "w1:p9"}
	if !n.hostDue(host.Herdr, now, false) {
		t.Fatal("TRAE CLI in herdr keeps it polling")
	}
}

func TestHostWithoutWatcherAlwaysDue(t *testing.T) {
	n := &Node{} // botmux has no Watcher: hostDue is true on every tick
	if !n.hostDue(host.Botmux, time.Now(), false) {
		t.Fatal("a host without an event stream is polled every tick")
	}
}

// A lone event snapshots its host at once: the select that wakes the loop takes the host's only marker (#751: since
// #611 it was then not counted, and an agent turning blocked waited for the slow snapshot, up to 10 s).
func TestALoneEventSnapshotsItsHost(t *testing.T) {
	sig := newWatcherSignals()
	sigs := map[string]*watcherSignals{host.Herdr: sig, host.Botmux: newWatcherSignals()}
	sig.poke(false)
	start := time.Now()
	kicked, ok := nextChange(context.Background(), nil, sigs, time.Time{})
	if !ok || !kicked[host.Herdr] || kicked[host.Botmux] || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("kicked %v after %v", kicked, time.Since(start))
	}
}

func TestWaitsTellsACardsChange(t *testing.T) {
	var w waits
	for i, c := range []struct {
		pane    string
		blocked bool
		want    bool
	}{
		{"w1:p1", false, true}, // first heard of
		{"w1:p1", false, false},
		{"w1:p1", true, true},
		{"w1:p1", true, false},
		{"w1:p1", false, true},
		{"w1:p2", true, true},
	} {
		if got := w.changed(c.pane, c.blocked); got != c.want {
			t.Errorf("%d: %s blocked=%v: %v", i, c.pane, c.blocked, got)
		}
	}
}

// herdr's status events are urgent into or out of blocked, in the names herdr 0.9 sends (dotted, like the
// subscriptions: #751 found the node only knew the underscored ones) and older herdrs send.
func TestHerdrStatusEventsAreUrgent(t *testing.T) {
	dir, err := os.MkdirTemp("", "herdr")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")
	ln, err := herdr.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	status := func(event, pane, s string) string {
		return `{"event":"` + event + `","data":{"agent":"claude","agent_status":"` + s + `","pane_id":"` + pane + `","workspace_id":"w1"}}` + "\n"
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(`{"id":"persona:events","result":{"type":"subscription_started"}}` + "\n" +
			status("pane.agent_status_changed", "w1:p1", "working") +
			status("pane.agent_status_changed", "w1:p1", "blocked") +
			status("pane.agent_status_changed", "w1:p1", "working") +
			status("pane.agent_status_changed", "w1:p1", "idle") +
			status("pane_agent_status_changed", "w1:p2", "blocked") +
			`{"event":"pane.updated","data":{"pane_id":"w1:p1"}}` + "\n"))
	}()
	h := &herdrHost{c: &herdr.Client{Socket: sock}}
	var got []bool
	_ = h.Watch(context.Background(), []host.PaneRef{{ID: "w1:p1"}, {ID: "w1:p2"}}, func(urgent bool) { got = append(got, urgent) })
	want := []bool{false, true, true, true, false, true, false} // started, first status, into, out of, idle, older name, lifecycle
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("urgent %v, want %v", got, want)
	}
}

func TestHostFailBackoff(t *testing.T) {
	b := newHostFailBackoff()
	t0 := time.Date(2026, 11, 21, 16, 0, 0, 0, time.UTC)

	if b.skip(host.Herdr, t0, false) {
		t.Fatal("fresh host must not be skipped")
	}
	wantGaps := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	now := t0
	for i, want := range wantGaps {
		b.record(host.Herdr, true, now)
		if !b.skip(host.Herdr, now.Add(want-time.Millisecond), false) {
			t.Fatalf("step %d: expected skip before %s", i, want)
		}
		if b.skip(host.Botmux, now.Add(time.Millisecond), false) {
			t.Fatalf("step %d: failing herdr must not back off botmux", i)
		}
		if b.skip(host.Herdr, now.Add(want), false) {
			t.Fatalf("step %d: expected due at %s", i, want)
		}
		now = now.Add(want)
	}

	// A watcher kick bypasses and clears backoff immediately.
	b.record(host.Herdr, true, now)
	if !b.skip(host.Herdr, now.Add(time.Second), false) {
		t.Fatal("expected skip before kick")
	}
	if b.skip(host.Herdr, now.Add(time.Second), true) {
		t.Fatal("watcher kick must bypass backoff immediately")
	}
	if b.skip(host.Herdr, now.Add(2*time.Second), false) {
		t.Fatal("watcher kick must reset backoff")
	}

	// A successful refresh resets backoff so the next failure starts at pollFailBackoffFirst again.
	b.record(host.Herdr, true, now)
	b.record(host.Herdr, true, now.Add(2*time.Second))
	b.record(host.Herdr, false, now.Add(6*time.Second))
	b.record(host.Herdr, true, now.Add(7*time.Second))
	if b.skip(host.Herdr, now.Add(9*time.Second), false) {
		t.Fatal("backoff after reset must start at 2s")
	}
}

func TestRefreshHostsWithFailuresReportsOnlyFailingHosts(t *testing.T) {
	n, _, _ := herdrNode(t)
	failErr := fmt.Errorf("herdr socket unavailable")
	hFail := &inventoryHost{bareHost: bareHost{kind: host.Herdr}, snapshotErr: failErr, listErr: failErr}
	hNotRunning := &inventoryHost{bareHost: bareHost{kind: host.Botmux}, snapshotErr: host.ErrNotRunning, listErr: host.ErrNotRunning}
	hOK := &inventoryHost{bareHost: bareHost{kind: host.Tmux}}
	n.hosts = host.NewRegistry(hFail, hNotRunning, hOK)

	failed, err := n.refreshHostsWithFailures(context.Background())
	if err != nil {
		t.Fatalf("unexpected error when some hosts answered: %v", err)
	}
	if !failed[host.Herdr] {
		t.Fatalf("expected failing host to be marked failed: %v", failed)
	}
	if failed[host.Botmux] || failed[host.Tmux] {
		t.Fatalf("answered or ErrNotRunning hosts must not be marked failed: %v", failed)
	}
}
