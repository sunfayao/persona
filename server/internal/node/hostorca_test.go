package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/node/engine/orca"
	"github.com/sunfayao/persona/server/internal/nodeclient"
	"github.com/sunfayao/persona/server/internal/orcart"
	"github.com/sunfayao/persona/server/internal/orcart/orcarttest"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rexcli"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/screen"
	"github.com/sunfayao/persona/server/internal/terncli"
)

// Orca as the machine's engine (hostorca.go, engine.go), driven through the node's requests against a fake Orca
// runtime socket (orcarttest) and a fake process table. No test here talks to a real Orca.

func TestMain(m *testing.M) {
	if os.Getenv("NODE_TEST_LAUNCH") == "1" { // the real managed-launch path, before the test harness clears its environment
		if err := Launch(context.Background(), os.Args[2], os.Args[3:], ""); err != nil {
			panic(err)
		}
		os.Exit(0)
	}
	if mode := os.Getenv("NODE_TEST_PLUGIN"); mode != "" { // the test binary as a plugin's program (plugins_test.go)
		runTestPlugin(mode)
	}
	if os.Getenv("NODE_TEST_PREFLIGHT") == "1" { // the test binary as the preflight's child (preflight_test.go)
		// NODE_TEST_PREFLIGHT_SLOW: start that much later, as on a busy machine; NODE_TEST_PREFLIGHT_STARTED: a file
		// to write the time to once running, for a test that times the run without the start.
		if d, err := time.ParseDuration(os.Getenv("NODE_TEST_PREFLIGHT_SLOW")); err == nil {
			time.Sleep(d)
		}
		if f := os.Getenv("NODE_TEST_PREFLIGHT_STARTED"); f != "" {
			_ = os.WriteFile(f, []byte(time.Now().Format(time.RFC3339Nano)), 0o600)
		}
		preflightChild(os.Args[1:], os.Stdout)
		os.Exit(0)
	}
	// A plain run of the whole package: its tests in concurrent processes of this binary (partition_test.go, #5734).
	if code, ran := runPartitioned(); ran {
		os.Exit(code)
	}
	// Nor `node doctor` as the preflight's child: the test binary again, in that mode, given long enough to come up
	// under the whole server suite's load (#1110); only a child that never starts waits that long.
	preflightChildCmd = func(string) ([]string, []string) {
		self, _ := os.Executable()
		return []string{self}, []string{"NODE_TEST_PREFLIGHT=1"}
	}
	preflightStart = 30 * time.Second
	// A Codex the fake herdr starts or resumes never draws its input box, so each start would watch the full 8 s for
	// the update offer; the tests that show the offer show it at once, and a second still reads the screen a few times.
	codexStartupLook = time.Second
	// No test may reach the user's running Orca: the default metadata path points nowhere.
	os.Setenv("ORCA_USER_DATA_PATH", "/tmp/persona-test-no-orca")
	// Nor the user's botmux: a node registers no botmux host unless a test gives it one.
	botmuxBin = func() string { return "" }
	// Nor the user's Tern: no tern binary is found (a test that wants one gives its client a fake).
	ternClient = func() *terncli.Client { return &terncli.Client{Bin: func() string { return "" }} }
	// Nor the user's Rex: no rex binary is found (a test that wants one gives its client a fake).
	rexClient = func() *rexcli.Client { return &rexcli.Client{Bin: func() string { return "" }} }
	// Nor the user's gh outside the test's PATH (ghpath.go).
	ghFallbackDirs = func() []string { return nil }
	// Nor the user's Codex catalog: a model recovery on a Codex list runs no `codex debug models` (a test that wants
	// windows gives its own, assistant_fallback_window.go).
	codexDebugModels = func(context.Context, *Node, string) ([]byte, error) { return nil, errors.New("no codex in tests") }
	// Subscription usage tests provide their own app-server instead of polling a real account.
	codexUsageCLI = func() (string, error) { return "", errors.New("no codex in tests") }
	// Nor the ChatGPT app's bundled codex: a test that wants one sets its own (codex_bundled_test.go).
	bundledCodexCLI = func() string { return "" }
	// Nor the user's Paseo: its home points nowhere (a test that wants one sets its own).
	os.Setenv("PASEO_HOME", "/tmp/persona-test-no-paseo")
	// Nor the user's Cindy: its home points nowhere (a test that wants one sets its own).
	os.Setenv("CINDY_HOME", "/tmp/persona-test-no-cindy")
	// Nor the user's T3 Code: its home points nowhere and its command line is not found (a test that wants one sets
	// its own).
	os.Setenv("T3CODE_HOME", "/tmp/persona-test-no-t3code")
	t3codeBin = func() string { return "" }
	// Nor the user's Claude config directory (claude under a profile, or these tests run from inside a Claude Code
	// session): the node's own default is then ~/.claude of the test's home (#2140; a test that wants one sets its own).
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	// Nor the user's state directory (these tests run from inside a Persona conversation, or a shell that set it): the
	// state directory of a test's node is the one under its own home. A PERSONA_HOME, or its former name, left in the
	// environment won over it, and the health uploads' tests wrote their fake reports into the user's real health
	// space. A test that wants one sets its own.
	for _, k := range []string{"PERSONA_HOME", legacyEnv("PERSONA_HOME")} {
		os.Unsetenv(k)
	}
	// Nor the user's login shell: herdr's panes run zsh, whatever the machine's is (a test that wants another sets it).
	herdrPaneShell = func() string { return "/bin/zsh" }
	// A send looks at the box once after typing and confirms nothing: most fakes never write what was typed into the
	// transcript, and would wait out the window (#6428). sendconfirm_test.go turns it on (withSubmitWindow).
	submitWindow = 0
	os.Exit(m.Run())
}

