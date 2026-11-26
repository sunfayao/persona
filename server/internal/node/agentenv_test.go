package node

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/identity"
)

// inAgentEnv is agentCommand's line for cmd in an sh-family pane of a node whose directory is dir, written out: what
// tests that look at a whole typed command expect.
func inAgentEnv(dir, cmd string, q func(string) string) string {
	return "unset " + strings.Join(agentUnsetNames(), " ") + " && PERSONA_HOME=" + q(dir) + " " + agentMarkEnv + " " + cmd
}

// jsonPrefix is s as the fake herdr's trace prints it (JSON, & as \u0026) without its closing quote: what a check on
// the start of a recorded command looks for.
func jsonPrefix(s string) string {
	b, _ := json.Marshal(s)
	return strings.TrimSuffix(string(b), `"`)
}

// legacyEnv is name's older spelling (compat.EnvNames).
func legacyEnv(name string) string {
	_, legacy := compat.EnvNames(name)
	return legacy
}

// twoStores is a home with an older installation's data directory beside the node's (#744): two real directories,
// neither an alias of the other, and no PERSONA_HOME in this process.
func twoStores(t *testing.T) (home, dir, legacy string) {
	t.Helper()
	home = t.TempDir()
	dir, legacy = mkdir(t, filepath.Join(home, ".persona")), mkdir(t, filepath.Join(home, compat.StateDir))
	t.Setenv("HOME", home)
	for _, k := range []string{"PERSONA_HOME", legacyEnv("PERSONA_HOME")} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	return home, dir, legacy
}

// What a start types (agentCommand): the directory and the unset in the sh family, the directory alone in fish, the
// command as it was in a shell that takes no `VAR=val cmd`, and from a node that has no directory.
func TestAgentCommandByShell(t *testing.T) {
	n, home := testNode(t)
	n.dir = filepath.Join(home, "my dir", ".persona")
	const cmd = "HERDR_AGENT=claude claude --session-id x"
	for _, sh := range []string{"/bin/zsh", "-zsh", "/bin/bash", "/bin/sh", "/usr/bin/dash", "/bin/ksh", "/bin/mksh"} {
		if got, want := n.agentCommand(cmd, shellQuote, sh), inAgentEnv(n.dir, cmd, shellQuote); got != want {
			t.Errorf("%s: %q\nwant %q", sh, got, want)
		}
	}
	if got, want := n.agentCommand(cmd, shellQuote, "/opt/homebrew/bin/fish"), "PERSONA_HOME="+shellQuote(n.dir)+" "+agentMarkEnv+" "+cmd; got != want {
		t.Errorf("fish: %q, want %q", got, want)
	}
	if got := n.agentCommand(cmd, shellQuote, "/bin/zsh", "/opt/homebrew/bin/fish"); strings.HasPrefix(got, "unset") {
		t.Errorf("fish in front of the pane got unset: %q", got)
	}
	for _, shells := range [][]string{{"/bin/tcsh"}, {"/usr/bin/nu"}, {"pwsh"}, {}, {"/bin/zsh", "/bin/tcsh"}} {
		if got := n.agentCommand(cmd, shellQuote, shells...); got != cmd {
			t.Errorf("%v: %q", shells, got)
		}
	}
	n.dir = ""
	if got := n.agentCommand(cmd, shellQuote, "/bin/zsh"); got != cmd {
		t.Errorf("no directory: %q", got)
	}
	// What the incident left in every Agent (#744) is in the list, in both spellings where Persona has two.
	names := assistantOnlyEnvNames()
	for _, k := range []string{"PERSONA_ASSISTANT", legacyEnv("PERSONA_ASSISTANT"), "PERSONA_SESSION_ID", legacyEnv("PERSONA_SESSION_ID"),
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW", "ENABLE_TOOL_SEARCH", "CLAUDE_EFFORT", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"} {
		if !slices.Contains(names, k) {
			t.Errorf("%s is not dropped", k)
		}
	}
	for _, k := range []string{"PERSONA_HOME", legacyEnv("PERSONA_HOME"), "PATH", "HOME", "HERDR_AGENT", "CLAUDE_CONFIG_DIR"} {
		if slices.Contains(names, k) {
			t.Errorf("%s is dropped", k)
		}
	}
}

