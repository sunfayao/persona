package node

import (
	"context"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// withPaneShell makes the shell herdr opens panes with shell for the test (TestMain: zsh).
func withPaneShell(t *testing.T, shell string) {
	t.Helper()
	old := herdrPaneShell
	herdrPaneShell = func() string { return shell }
	t.Cleanup(func() { herdrPaneShell = old })
}

// HERDR_AGENT (hostherdr.go, #3454): herdr is told the agent only for what Persona starts in a herdr pane, Claude and
// Codex only, and typed only into a shell that takes `VAR=val cmd`; a launcher run without a pane (the assistant,
// launcher checks) never gets it.
func TestHerdrAgentOnlyInHerdrPanes(t *testing.T) {
	herdr := &herdrHost{}
	for _, tc := range []struct {
		launch host.Launcher
		kind   string
		want   string
	}{
		{herdr, "claude", "claude"},
		{herdr, "codex", "codex"},
		{herdr, traexAgent, ""},
		{herdr, piAgent, "pi"},
	} {
		if got := herdrAgentFor(tc.launch, tc.kind); got != tc.want {
			t.Errorf("%T %s: %q, want %q", tc.launch, tc.kind, got, tc.want)
		}
	}
	for _, tc := range []struct {
		shells []string
		want   string
	}{
		{[]string{"/bin/zsh"}, "claude"}, {[]string{"-bash"}, "claude"}, {[]string{"fish"}, "claude"}, {[]string{"/bin/sh"}, "claude"},
		{[]string{"dash"}, "claude"}, {[]string{"ksh"}, "claude"}, {[]string{"mksh"}, "claude"},
		{[]string{"-tcsh"}, ""}, {[]string{"csh"}, ""}, {[]string{"pwsh"}, ""}, {[]string{"nu"}, ""}, {[]string{"elvish"}, ""},
		{[]string{"xonsh"}, ""}, {[]string{"zsh", "tcsh"}, ""}, {[]string{""}, ""}, {nil, ""},
	} {
		if got := typedHerdrAgent(herdr, "claude", tc.shells...); got != tc.want {
			t.Errorf("typed into %q: %q, want %q", tc.shells, got, tc.want)
		}
	}
	if got := withHerdrAgent("", "traex hi"); got != "traex hi" {
		t.Errorf("no agent: %q", got)
	}
	n, _ := testNode(t)
	d := "~/.claude-work"
	n.claudeStartDir.Store(&d)
	if got := n.launcherCommand("claude"); got != `CLAUDE_CONFIG_DIR="$HOME"/.claude-work claude` {
		t.Errorf("headless launcher command %q", got)
	}
}

// A launcher the hub says runs Codex (a wrapper) is typed into its herdr pane with HERDR_AGENT=codex in front, but
// not into a shell that cannot take it; a compatible CLI is left to herdr's own detection.
func TestOtherAgentStartTellsHerdrCodex(t *testing.T) {
	for _, tc := range []struct{ kind, launcher, shell, want string }{
		{"codex", "cxw", "/bin/zsh", "HERDR_AGENT=codex cxw hi"},
		{"codex", "cxw", "/bin/tcsh", "cxw hi"},
		{traexAgent, traexAgent, "/bin/zsh", "traex -- hi"},
	} {
		t.Run(tc.kind+" "+tc.shell, func(t *testing.T) {
			withPaneShell(t, tc.shell)
			n, f, home := herdrNode(t)
			f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
			f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.after(func(c herdrCall) {
				if c.Method == "pane.send_input" {
					cancel() // what was typed is all this test looks at
				}
			})
			_, _ = n.startOtherAgent(ctx, n.hosts.Get(host.Herdr).(*herdrHost), home, tc.launcher, tc.kind, "hi", "", "w1", "", "", modelExecution{})
			text := tc.want
			if tc.shell == "/bin/zsh" { // with the node's directory, without the assistant's environment (agentenv.go)
				text = inAgentEnv(n.dir, text, shellQuote)
			}
			want := call("pane.send_input", map[string]any{"pane_id": "w1:p2", "keys": []string{"Enter"}, "text": text})
			if got := f.trace("pane.send_input"); len(got) != 1 || got[0] != want {
				t.Errorf("typed %v, want %s", got, want)
			}
		})
	}
}

// herdr opens its panes with a shell that does not take `VAR=val cmd` (tcsh here; pwsh, nu…): a Claude start types
// its command as before, without HERDR_AGENT, and so does herdr's resume of it, which herdr types into such a shell.
func TestHerdrAgentLeftOutOfAShellThatCannotTakeIt(t *testing.T) {
	withPaneShell(t, "/bin/tcsh")
	n, f, home := herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.after(func(c herdrCall) {
		if c.Method == "pane.send_input" {
			cancel()
		}
	})
	_, _ = n.start(ctx, home, "claude", "", "", "w1", "", "", "", startID, "")
	typed := f.trace("pane.send_input")
	if len(typed) != 1 || !strings.Contains(typed[0], `"text":"claude --session-id `+startID+` `) {
		t.Errorf("typed %v", typed)
	}
	for _, spec := range []agentLaunch{{Launcher: "claude"}, {Launcher: "cx", Agent: "codex"}} {
		if argv, ok := n.agentResumeArgv(spec, startID); !ok || strings.Contains(argv[1], "HERDR_AGENT") {
			t.Errorf("%s resume_argv %q %v", spec.Launcher, argv, ok)
		}
	}
}

// screenHost is a bareHost whose panes all show screen.
type screenHost struct {
	bareHost
	screen string
}

func (h *screenHost) Read(context.Context, host.PaneRef, host.ReadOpts) (string, error) {
	return h.screen, nil
}

// stateless is a screenHost whose engine sees claude in front of every pane but reports no state for it, as a host can
// after the tab's rename until claude's first hook (#4438).
type stateless struct{ screenHost }

func (h *stateless) Agent(context.Context, host.PaneRef) (*host.Agent, error) {
	return &host.Agent{Agent: "claude", AgentStatus: proto.StatusUnknown}, nil
}