const orcaCwd = "/tmp/orcaproj"

// fakeProcs is the process table the Orca host scans: agent processes by pane key.
type orcaTestClock struct{ offset atomic.Int64 }

func (c *orcaTestClock) Now() time.Time                       { return time.Now().Add(time.Duration(c.offset.Load())) }
func (*orcaTestClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (c *orcaTestClock) advance(d time.Duration)              { c.offset.Add(int64(d)) }

type fakeProcs struct {
	clock orcaTestClock
	mu    sync.Mutex
	procs map[string][]orcaProc
	err   error
	scans int
}

func (p *fakeProcs) scan() (map[string][]orcaProc, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scans++
	out := map[string][]orcaProc{}
	for k, v := range p.procs {
		out[k] = slices.Clone(v)
	}
	return out, p.err
}

func (p *fakeProcs) set(pane string, procs ...orcaProc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.procs == nil {
		p.procs = map[string][]orcaProc{}
	}
	if len(procs) == 0 {
		delete(p.procs, pane)
		return
	}
	p.procs[pane] = procs
}

// orcaNode is a Node whose engine is Orca, on a fake runtime and process table; the fake herdr it also gets
// (herdrNode's) is not registered, so a call to it would show in its trace.
func orcaNode(t *testing.T, options ...func(*orca.Config)) (*Node, *fakeHerdr, *orcarttest.Fake, *fakeProcs, string) {
	t.Helper()
	n, f, home := herdrNode(t)
	o := orcarttest.New(t)
	procs := &fakeProcs{}
	cfg := orcaConfig(&orcart.Client{MetaPath: o.MetaPath})
	cfg.Scan, cfg.Clock = procs.scan, &procs.clock
	for _, option := range options {
		option(&cfg)
	}
	h := orca.New(cfg)
	n.hosts = host.NewRegistry(h)
	n.eng.name = proto.EngineOrca
	t.Cleanup(n.starts.close) // before the fake Orca runtime closes
	return n, f, o, procs, home
}

func orcaTerm(handle, tab, leaf, identity string) map[string]any {
	return orcarttest.Terminal(handle, tab, leaf, "repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x", "✳ "+handle, identity)
}

// The claude terminals are live sessions, each named by its claude process's sessions/<pid>.json (found by the pane key
// in its environment); a terminal whose claude exited (Orca still says claude), a shell and another machine's terminal
// are not; spaces are worktrees named by branch, slots their tabs; states come from worktree.ps (waiting → blocked).
func TestOrcaListsClaudeTerminals(t *testing.T) {
	n, f, o, procs, home := orcaNode(t)
	for _, id := range []string{sidA, sidB, sidC} {
		writeTranscript(t, home, orcaCwd, id, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	}
	writeClaudePID(t, home, 4242, sidA)
	writeClaudePID(t, home, 4343, sidB)
	writeClaudePID(t, home, 4444, sidC)
	remote := orcaTerm("term_r", "tabR", "leafR", "claude")
	remote["executionHostId"] = "ssh-test-host"
	o.SetTerminals(
		orcaTerm("term_1", "tab1", "leaf1", "claude"),
		orcaTerm("term_2", "tab1", "leaf2", "claude"),
		orcaTerm("term_3", "tab2", "leaf3", "claude"), // no row yet: unknown
		orcaTerm("term_4", "tab2", "leaf4", "claude"), // its claude exited
		orcaTerm("term_5", "tab3", "leaf5", ""),       // a shell
		remote,
	)
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x",
		orcarttest.Agent("tab1:leaf1", "claude", "working"),
		orcarttest.Agent("tab1:leaf2", "claude", "waiting"),
		orcarttest.Agent("tab2:leaf4", "claude", "done"),
	))
	procs.set("tab1:leaf1", orcaProc{PID: 4242, Agent: "claude"})
	procs.set("tab1:leaf2", orcaProc{PID: 4343, Agent: "claude"})
	procs.set("tab2:leaf3", orcaProc{PID: 4444, Agent: "claude"})
	procs.set("tabR:leafR", orcaProc{PID: 4545, Agent: "claude"}) // cannot happen, but must not matter

	list := listSessions(t, n)
	m := byID(list)
	a, b, c := m[sidA], m[sidB], m[sidC]
	if a.PaneID != "orca:tab1:leaf1" || a.Status != proto.StatusWorking || a.SpaceKind != host.Orca ||
		a.SpaceID != "repo::"+orcaCwd || a.SpaceName != "feature-x" || a.SpaceIndex != 1 || a.SlotID != "tab1" ||
		a.SlotName != "tab tab1" || a.SlotIndex != 1 {
		t.Errorf("term_1: %+v", a)
	}
	if b.PaneID != "orca:tab1:leaf2" || b.Status != proto.StatusBlocked {
		t.Errorf("term_2: %+v", b)
	}
	if c.PaneID != "orca:tab2:leaf3" || c.Status != proto.StatusUnknown || c.SlotIndex != 2 {
		t.Errorf("term_3: %+v", c)
	}
	live := 0
	for _, s := range list {
		if s.PaneID != "" {
			live++
		}
	}
	if live != 3 {
		t.Errorf("%d live sessions: %v", live, ids(list))
	}
	// Listing reads; it neither types nor reads screens: only the claude without a row is asked about (Orca's own
	// reading of the terminal, the state when its hooks have none); herdr is not asked.
	if tr := o.Trace(); strings.Join(tr, "|") != `terminal.agentStatus {"terminal":"term_3"}` {
		t.Errorf("orca calls beyond listing: %v", tr)
	}
	if inv := o.Invalid(); len(inv) != 0 {
		t.Errorf("params Orca would refuse: %v", inv)
	}
	if tr := f.trace(); len(tr) != 0 {
		t.Errorf("herdr called: %v", tr)
	}
}