// A start and herdr's resume of it, run by real shells whose environment is the assistant's (the engine was started
// from its Bash tool) and names the older directory (#744): whatever the launcher is, the claude it runs has the node's
// directory and none of the assistant's variables, and the shell is not left with PERSONA_HOME. A shell that is not
// installed is skipped.
func TestAgentStartInRealShellsHasTheNodesDirectoryOnly(t *testing.T) {
	posixDefs := "ccf() { claude --model from-function \"$@\"; }\nalias ccv='claude --model from-alias'\n"
	for _, sh := range []struct{ name, rc string }{{"bash", ".bashrc"}, {"zsh", ".zshrc"}, {"sh", ""}, {"dash", ""}} {
		t.Run(sh.name, func(t *testing.T) {
			bin, err := exec.LookPath(sh.name)
			if err != nil {
				t.Skipf("%s is not installed", sh.name)
			}
			withPaneShell(t, bin)
			home, dir, legacy := twoStores(t)
			path := filepath.Join(home, "bin")
			fakeProgram(t, filepath.Join(path, "claude"), `env >"$HOME/claude-env"`)
			fakeProgram(t, filepath.Join(path, "probe"), `printf '%s' "${PERSONA_HOME-unset}" >"$HOME/after"`)
			launchers := []string{"claude"}
			if sh.rc != "" {
				writeFile(t, filepath.Join(home, sh.rc), posixDefs)
				launchers = append(launchers, "ccf", "ccv")
			}
			env := []string{"HOME=" + home, "ZDOTDIR=" + home, "PATH=" + path + ":/usr/bin:/bin", legacyEnv("PERSONA_HOME") + "=" + legacy}
			for _, k := range assistantOnlyEnvNames() {
				env = append(env, k+"=from-the-assistant")
			}
			flags := []string{"-c"}
			if sh.rc != "" {
				flags = []string{"-ic"} // reads the rc file with the launchers in it
			}
			n, _ := testNode(t)
			n.dir = dir
			for _, l := range launchers {
				for _, resume := range []bool{false, true} {
					spec := claudeLaunch{Launcher: l}
					line := n.agentCommand(withHerdrAgent(typedHerdrAgent(&herdrHost{}, "claude", bin),
						n.claudeCommandLine(spec, "--session-id "+startID, shellQuote)), shellQuote, bin)
					if resume {
						argv, ok := n.claudeResumeArgv(spec, startID)
						if !ok {
							t.Fatal("no resume command")
						}
						line = argv[0] + " " + shellQuote(argv[1])
					}
					for _, f := range []string{"claude-env", "after"} {
						_ = os.Remove(filepath.Join(home, f))
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					cmd := exec.CommandContext(ctx, bin, append(flags, line+"; probe")...)
					cmd.Env = env
					out, err := cmd.CombinedOutput()
					cancel()
					if err != nil {
						t.Fatalf("%s: %v: %s", line, err, out)
					}
					got := map[string]string{}
					b, err := os.ReadFile(filepath.Join(home, "claude-env"))
					if err != nil {
						t.Fatalf("%s (%s, resume %v): claude did not run: %v", sh.name, l, resume, err)
					}
					for _, kv := range strings.Split(string(b), "\n") {
						if k, v, ok := strings.Cut(kv, "="); ok {
							got[k] = v
						}
					}
					if got["PERSONA_HOME"] != dir {
						t.Errorf("%s (%s, resume %v): PERSONA_HOME=%q", sh.name, l, resume, got["PERSONA_HOME"])
					}
					for _, k := range assistantOnlyEnvNames() {
						if _, ok := got[k]; ok {
							t.Errorf("%s (%s, resume %v): %s reached claude", sh.name, l, resume, k)
						}
					}
					if after, _ := os.ReadFile(filepath.Join(home, "after")); string(after) != "unset" {
						t.Errorf("%s (%s, resume %v): the shell kept PERSONA_HOME=%s", sh.name, l, resume, after)
					}
					// With what claude got, its MCP server, hooks and `personad launch` resolve this node's directory,
					// though the environment also names the older one.
					t.Setenv("PERSONA_HOME", got["PERSONA_HOME"])
					t.Setenv(legacyEnv("PERSONA_HOME"), got[legacyEnv("PERSONA_HOME")])
					if d := identity.DataDir(home); d != dir {
						t.Errorf("%s (%s, resume %v): resolves %s", sh.name, l, resume, d)
					}
				}
			}
		})
	}
}
