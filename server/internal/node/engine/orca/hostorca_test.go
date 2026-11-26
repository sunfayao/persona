package orca

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

// When the processes cannot be listed, Orca's own word decides: its agent identity and a live agent row.
func TestOrcaWithoutProcessScan(t *testing.T) {
	t.Parallel()
	v := &orcaView{rows: map[string]orcart.AgentRow{
		"t:a": {PaneKey: "t:a", AgentType: "claude", State: "done", HasLivePty: true},
		"t:b": {PaneKey: "t:b", AgentType: "claude", State: "working"},
	}}
	term := func(leaf, identity string) orcart.Terminal {
		return orcart.Terminal{Handle: "h", TabID: "t", LeafID: leaf, AgentIdentity: identity}
	}
	if a := v.agent(term("a", "claude")); a.Agent != "claude" || a.AgentStatus != proto.StatusDone {
		t.Errorf("a: %+v", a)
	}
	for _, tm := range []orcart.Terminal{term("b", "claude"), term("a", ""), term("c", "claude")} {
		if a := v.agent(tm); a.Agent != "" {
			t.Errorf("%s: an agent %+v", tm.LeafID, a)
		}
	}
	// With the scan, a codex process is codex; its row's state counts only when the row is codex's.
	v = &orcaView{procsKnown: true, procs: map[string][]Process{"t:a": {{PID: 1, Agent: "codex"}}},
		rows: map[string]orcart.AgentRow{"t:a": {AgentType: "claude", State: "working"}}}
	if a := v.agent(term("a", "codex")); a.Agent != "codex" || a.AgentStatus != proto.StatusUnknown {
		t.Errorf("codex: %+v", a)
	}
	// The row folds in its subagents (working); the pane's own claude finished its turn.
	row := orcart.AgentRow{AgentType: "claude", State: "working"}
	row.MainAgent = &struct {
		State string `json:"state"`
	}{State: "done"}
	v = &orcaView{procsKnown: true, procs: map[string][]Process{"t:a": {{PID: 1, Agent: "claude"}}},
		rows: map[string]orcart.AgentRow{"t:a": row}}
	if a := v.agent(term("a", "claude")); a.AgentStatus != proto.StatusDone {
		t.Errorf("main agent done, children working: %+v", a)
	}
}

// The process scan keeps the claude / codex processes in front (not suspended, not in the background), only the pane
// key out of their environment (never the hook token), skips agents inside a herdr / herdr-core nested in an Orca
// terminal, finds claude and codex under node too, falls back on ORCA_AGENT_PANE, and lists claude first.
func TestOrcaProcessParsing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	args := strings.Join([]string{
		"  101 S+   01:00 /usr/local/bin/claude --resume x",
		"  102 S+   1-02:03:04 node /opt/lib/node_modules/@anthropic-ai/claude-code/cli.js",
		"  103 R+   00:05 /opt/bin/codex",
		"  104 Ss+  00:05 -zsh",
		"  105 T+   00:09 /usr/local/bin/claude", // suspended (ctrl+z)
		"  106 S    00:09 /usr/local/bin/claude", // in the background (claude -p … &)
		"  107 S+   00:09 /usr/local/bin/claude", // inside a herdr nested in the Orca terminal
		"  108 S+   00:09 /usr/local/bin/claude", // inside herdr-core
		"  100 S+   00:30 node /opt/lib/node_modules/@openai/codex/bin/codex.js",
		"garbage",
	}, "\n")
	cands := ParsePSArgs(args, now)
	if _, ok := cands[105]; ok {
		t.Error("a suspended claude counts")
	}
	if _, ok := cands[106]; ok {
		t.Error("a background claude counts")
	}
	if p := cands[102]; p.Start != now.Add(-(26*time.Hour + 3*time.Minute + 4*time.Second)) {
		t.Errorf("start %v", p.Start)
	}
	env := strings.Join([]string{
		"  101 /usr/local/bin/claude --resume x TERM=xterm ORCA_AGENT_HOOK_TOKEN=secret ORCA_PANE_KEY=tab1:leaf1 HOME=/h",
		"  102 node /opt/lib/node_modules/@anthropic-ai/claude-code/cli.js ORCA_AGENT_PANE=tab2:leaf2",
		"  103 /opt/bin/codex ORCA_PANE_KEY=tab1:leaf1",
		"  107 /usr/local/bin/claude ORCA_PANE_KEY=tab5:leaf5 HERDR_PANE_ID=w1:p1",
		"  108 /usr/local/bin/claude ORCA_PANE_KEY=tab5:leaf5 HC_TERMINAL_ID=t3",
		"  100 node /opt/lib/node_modules/@openai/codex/bin/codex.js ORCA_PANE_KEY=tab4:leaf4",
		"  999 /usr/local/bin/claude ORCA_PANE_KEY=tab9:leaf9", // not a candidate
	}, "\n")
	got := ParsePSEnvironment(env, cands, nil) // the pty owners: TestOrcaNestedPtysAreNotThePanes
	pids := map[string][]int{}
	for pane, ps := range got {
		for _, p := range ps {
			pids[pane] = append(pids[pane], p.PID)
		}
	}
	want := map[string][]int{"tab1:leaf1": {101, 103}, "tab2:leaf2": {102}, "tab4:leaf4": {100}}
	if fmt.Sprint(pids) != fmt.Sprint(want) {
		t.Errorf("parsed %v\nwant %v", pids, want)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), "secret") {
		t.Error("the hook token was kept")
	}
	for stat, front := range map[string]bool{
		"42 (claude) S 1 42 42 34816 42 4194304":      true,
		"42 (claude code) S 1 42 42 34816 77 4194304": false, // background: another group has the terminal
		"42 (claude) T 1 42 42 34816 42 4194304":      false, // stopped
	} {
		if procInFront(stat) != front {
			t.Errorf("procInFront(%q) = %v", stat, !front)
		}
	}
	for s, want := range map[string]time.Duration{"00:05": 5 * time.Second, "01:02:03": time.Hour + 2*time.Minute + 3*time.Second,
		"2-00:00:01": 48*time.Hour + time.Second} {
		if d, ok := parseEtime(s); !ok || d != want {
			t.Errorf("parseEtime(%q) = %v", s, d)
		}
	}
}