// orcaSession is a node with one claude session in Orca's terminal term_1 (pane tab1:leaf1).
func orcaSession(t *testing.T) (*Node, *orcarttest.Fake, *fakeProcs) {
	t.Helper()
	n, _, o, procs, home := orcaNode(t)
	writeTranscript(t, home, orcaCwd, sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeClaudePID(t, home, 4242, sidA)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"))
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x",
		orcarttest.Agent("tab1:leaf1", "claude", "done")))
	procs.set("tab1:leaf1", orcaProc{PID: 4242, Agent: "claude"})
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "orca:tab1:leaf1" {
		t.Fatalf("not listed: %v", m)
	}
	o.Reset()
	return n, o, procs
}

// Orca turning the input down (nothing written) is a refusal the app can retry: a permission dialog is dialog_open,
// no agent in front / a gone or read-only terminal is not_delivered.
func TestOrcaSendRefusedByOrca(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "❯ \n")
	for _, c := range []struct {
		answer any
		code   string
	}{
		{map[string]any{"send": map[string]any{"accepted": false, "bytesWritten": 0, "refusedReason": "permission"}}, "dialog_open"},
		{map[string]any{"send": map[string]any{"accepted": false, "bytesWritten": 0, "refusedReason": "no-agent"}}, "not_delivered"},
		{orcarttest.Fail{Code: "terminal_not_writable"}, "not_delivered"},
	} {
		o.Handle("terminal.send", func(map[string]any) any { return c.answer })
		_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
		if got, data := refusalOf(t, err); got != c.code || data["delivered"] != "none" {
			t.Errorf("%v: %v %v", c.answer, got, data)
		}
	}
	// An error that leaves open whether Orca wrote: not a clean refusal.
	o.Handle("terminal.send", func(map[string]any) any { return orcarttest.Fail{Code: "runtime_error"} })
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
	var re *rpc.Error
	if err == nil || errors.As(err, &re) && re.Code == "not_delivered" {
		t.Errorf("runtime_error: %v", err)
	}
}

