package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// toolsExt is a set of extensions whose assistant-only tools a test changes, as turning ego on for a Mac does.
type toolsExt struct {
	guestExt
	mu   sync.Mutex
	only []proto.Tool
}

func (e *toolsExt) AgentTools(context.Context, bool) (agents, assistantOnly []proto.Tool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return nil, slices.Clone(e.only)
}

func (e *toolsExt) set(names ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.only = nil
	for _, n := range names {
		e.only = append(e.only, proto.Tool{Name: n, Description: "Tool " + n + ".", InputSchema: json.RawMessage(`{"type":"object"}`)})
	}
}

func toolNamesOf(tools []proto.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// A change in the extensions' tools moves the tool-list revision by itself: a watch that asks with the old revision
// gets the new list at once, with nothing having told it (the heartbeat's safety net).
func TestExtensionToolChangesMoveTheToolRevision(t *testing.T) {
	s := newTestService(t, newGuardFakeHub())
	ext := &toolsExt{}
	ext.set("computer_state", "computer_run")
	s.SetExtensions(ext)
	before, revision := discoveryTools(t, s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, ""))
	if slices.Contains(toolNamesOf(before), "browser_run") || !slices.Contains(toolNamesOf(before), "computer_run") {
		t.Fatalf("initial list: %v", toolNamesOf(before))
	}
	ext.set("computer_state", "computer_run", "browser_look", "browser_run")
	after, next := discoveryTools(t, s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, revision))
	if next == revision || !slices.Contains(toolNamesOf(after), "browser_run") || !slices.Contains(toolNamesOf(after), "browser_look") {
		t.Fatalf("revision %q → %q, tools %v", revision, next, toolNamesOf(after))
	}
}

// extmgr's word that the tools changed (Options.ToolsChanged → extensionToolsChanged) wakes a waiting watch at once,
// long before its heartbeat, with the new list.
func TestExtensionToolsChangedWakesAWaitingWatch(t *testing.T) {
	s := newTestService(t, newGuardFakeHub())
	ext := &toolsExt{}
	s.SetExtensions(ext)
	events := make(chan string, 4)
	previousEvent, heartbeat := mcpToolsWatchEvent, mcpToolsHeartbeat
	mcpToolsWatchEvent, mcpToolsHeartbeat = func(event string) { events <- event }, time.Hour
	t.Cleanup(func() { mcpToolsWatchEvent, mcpToolsHeartbeat = previousEvent, heartbeat })
	_, revision := discoveryTools(t, s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, ""))
	watch := make(chan map[string]any, 1)
	go func() {
		watch <- s.sessionMCPToolsSince(context.Background(), guardMachine, guardSession, true, false, revision)
	}()
	requireWatchEvent(t, events, "wait")
	ext.set("browser_look", "browser_run")
	s.extensionToolsChanged()
	select {
	case out := <-watch:
		after, next := discoveryTools(t, out)
		if next == revision || !slices.Contains(toolNamesOf(after), "browser_look") {
			t.Fatalf("revision %q → %q, tools %v", revision, next, toolNamesOf(after))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watch was not woken by the extensions' tool change")
	}
}