// Keys become xterm bytes, modifiers included; keys that only move or interrupt do not count as typing.
func TestOrcaKeyBytes(t *testing.T) {
	t.Parallel()
	for k, want := range map[string]string{
		"enter": "\r", "backspace": "\x7f", "ctrl+c": "\x03", "tab": "\t", "1": "1", "y": "y", "down": "\x1b[B",
		"space": " ", "ctrl+space": "\x00", "alt+b": "\x1bb", "alt+left": "\x1b[1;3D", "ctrl+left": "\x1b[1;5D",
		"shift+right": "\x1b[1;2C", "ctrl+up": "\x1b[1;5A", "f1": "\x1bOP", "f5": "\x1b[15~", "f12": "\x1b[24~",
		"shift+f1": "\x1b[1;2P", "delete": "\x1b[3~", "insert": "\x1b[2~", "pageup": "\x1b[5~", "home": "\x1b[H",
		"alt+backspace": "\x1b\x7f", "alt+space": "\x1b ", "alt+tab": "\x1b\t", "shift+a": "A", "alt+2": "\x1b2",
		"shift+enter": "\x1b\r", "ctrl+enter": "\x1b[13;5u", "ctrl+a": "\x01", "esc": "\x1b",
	} {
		if got, ok := screen.XtermKeyBytes(k); !ok || got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	for _, k := range []string{"hyper+q", "f0", "ctrl+tab", ""} {
		if b, ok := screen.XtermKeyBytes(k); ok {
			t.Errorf("%q has bytes %q", k, b)
		}
	}
	for b, typing := range map[string]bool{"\r": false, "\x1b": false, "\x1b[1;3D": false, "\x1bOP": false, "\x03": false,
		"y": true, " ": true, "\x1bb": true, "\x1b[15~": false} {
		if orcaTypes(b) != typing {
			t.Errorf("orcaTypes(%q) = %v", b, !typing)
		}
	}
}

// The process trees as a Mac shows them (2026-11-06): an agent in an Orca pane sits under Orca's terminal host, which
// has no pane key; one inside a tmux started from that pane sits under the tmux server, which inherited the key.
const orcaPSTree = `
    1     0 ??
26485     1 ??
30044 26485 ttys001
30046 30044 ttys001
30058 30046 ttys001
30059 30058 ttys001
22651     1 ??
22656 22651 ttys004
22658 22656 ttys004
22675 22658 ttys004
40000 30059 ttys009
40001 40000 ttys009
`

// A pty made inside the pane (tmux, zellij, screen, script, a node-pty in an MCP server…) is not the pane: its
// processes inherited the pane key and are each "in front" of that pty while the pane may show a shell. Counted as
// the pane's, tmux's `claude -p` workers took the pane from the claude that started them (#5463); a message would go
// to the pane's shell as a command once claude had left it. Told by the pty's owner, not by a variable: Orca's own
// agent teams set TMUX in a real pane, and it stays the pane's.
func TestOrcaNestedPtysAreNotThePanes(t *testing.T) {
	t.Parallel()
	tree := parsePSTree(orcaPSTree)
	if q := ptyOwner(tree, 30059); q != 26485 {
		t.Errorf("owner of the pane's claude: %d, want Orca's terminal host 26485", q)
	}
	if q := ptyOwner(tree, 22675); q != 22651 {
		t.Errorf("owner of tmux's claude: %d, want the tmux server 22651", q)
	}
	if q := ptyOwner(tree, 99999); q != 0 {
		t.Errorf("an unknown process has owner %d", q)
	}
	cands := ParsePSArgs(strings.Join([]string{
		"30059 S+   04:00:00 claude --resume 3029711c",
		"22675 S+   00:05:00 claude -p do-triage-a",
		"40001 S+   00:01:00 claude", // under `script` run by the pane's claude: its own pty
	}, "\n"), time.Now())
	owners := map[int]int{}
	for pid := range cands {
		owners[pid] = ptyOwner(tree, pid)
	}
	env := strings.Join([]string{
		"30059 claude --resume 3029711c ORCA_PANE_KEY=tab1:leaf1 TMUX=/tmp/orca-claude-agent-teams/t1,0,1",
		"22675 claude -p do-triage-a TMUX=/private/tmp/tmux-501/default,22651,0 ORCA_PANE_KEY=tab1:leaf1",
		"40001 claude ORCA_PANE_KEY=tab1:leaf1",
		"26485 orca-terminal-host HOME=/Users/u",
		"22651 tmux HOME=/Users/u ORCA_PANE_KEY=tab1:leaf1",
		"40000 node pty-host.js HOME=/Users/u ORCA_PANE_KEY=tab1:leaf1",
	}, "\n")
	got := ParsePSEnvironment(env, cands, owners)
	if ps := got["tab1:leaf1"]; len(ps) != 1 || ps[0].PID != 30059 {
		t.Fatalf("the pane's processes: %+v, want only the claude of the pane's own pty", ps)
	}
	// An owner whose environment ps cannot show (Apple's own binaries: /usr/bin/script, screen) may carry the key:
	// not taken. Here the pane's own owner is shown without its environment.
	noEnv := strings.Replace(env, "26485 orca-terminal-host HOME=/Users/u", "26485 /usr/bin/script", 1)
	if ps := ParsePSEnvironment(noEnv, cands, owners)["tab1:leaf1"]; len(ps) != 0 {
		t.Errorf("taken with an owner whose environment is unknown: %+v", ps)
	}
	delete(owners, 30059) // its owner is unknown (the chain broke): not taken as the pane's
	if ps := ParsePSEnvironment(env, cands, owners)["tab1:leaf1"]; len(ps) != 0 {
		t.Errorf("taken without a known owner: %+v", ps)
	}
}

// A started terminal the user moves to another tab is still in Orca's UI: it is listed where it is now, and that tab
// is renamed, not the one it was made in (#5547 keeps only an orphaned one under its first key).
func TestOrcaListsAMovedTerminalWhereItIs(t *testing.T) {
	t.Parallel()
	fake := orcarttest.New(t)
	root := t.TempDir()
	fake.SetWorktrees(orcarttest.Worktree("wt-1", root, "refs/heads/main"))
	h := New(Config{
		Client: &orcart.Client{MetaPath: fake.MetaPath},
		NewID:  func() string { return "00000000-0000-4000-8000-000000000001" },
		Scan:   func() (map[string][]Process, error) { return nil, nil },
	})
	created, err := h.CreateSpace(context.Background(), root, "tab1")
	if err != nil {
		t.Fatalf("CreateSpace: %v", err)
	}
	fake.Move("term_fake_1", "tab_moved", "leaf_moved")
	l, err := h.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Panes) != 1 || l.Panes[0].PaneID != (host.PaneRef{Kind: host.Orca, ID: "tab_moved:leaf_moved"}).String() || l.Panes[0].TabID != "tab_moved" {
		t.Errorf("panes %+v, want the moved one under tab_moved:leaf_moved (made as %s)", l.Panes, created.Pane.ID)
	}
	if err := h.RenameSlot(context.Background(), "tab_moved", "renamed"); err != nil {
		t.Errorf("RenameSlot(tab_moved): %v", err)
	}
	if tr := fake.Trace("terminal.rename"); len(tr) != 1 || !strings.Contains(tr[0], `"terminal":"term_fake_1"`) {
		t.Errorf("renames %v", tr)
	}
}