// The claude exited and the pane is back at its shell (Orca may still name claude): the shell does not get the
// message as a command — refused before anything is typed, and the session is no longer live.
func TestOrcaSendAfterTheAgentExited(t *testing.T) {
	n, o, procs := orcaSession(t)
	o.SetScreen("term_1", "$ \n")
	procs.set("tab1:leaf1")
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "rm -rf build"})
	if code, _ := refusalOf(t, err); code != "not_delivered" {
		t.Fatalf("send to an exited agent: %v", err)
	}
	if tr := o.Trace("terminal.send"); len(tr) != 0 {
		t.Errorf("typed: %v", tr)
	}
	procs.clock.advance(orcaProcsMaxAge + time.Nanosecond)
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "" {
		t.Errorf("an exited agent still listed live: %+v", m[sidA])
	}
}

// Keys go as their bytes, one write each; text is typed as is, then its keys; an unknown key is refused before
// anything is pressed.
func TestOrcaKeysAndText(t *testing.T) {
	n, o, _ := orcaSession(t)
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"esc", "shift+tab", "ctrl+u", "up"}}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace(), []string{
		orcarttest.SendCall("term_1", map[string]any{"text": "\x1b"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\x1b[Z"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\x15"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\x1b[A"}),
	})
	o.Reset()
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "text": "yes please", "keys": []string{"enter"}}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace(), []string{
		`terminal.agentStatus {"terminal":"term_1"}`,
		orcarttest.SendCall("term_1", map[string]any{"text": "yes please"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\r"}),
	})
	o.Reset()
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"enter", "hyper+q"}}); err == nil {
		t.Error("an unknown key was taken")
	}
	if tr := o.Trace(); len(tr) != 0 {
		t.Errorf("pressed before refusing: %v", tr)
	}
	for k, want := range map[string]string{"enter": "\r", "backspace": "\x7f", "ctrl+c": "\x03", "tab": "\t", "1": "1",
		"y": "y", "down": "\x1b[B", "space": " "} {
		if got, ok := screen.XtermKeyBytes(k); !ok || got != want {
			t.Errorf("%s = %q", k, got)
		}
	}
}

// A colored screen read gets the plain rows (CRLF-ended like colored ones); "recent" reads the screen too.
func TestOrcaScreenIsPlain(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "line one\nline two\n")
	res, err := request(t, n, "session.screen", map[string]any{"session_id": sidA, "format": "ansi"})
	if err != nil {
		t.Fatal(err)
	}
	if res["text"] != "line one\r\nline two\r\n" || res["status"] != proto.StatusDone {
		t.Errorf("screen = %q", res)
	}
	if _, err := request(t, n, "session.screen", map[string]any{"session_id": sidA, "source": "recent", "lines": 500}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace(), []string{
		`terminal.read {"screen":true,"terminal":"term_1"}`,
		`terminal.read {"screen":true,"terminal":"term_1"}`,
	})
	if _, err := request(t, n, "terminal.watch", map[string]any{"session_id": sidA}); err == nil ||
		!strings.HasPrefix(err.Error(), "unsupported") {
		t.Errorf("terminal.watch = %v", err)
	}
}

// A handle Orca no longer knows (the terminal came back under a new one) is looked up again by the pane key.
func TestOrcaStaleHandle(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetTerminals(orcaTerm("term_1b", "tab1", "leaf1", "claude"))
	o.SetScreen("term_1b", "hi\n")
	res, err := request(t, n, "session.screen", map[string]any{"session_id": sidA})
	if err != nil || res["text"] != "hi\n" {
		t.Fatalf("screen = %v, %v", res, err)
	}
	assertTrace(t, o.Trace("terminal.read"), []string{
		`terminal.read {"screen":true,"terminal":"term_1"}`,
		`terminal.read {"screen":true,"terminal":"term_1b"}`,
	})
}

