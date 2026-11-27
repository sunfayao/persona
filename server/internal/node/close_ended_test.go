package node

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The owner's "close an Agent's terminal when its session ends" (#10355) reuses session.retire with close_terminal.
// after_exit also closes the marked, idle shell an Agent that already quit left behind (close_after_exit.go); without
// it a stopped Agent's terminal is kept, as before.

func TestRetireAfterExitClosesTheKeptShell(t *testing.T) {
	n, f, _ := closeAfterExitFixture(t)
	n.state.stopped(sidA, proto.StopExit)
	res, err := request(t, n, "session.retire", map[string]any{"session_id": sidA, "close_terminal": true, "after_exit": true})
	if err != nil || res["closed"] != true {
		t.Fatalf("retire after exit = %v, %v", res, err)
	}
	if calls := f.trace("pane.close", "tab.close"); len(calls) != 1 || calls[0] != `pane.close {"pane_id":"w1:p1"}` {
		t.Fatalf("kept shell not closed alone: %v", calls)
	}
	if got := n.stopCause(sidA); got != proto.StopExit {
		t.Fatalf("automatic close rewrote the stop cause: %q", got)
	}
}

func TestRetireCloseWithoutAfterExitKeepsTheShell(t *testing.T) {
	n, f, _ := closeAfterExitFixture(t)
	res, err := request(t, n, "session.retire", map[string]any{"session_id": sidA, "close_terminal": true})
	if err != nil || res["closed"] != false {
		t.Fatalf("retire = %v, %v", res, err)
	}
	if calls := f.trace("pane.close", "tab.close"); len(calls) != 0 {
		t.Fatalf("a stopped Agent's terminal was closed without after_exit: %v", calls)
	}
}

func TestRetireAfterExitLeavesAShellInUse(t *testing.T) {
	n, f, _ := closeAfterExitFixture(t)
	f.reply("pane.process_info", map[string]any{"process_info": map[string]any{"foreground_processes": []any{
		map[string]any{"pid": 2, "name": "vim", "argv0": "vim"}}}})
	res, err := request(t, n, "session.retire", map[string]any{"session_id": sidA, "close_terminal": true, "after_exit": true})
	if err == nil || res["closed"] == true {
		t.Fatalf("a shell running a program was closed: %v, %v", res, err)
	}
	if calls := f.trace("pane.close", "tab.close"); len(calls) != 0 {
		t.Fatalf("closed: %v", calls)
	}
}

func TestRetireAfterExitLeavesAnUnmarkedPane(t *testing.T) {
	n, f, cwd := closeAfterExitFixture(t)
	p := shellPane("w1:p1", "w1:t1", "w1", cwd, sidA)
	delete(p, "tokens") // a pane Persona did not open for this session
	f.reply("session.snapshot", snapshot(nil, nil, nil, []any{p}))
	listSessions(t, n)
	f.reset()
	_, _ = request(t, n, "session.retire", map[string]any{"session_id": sidA, "close_terminal": true, "after_exit": true})
	if calls := f.trace("pane.close", "tab.close"); len(calls) != 0 {
		t.Fatalf("an unmarked pane was closed: %v", calls)
	}
}