// Run types only into the terminal this host made: one Orca orphaned without re-keying it is refused, and so is a
// new handle under the same pane key (#972: Persona trusts the handle terminal.create returned).

// #3828: a row blocked by a subagent waiting on the user is blocked, though its main agent works on (it waits for the
// subagent) or has settled: the pane needs the user. A row working only because of its background subagents is not.
func TestOrcaSubagentWaitingNeedsYou(t *testing.T) {
	t.Parallel()
	term := orcart.Terminal{Handle: "h", TabID: "t", LeafID: "a", AgentIdentity: "claude"}
	for _, c := range []struct{ row, main, want string }{
		{"blocked", "working", proto.StatusBlocked},
		{"waiting", "done", proto.StatusBlocked},
		{"working", "done", proto.StatusDone},
		{"blocked", "", proto.StatusBlocked},
	} {
		row := orcart.AgentRow{AgentType: "claude", State: c.row}
		if c.main != "" {
			row.MainAgent = &struct {
				State string `json:"state"`
			}{State: c.main}
		}
		v := &orcaView{procsKnown: true, procs: map[string][]Process{"t:a": {{PID: 1, Agent: "claude"}}},
			rows: map[string]orcart.AgentRow{"t:a": row}}
		if a := v.agent(term); a.AgentStatus != c.want {
			t.Errorf("row %s, main %s: %s, want %s", c.row, c.main, a.AgentStatus, c.want)
		}
	}
}