// Agent renames target only the Agent terminal.
func TestOrcaRenameTargetsTheAgent(t *testing.T) {
	n, o, _ := orcaSession(t)
	if _, err := request(t, n, "session.rename", map[string]any{"session_id": sidA, "tab_name": "Agent review"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace("terminal.rename"), []string{`terminal.rename {"terminal":"term_1","title":"Agent review"}`})
	if len(o.Trace("worktree.set")) != 0 {
		t.Fatal("renaming an Agent touched its worktree")
	}
	o.Reset()
	o.SetTerminals()
	if _, err := request(t, n, "session.rename", map[string]any{"session_id": sidA, "tab_name": "Gone"}); err == nil {
		t.Fatal("renamed a missing terminal")
	}
	if len(o.Trace("worktree.set", "terminal.rename", "terminal.send")) != 0 {
		t.Fatal("a refused rename reached another target")
	}
}

// Orca not running (no metadata, or its socket gone): no sessions at once, nothing logged, the refresh succeeds.
func TestOrcaAbsentOrGone(t *testing.T) {
	n, _, o, procs, home := orcaNode(t)
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, orcaCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"))
	procs.set("tab1:leaf1", orcaProc{PID: 4242, Agent: "claude"})
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); !ok {
		t.Fatal("Orca's session not found")
	}
	o.Stop()
	procs.clock.advance(time.Second) // past the failed list kept for Agents
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); ok {
		t.Error("Orca quit, its session is still live")
	}

	var logged []string
	var mu sync.Mutex
	c := &orcart.Client{MetaPath: "/tmp/persona-no-such-dir/orca-runtime.json", Log: func(format string, args ...any) {
		mu.Lock()
		logged = append(logged, fmt.Sprintf(format, args...))
		mu.Unlock()
	}}
	cfg := orcaConfig(c)
	cfg.Scan = procs.scan
	h := orca.New(cfg)
	n.hosts.Set(h)
	start := time.Now()
	for range 3 {
		if err := n.refreshHosts(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("3 refreshes took %s", took)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 0 {
		t.Errorf("logged %q", logged)
	}
}

// node.info finds Orca when its runtime answers with a protocol Persona speaks; an incompatible one is there, not
// available, with a note.
func TestOrcaEngineDetection(t *testing.T) {
	o := orcarttest.New(t)
	e := orcaEngine(context.Background(), &orcart.Client{MetaPath: o.MetaPath})
	if e != (proto.Engine{ID: "orca", Name: "Orca", Available: true, Version: "1.4.245"}) {
		t.Errorf("engine %+v", e)
	}
	o.SetStatus(map[string]any{"runtimeProtocolVersion": 2})
	e = orcaEngine(context.Background(), &orcart.Client{MetaPath: o.MetaPath})
	if e.Available || e.Note == "" || e.Version != "1.4.245" {
		t.Errorf("incompatible: %+v", e)
	}
	e = orcaEngine(context.Background(), &orcart.Client{MetaPath: "/tmp/persona-no-such-dir/orca-runtime.json"})
	if e != (proto.Engine{ID: "orca", Name: "Orca"}) {
		t.Errorf("absent: %+v", e)
	}
	if hs := engineHosts(proto.EngineOrca, nil); len(hs) != 1 || hs[0].Kind() != host.Orca {
		t.Errorf("orca hosts %v", hs)
	}
}

// engine.use orca swaps the hosts for Orca's and lists its sessions; back to herdr drops them.
func TestOrcaEngineUse(t *testing.T) {
	n, _, home := herdrNode(t)
	o := orcarttest.New(t)
	t.Setenv("ORCA_USER_DATA_PATH", filepath.Dir(o.MetaPath))
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, orcaCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"))
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "", orcarttest.Agent("tab1:leaf1", "claude", "working")))
	procs := &fakeProcs{}
	procs.set("tab1:leaf1", orcaProc{PID: 4242, Agent: "claude"})
	old := newOrcaHost
	newOrcaHost = func(c *orcart.Client) *orca.Host {
		cfg := orcaConfig(c)
		cfg.Scan = procs.scan
		return orca.New(cfg)
	}
	t.Cleanup(func() { newOrcaHost = old })
	if _, err := request(t, n, "engine.use", map[string]any{"engine": "orca"}); err != nil {
		t.Fatal(err)
	}
	h, ok := n.hosts.Get(host.Orca).(*orcaHost)
	if !ok || len(n.hosts.All()) != 2 || n.hosts.All()[1].Kind() != host.Tmux {
		t.Fatalf("hosts %v", hostKinds(n))
	}
	if m := byID(listSessions(t, n)); m[sidB].PaneID != "orca:tab1:leaf1" || m[sidB].Status != proto.StatusWorking ||
		m[sidB].SpaceName != "orcaproj" {
		t.Errorf("sessions %v", m)
	}
	if n.defaultHost() != host.Host(h) {
		t.Error("Orca is not the default host")
	}
	if _, err := request(t, n, "engine.use", map[string]any{"engine": "herdr"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); ok {
		t.Error("Orca's session kept after switching back")
	}
}

