package node

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/orcart"
	"github.com/sunfayao/persona/server/internal/orcart/orcarttest"
)

// A cached target needs the same final check. Neither a hook binding nor an agent-inventory observation may be
// closed; the latter has no n.panes entry. Calling the closer directly isolates its check from tab's earlier check.
func TestOrcaKeptCloseRechecksCachedTerminal(t *testing.T) {
	for _, inventory := range []bool{false, true} {
		t.Run(fmt.Sprintf("inventory=%v", inventory), func(t *testing.T) {
			n, h, o, created, _ := keptOrcaStart(t, true)
			claim, err := n.claimKeptTab(created.Pane.String())
			if err != nil || claim == nil {
				t.Fatalf("claim: %v", err)
			}
			defer claim.release()
			original := cachedKeptTerminal(t, h, created.Pane.ID)
			if inventory {
				n.mu.Lock()
				n.agents[sidA] = host.Agent{Agent: "claude", PaneID: created.Pane.String()}
				n.mu.Unlock()
			} else {
				n.bindPane(created.Pane.String(), sidA)
			}
			listed := o.Count("terminal.list")
			if err := claim.closeStalledTab(context.Background()); err == nil {
				t.Error("closing an occupied cached terminal was not refused")
			}
			if got := o.Trace("terminal.close"); len(got) != 0 {
				t.Errorf("closed an occupied cached terminal: %v", got)
			}
			if got := o.Count("terminal.list"); got != listed {
				t.Errorf("cached close unexpectedly listed terminals: %d, was %d", got, listed)
			}
			if !keptTerminalTracked(t, h, o, original) {
				t.Error("a refused close removed creation tracking")
			}
		})
	}
}

// A transport failure after resolving the cached terminal is a real close failure, not a recovery refusal. The
// failed start must release its original claim and expiry, open no second tab, and let a later retry close it.
func TestOrcaKeptCloseFailureReleasesClaim(t *testing.T) {
	n, h, o, created, _ := keptOrcaStart(t, true)
	until, _ := isKept(n, created.Pane.String())
	original := cachedKeptTerminal(t, h, created.Pane.ID)
	claim, err := n.claimKeptTab(created.Pane.String())
	if err != nil || claim == nil {
		t.Fatalf("claim: %v", err)
	}
	defer claim.release()
	o.Stop()
	if got, err := claim.tab(context.Background()); got != nil || err == nil || errors.Is(err, errKeptTabUnavailable) {
		t.Fatalf("failed close: tab %v, err %v", got, err)
	}
	if after, kept := isKept(n, created.Pane.String()); !kept || !after.Equal(until) {
		t.Fatalf("failed close did not release the original expiry: kept %v, until %v, was %v", kept, after, until)
	}
	if got := o.Count("terminal.create"); got != 1 {
		t.Fatalf("failed close created another tab: %d", got)
	}
	if err := o.Start(); err != nil {
		t.Fatal(err)
	}
	if !keptTerminalTracked(t, h, o, original) {
		t.Error("failed close removed creation tracking")
	}
	next, err := n.claimKeptTab(created.Pane.String())
	if err != nil || next == nil {
		t.Fatalf("claim after failed close: %v", err)
	}
	defer next.release()
	if got, err := next.tab(context.Background()); got != nil || err != nil {
		t.Fatalf("later close: tab %v, err %v", got, err)
	}
	n.mu.Lock()
	_, kept := n.keptStarts[created.Pane.String()]
	n.mu.Unlock()
	tracked := keptTerminalTracked(t, h, o, original)
	if kept || tracked || o.Count("terminal.close") != 1 {
		t.Errorf("successful later close did not drop the tab: kept %v, tracked %v, closes %v", kept, tracked, o.Trace("terminal.close"))
	}
}

func cachedKeptTerminal(t *testing.T, h *orcaHost, pane string) orcart.Terminal {
	t.Helper()
	if _, err := h.Snapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	list, err := h.Runtime().Terminals(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, terminal := range list.Terminals {
		if terminal.PaneKey() == pane {
			return terminal
		}
	}
	t.Fatalf("no terminal for %s", pane)
	return orcart.Terminal{}
}

func keptTerminalMap(terminal orcart.Terminal, tab string) map[string]any {
	m := orcarttest.Terminal(terminal.Handle, tab, terminal.LeafID, terminal.WorktreeID,
		terminal.WorktreePath, terminal.Branch, terminal.Title, terminal.AgentIdentity)
	m["incarnationId"] = terminal.IncarnationID
	return m
}

// When Orca orphans a terminal, only a host that still tracks its creation can
// find it under its original key.
func keptTerminalTracked(t *testing.T, h *orcaHost, o *orcarttest.Fake, terminal orcart.Terminal) bool {
	t.Helper()
	orphan := keptTerminalMap(terminal, "orphan")
	orphan["orphaned"] = true
	o.SetTerminals(orphan)
	if _, err := h.Snapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	return h.RekeyedKey(terminal.PaneKey())
}