func TestHookPaneOrca(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"ORCA_PANE_KEY": "tab1:leaf1"}, "orca:tab1:leaf1"},
		{map[string]string{"ORCA_AGENT_PANE": "tab1:leaf1"}, "orca:tab1:leaf1"},
		{map[string]string{"HERDR_PANE_ID": "w1:p2", "ORCA_PANE_KEY": "tab1:leaf1"}, "w1:p2"},
		{map[string]string{"HC_TERMINAL_ID": "t7", "ORCA_PANE_KEY": "tab1:leaf1"}, "herdr-core:t7"},
	} {
		if got := nodeclient.HookPane(env(c.env)); got != c.want {
			t.Errorf("%v: HookPane = %q, want %q", c.env, got, c.want)
		}
	}
	if r := host.ParsePane("orca:tab1:leaf1"); r != (host.PaneRef{Kind: host.Orca, ID: "tab1:leaf1"}) {
		t.Errorf("ParsePane = %+v", r)
	}
}

// Text goes in only when an agent is in front: Orca writes anything else raw into whatever is there (a shell would run
// it). Not in front — our scan finds none (suspended, background, nested: TestOrcaProcessParsing), or the scan failed
// — is refused before anything is typed, for prompts, typed text and keys that type; enter / esc / ctrl+c still go
// (a stuck pane can be interrupted and closed). Orca's own "no agent" is not a refusal (TestOrcaSendsWhileClaudeHas
// BackgroundTasks).
func TestOrcaRefusesTextWithoutAnAgentInFront(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(o *orcarttest.Fake, p *fakeProcs)
	}{
		{"no agent process in front", func(_ *orcarttest.Fake, p *fakeProcs) { p.set("tab1:leaf1") }},
		{"the process scan failed", func(_ *orcarttest.Fake, p *fakeProcs) {
			p.mu.Lock()
			p.err = errors.New("ps: exit status 1")
			p.mu.Unlock()
		}},
	} {
		n, o, procs := orcaSession(t)
		o.SetScreen("term_1", "❯ \n")
		c.setup(o, procs)
		for _, req := range []map[string]any{
			{"session_id": sidA, "text": "rm -rf build"},
		} {
			_, err := request(t, n, "session.send", req)
			if code, data := refusalOf(t, err); code != "not_delivered" || data["delivered"] != "none" {
				t.Errorf("%s: send: %v", c.name, err)
			}
		}
		for _, req := range []map[string]any{
			{"session_id": sidA, "text": "yes", "keys": []string{"enter"}},
			{"session_id": sidA, "keys": []string{"y"}},
		} {
			if _, err := request(t, n, "session.keys", req); err == nil {
				t.Errorf("%s: keys %v went in", c.name, req)
			}
		}
		if w := o.Writes(); len(w) != 0 {
			t.Errorf("%s: wrote %+v", c.name, w)
		}
		if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"ctrl+c"}}); err != nil {
			t.Errorf("%s: ctrl+c: %v", c.name, err)
		}
		if w := o.Writes(); len(w) != 1 || w[0].Text != "\x03" {
			t.Errorf("%s: ctrl+c writes %+v", c.name, w)
		}
	}
}

// A permission dialog in front (Orca's agentStatus): a prompt would answer it — refused as dialog_open; the dialog's
// own answer keys go through.
func TestOrcaPermissionDialog(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "❯ \n") // our screen check sees nothing: Orca knows better
	o.SetAgentStatus("term_1", true, "permission")
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
	if code, data := refusalOf(t, err); code != "dialog_open" || data["delivered"] != "none" {
		t.Fatalf("prompt into a permission dialog: %v", err)
	}
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"1"}}); err != nil {
		t.Fatalf("answer key: %v", err)
	}
	if w := o.Writes(); len(w) != 1 || w[0].Text != "1" {
		t.Errorf("writes %+v", w)
	}
}

// The terminal behind the pane gets another process between the check that an agent is in front and the write (a new
// incarnation): the write, fenced to the process that was checked, is refused, nothing typed — it is not retried
// against the new one.
func TestOrcaNewIncarnationRefusesWrites(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "❯ \n")
	o.Handle("terminal.agentStatus", func(map[string]any) any {
		t2 := orcaTerm("term_1", "tab1", "leaf1", "claude")
		t2["incarnationId"] = "inc-new"
		o.SetTerminals(t2)
		return map[string]any{"agentStatus": map[string]any{"handle": "term_1", "isRunningAgent": true, "status": nil}}
	})
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
	if code, _ := refusalOf(t, err); code != "not_delivered" {
		t.Fatalf("send to a replaced process: %v", err)
	}
	if w := o.Writes(); len(w) != 0 {
		t.Errorf("wrote %+v", w)
	}
	// A read follows the pane to its new process.
	o.SetScreen("term_1", "new\n")
	if res, err := request(t, n, "session.screen", map[string]any{"session_id": sidA}); err != nil || res["text"] != "new\n" {
		t.Errorf("read after the change: %v %v", res, err)
	}
}

// States: a pane without a worktree.ps row takes Orca's agentStatus (working / permission → blocked / idle → done);
// a row older than the pane's agent process is the previous agent's and does not count.
func TestOrcaStatusFallbackAndStaleRows(t *testing.T) {
	n, _, o, procs, home := orcaNode(t)
	writeTranscript(t, home, orcaCwd, sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeTranscript(t, home, orcaCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeClaudePID(t, home, 4242, sidA)
	writeClaudePID(t, home, 4343, sidB)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"), orcaTerm("term_2", "tab2", "leaf2", "claude"))
	old := orcarttest.Agent("tab2:leaf2", "claude", "working")
	old["updatedAt"] = time.Now().Add(-time.Hour).UnixMilli()
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "", old))
	o.SetAgentStatus("term_1", true, "permission")
	o.SetAgentStatus("term_2", true, "idle")
	procs.set("tab1:leaf1", orcaProc{PID: 4242, Agent: "claude", Start: time.Now().Add(-time.Minute)})
	procs.set("tab2:leaf2", orcaProc{PID: 4343, Agent: "claude", Start: time.Now().Add(-time.Minute)})
	m := byID(listSessions(t, n))
	if m[sidA].Status != proto.StatusBlocked || m[sidB].Status != proto.StatusDone {
		t.Errorf("states %s %s", m[sidA].Status, m[sidB].Status)
	}
	// The fallback is cached: the next poll does not ask again.
	o.Reset()
	listSessions(t, n)
	if c := o.Count("terminal.agentStatus"); c != 0 {
		t.Errorf("asked again %d times", c)
	}
}

// Polling is light: the layouts (costly for Orca) come only when the terminals or a topology changed; worktree.ps
// answers "unchanged" to its snapshot; Agents after a Snapshot reuses its look.
func TestOrcaPollingCost(t *testing.T) {
	n, o, _ := orcaSession(t)
	h := n.hosts.Get(host.Orca).(*orcaHost)
	for range 3 {
		listSessions(t, n)
	}
	layouts := func() int {
		c := 0
		for _, l := range o.Trace("terminal.list") {
			if strings.Contains(l, `"includeVisualLayouts":true`) {
				c++
			}
		}
		return c
	}
	if c := layouts(); c != 0 {
		t.Errorf("%d layout reads without a change", c)
	}
	if tr := o.Trace("worktree.ps"); len(tr) != 3 || strings.Contains(tr[1], "null") {
		t.Errorf("worktree.ps %v", tr)
	}
	o.SetRevision("repo::"+orcaCwd, 2)
	listSessions(t, n)
	if c := layouts(); c != 1 {
		t.Errorf("%d layout reads after a topology change", c)
	}
	o.Reset()
	if _, err := h.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Agents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := o.Count("terminal.list"); c != 1 {
		t.Errorf("Snapshot then Agents listed %d times", c)
	}
}

// Pressing keys stops between keys when the caller gives up.
func TestOrcaKeysHonourContext(t *testing.T) {
	_, o, _ := orcaSession(t)
	h := newOrcaHost(&orcart.Client{MetaPath: o.MetaPath})
	if _, err := h.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := h.SendKeys(ctx, host.PaneRef{Kind: host.Orca, ID: "tab1:leaf1"}, "esc", "esc", "esc")
	if err == nil || len(o.Writes()) > 1 {
		t.Errorf("keys after cancel: %v, %d writes", err, len(o.Writes()))
	}
}

// A hook reporting an Orca pane is bound only on a machine whose engine is Orca; a herdr machine stays as it was.
func TestOrcaHookPaneBinding(t *testing.T) {
	n, _, _ := herdrNode(t)
	_, _ = hubPipe(t, n), 0
	in, _ := json.Marshal(map[string]any{"session_id": sidA, "hook_event_name": "Notification"})
	_, _ = n.handleHook(context.Background(), sockRequest{Kind: "hook", Input: in, Pane: "orca:tab1:leaf1"})
	n.mu.Lock()
	_, bound := n.panes["orca:tab1:leaf1"]
	n.mu.Unlock()
	if bound {
		t.Error("an Orca pane bound on a herdr machine")
	}
	n.eng.name = proto.EngineOrca
	_, _ = n.handleHook(context.Background(), sockRequest{Kind: "hook", Input: in, Pane: "orca:tab1:leaf1"})
	n.mu.Lock()
	_, bound = n.panes["orca:tab1:leaf1"]
	n.mu.Unlock()
	if !bound {
		t.Error("an Orca pane not bound on an Orca machine")
	}
}

// A child worktree (Orca's "N children") carries its parent's id as its space's parent, on the space and on the sessions
// in it; a top-level one, and one that names itself, has none (#2073).
func TestOrcaChildWorktreeNamesItsParent(t *testing.T) {
	n, _, o, procs, home := orcaNode(t)
	writeTranscript(t, home, orcaCwd, sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeClaudePID(t, home, 4242, sidA)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"))
	parent := orcarttest.Worktree("repo::/code/main", "/code/main", "refs/heads/main")
	child := orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x", orcarttest.Agent("tab1:leaf1", "claude", "working"))
	child["parentWorktreeId"] = "repo::/code/main"
	self := orcarttest.Worktree("repo::/code/self", "/code/self", "refs/heads/self")
	self["parentWorktreeId"] = "repo::/code/self"
	o.SetWorktrees(parent, child, self)
	procs.set("tab1:leaf1", orcaProc{PID: 4242, Agent: "claude"})

	if s := byID(listSessions(t, n))[sidA]; s.SpaceID != "repo::"+orcaCwd || s.ParentSpaceID != "repo::/code/main" {
		t.Errorf("child session: space %q parent %q", s.SpaceID, s.ParentSpaceID)
	}
	h := n.hosts.Get(host.Orca)
	snap, err := h.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := h.(interface {
		Spaces(context.Context) ([]host.Space, error)
	}).Spaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, spaces := range map[string][]host.Space{"snapshot": snap.Spaces, "catalog": catalog} {
		got := map[string]string{}
		for _, sp := range spaces {
			got[sp.ID] = sp.ParentID
		}
		want := map[string]string{"repo::/code/main": "", "repo::" + orcaCwd: "repo::/code/main", "repo::/code/self": ""}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s parents %v, want %v", name, got, want)
		}
	}
}

// #3828: a row blocked by a subagent waiting on the user is blocked, though its main agent works on (it waits for the
// subagent) or has settled: the pane needs the user. A row working only because of its background subagents is not.
func TestOrcaSubagentWaitingNeedsYou(t *testing.T) {
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
		if got := orcaChatStatus(row); got != c.want {
			t.Errorf("chat: row %s, main %s: %s, want %s", c.row, c.main, got, c.want)
		}
	}
}
