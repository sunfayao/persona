package node

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/proto"
)

var macSandbox = sandbox.Support{OS: "darwin", SandboxExec: true}

// agentSandboxFakes makes the agent sandbox's look at this machine predictable: the probe finds sup, sandbox-exec
// takes every profile, there are no administrator's settings for Claude or Codex, and the environment says nothing
// about the switch, the data directory or Claude's config directory.
func agentSandboxFakes(t *testing.T, sup sandbox.Support) {
	t.Helper()
	probe, check := agentSandboxProbe, agentSandboxCheck
	claude, claudeDirs, codex := claudeManagedSettings, claudeManagedSettingsDirs, codexManagedConfig
	t.Cleanup(func() {
		agentSandboxProbe, agentSandboxCheck = probe, check
		claudeManagedSettings, claudeManagedSettingsDirs, codexManagedConfig = claude, claudeDirs, codex
	})
	agentSandboxProbe = func() sandbox.Support { return sup }
	agentSandboxCheck = func(string) error { return nil }
	none := filepath.Join(t.TempDir(), "none")
	claudeManagedSettings, claudeManagedSettingsDirs, codexManagedConfig = []string{none}, []string{none}, []string{none}
	for _, k := range []string{"PERSONA_AGENT_SANDBOX", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "PERSONA_HOME"} {
		t.Setenv(k, "")
	}
}

func TestAgentDataDirsCanonical(t *testing.T) {
	home := t.TempDir()
	canonical := filepath.Join(home, ".persona")
	t.Setenv("PERSONA_HOME", "")
	if got := agentDataDirs(home, canonical); !slices.Equal(got, []string{canonical}) {
		t.Fatalf("canonical root must occur once: %q", got)
	}
	custom := filepath.Join(home, "custom-state")
	t.Setenv("PERSONA_HOME", custom)
	if got := agentDataDirs(home, custom); !slices.Equal(got, []string{custom, canonical}) {
		t.Fatalf("configured root and default: %q", got)
	}
	nodeRoot := filepath.Join(home, "node-state")
	if got := agentDataDirs(home, nodeRoot); !slices.Equal(got, []string{nodeRoot, custom, canonical}) {
		t.Fatalf("distinct configured roots: %q", got)
	}
}

// agentNode is a test node with its data directory (~/.persona), the switch file there when on.
func agentNode(t *testing.T, on bool) *Node {
	t.Helper()
	n, home := testNode(t)
	n.logger = log.New(io.Discard, "", 0)
	n.dir = filepath.Join(home, ".persona")
	for _, d := range []string{"assistant", "launcher-settings"} {
		if err := os.MkdirAll(filepath.Join(n.dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if on {
		writeFile(t, filepath.Join(n.dir, sandbox.AgentOnFile), "")
	}
	return n
}

func inSandbox(args []string) bool {
	return len(args) > 3 && args[0] == "/usr/bin/sandbox-exec" && args[1] == "-p"
}

func realOf(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The assistant's claude on the CLI runs as before with the switch off (the default); on, sandbox-exec runs the same
// command, its launcher settings readable, and the environment can turn it off again.
func TestAssistantCLIInAgentSandbox(t *testing.T) {
	agentSandboxFakes(t, macSandbox)
	profile := t.TempDir()
	env := []string{"PATH=/usr/bin:/bin", "CLAUDE_CONFIG_DIR=" + profile}
	plain := func(args []string) []string {
		return append([]string{"/bin/zsh", "-ic", "echo " + assistantShellReady + "; CLAUDE_CONFIG_DIR=" + shellQuote(profile) + ` claude "$@"`, "persona"}, args...)
	}

	n := agentNode(t, false)
	settings := filepath.Join(n.dir, "launcher-settings", "claude-claude-settings.json")
	writeFile(t, settings, "{}")
	args := []string{"-p", "--settings", settings}
	cmd := n.assistantCLICommand("/bin/zsh", "claude", filepath.Join(n.dir, "assistant"), false, args, env)
	if !slices.Equal(cmd.Args, plain(args)) || !slices.Equal(cmd.Env, env) {
		t.Fatalf("off: %q %q", cmd.Args, cmd.Env)
	}
	if _, err := os.Stat(filepath.Join(n.dir, agentSandboxStatusFile)); err == nil {
		t.Error("off: a status was recorded")
	}

	n = agentNode(t, true)
	settings = filepath.Join(n.dir, "launcher-settings", "claude-claude-settings.json")
	writeFile(t, settings, "{}")
	args = []string{"-p", "--settings", settings}
	cmd = n.assistantCLICommand("/bin/zsh", "claude", filepath.Join(n.dir, "assistant"), false, args, env)
	if !inSandbox(cmd.Args) || !slices.Equal(cmd.Args[3:], plain(args)) {
		t.Fatalf("on: %q", cmd.Args)
	}
	if !slices.Contains(cmd.Env, sandbox.AgentEnv+"=1") || !slices.Contains(cmd.Env, env[0]) {
		t.Errorf("on: env %q", cmd.Env)
	}
	if want := `(allow file-read* (literal "` + realOf(t, settings) + `"))`; !strings.Contains(cmd.Args[2], want) {
		t.Errorf("its launcher settings are not readable:\n%s", cmd.Args[2])
	}
	if st := readAgentSandboxStatus(n.dir)["assistant"]; st.State != sandbox.AgentActive || st.At.IsZero() {
		t.Errorf("status %+v", st)
	}

	// A managed launcher's shell runs `personad launch` outside the sandbox: it fetches the launcher's settings, then
	// wraps the agent it execs itself (TestLaunchExec).
	n.setManaged([]proto.ManagedLauncher{{Name: "work"}})
	if cmd := n.assistantCLICommand("/bin/zsh", "work", filepath.Join(n.dir, "assistant"), false, args, env); inSandbox(cmd.Args) || cmd.Args[0] != "/bin/zsh" {
		t.Errorf("managed launcher: the shell was wrapped: %q", cmd.Args)
	}

	t.Setenv("PERSONA_AGENT_SANDBOX", "off")
	if cmd := n.assistantCLICommand("/bin/zsh", "claude", filepath.Join(n.dir, "assistant"), false, args, env); inSandbox(cmd.Args) {
		t.Errorf("PERSONA_AGENT_SANDBOX=off over the file: %q", cmd.Args)
	}
	t.Setenv("PERSONA_AGENT_SANDBOX", "on")
	n = agentNode(t, false)
	if cmd := n.assistantCLICommand("/bin/zsh", "claude", filepath.Join(n.dir, "assistant"), false, nil, env); !inSandbox(cmd.Args) {
		t.Errorf("PERSONA_AGENT_SANDBOX=on without the file: %q", cmd.Args)
	}
}

// On but not wrapped: the agent starts as before, and the status says why.
func TestAgentSandboxFallsBack(t *testing.T) {
	enabled := `{"sandbox": {"enabled": true}}`
	cases := []struct {
		name  string
		sup   sandbox.Support
		prep  func(t *testing.T, n *Node, cwd string) []string // more claude args
		state string
	}{
		{"no sandbox-exec", sandbox.Support{OS: "darwin", Why: "机器上没有 sandbox-exec"}, nil, sandbox.AgentUnavailable},
		{"linux without bwrap", sandbox.Support{OS: "linux", Why: "机器上没有 bwrap"}, nil, sandbox.AgentUnavailable},
		{"claude's own sandbox in the user's settings", macSandbox, func(t *testing.T, n *Node, cwd string) []string {
			writeFile(t, filepath.Join(n.home, ".claude", "settings.json"), enabled)
			return nil
		}, sandbox.AgentSkipped},
		{"in the project's local settings", macSandbox, func(t *testing.T, n *Node, cwd string) []string {
			writeFile(t, filepath.Join(cwd, ".claude", "settings.local.json"), enabled)
			return nil
		}, sandbox.AgentSkipped},
		{"in the administrator's settings", macSandbox, func(t *testing.T, n *Node, cwd string) []string {
			f := filepath.Join(t.TempDir(), "managed-settings.json")
			writeFile(t, f, enabled)
			claudeManagedSettings = []string{f}
			return nil
		}, sandbox.AgentSkipped},
		{"in --settings", macSandbox, func(t *testing.T, n *Node, cwd string) []string {
			return []string{"--settings", enabled}
		}, sandbox.AgentSkipped},
		{"settings that cannot be read", macSandbox, func(t *testing.T, n *Node, cwd string) []string {
			writeFile(t, filepath.Join(n.home, ".claude", "settings.json"), "{")
			return nil
		}, sandbox.AgentSkipped},
		{"a --settings file that is not there", macSandbox, func(t *testing.T, n *Node, cwd string) []string {
			return []string{"--settings", filepath.Join(n.home, "gone.json")}
		}, sandbox.AgentSkipped},
		{"sandbox-exec refuses the rules", sandbox.Support{OS: "darwin", SandboxExec: true}, func(t *testing.T, n *Node, cwd string) []string {
			agentSandboxCheck = func(string) error { return os.ErrInvalid }
			return nil
		}, sandbox.AgentUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agentSandboxFakes(t, c.sup)
			n := agentNode(t, true)
			cwd := filepath.Join(n.dir, "assistant")
			args := []string{"-p"}
			if c.prep != nil {
				args = append(args, c.prep(t, n, cwd)...)
			}
			env := []string{"PATH=/usr/bin"}
			cmd := n.assistantCLICommand("/bin/zsh", "claude", cwd, false, args, env)
			if inSandbox(cmd.Args) || cmd.Args[0] != "/bin/zsh" || !slices.Equal(cmd.Env, env) {
				t.Fatalf("wrapped: %q %q", cmd.Args, cmd.Env)
			}
			st := readAgentSandboxStatus(n.dir)["assistant"]
			if st.State != c.state || st.Why == "" {
				t.Errorf("status %+v, want %s with why", st, c.state)
			}
			if st.State == sandbox.AgentSkipped && st.Reason == nil {
				t.Errorf("Persona-owned inner reason has no metadata: %+v", st)
			}
		})
	}
	t.Run("off says nothing", func(t *testing.T) {
		agentSandboxFakes(t, macSandbox)
		n := agentNode(t, true)
		writeFile(t, filepath.Join(n.home, ".claude", "settings.json"), `{"sandbox": {"enabled": false, "autoAllowBashIfSandboxed": true}}`)
		if cmd := n.assistantCLICommand("/bin/zsh", "claude", filepath.Join(n.dir, "assistant"), false, nil, nil); !inSandbox(cmd.Args) {
			t.Errorf("sandbox.enabled false: not wrapped %q", cmd.Args)
		}
	})
}

// codex-start wraps a Codex that runs danger-full-access in its own process, leaves a managed launcher to `personad
// launch` and other words (pi) alone, and says why when it does not wrap a Codex.
func TestCodexStartExec(t *testing.T) {
	agentSandboxFakes(t, macSandbox)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".persona")
	writeFile(t, filepath.Join(dir, sandbox.AgentOnFile), "")
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(proj)
	for _, words := range [][]string{{"/x/personad", "launch", "codex-work"}, {"pi", "--session", "s1"}} {
		path := "/bin/" + filepath.Base(words[0])
		if p, argv, _, st := codexStartExec(path, words, nil, nil); p != path || !slices.Equal(argv, words) || st.State != sandbox.AgentOff {
			t.Errorf("%v: %s %q %+v", words, p, argv, st)
		}
	}
	if len(readAgentSandboxStatus(dir)) != 0 {
		t.Errorf("recorded %v", readAgentSandboxStatus(dir))
	}
	words := []string{"codex", "-c", `sandbox_mode="danger-full-access"`, "--", "hello"}
	command := func(words []string) []string {
		if runtime.GOOS != "windows" {
			if shell, err := exec.LookPath(loginShellProgram()); err == nil {
				return append([]string{shell, "-ic", codexNativeStartScript, "persona-codex-start", proj, "--"}, words...)
			}
		}
		return append([]string{"/opt/bin/codex"}, words[1:]...)
	}
	p, argv, env, st := codexStartExec("/opt/bin/codex", words, []string{"A=1"}, nil)
	if st.State != sandbox.AgentActive || p != argv[0] || !inSandbox(argv) || !slices.Equal(argv[3:], command(words)) || !slices.Contains(env, sandbox.AgentEnv+"=1") {
		t.Errorf("danger-full-access: %s %q %q %+v", p, argv, env, st)
	}
	words = []string{"codex", "--", "hello"}
	want := command(words)
	wantPath := want[0]
	if wantPath == "/opt/bin/codex" {
		want = words
	}
	if p, argv, _, st := codexStartExec("/opt/bin/codex", words, nil, nil); p != wantPath || !slices.Equal(argv, want) || st.State != sandbox.AgentSkipped || st.Why == "" {
		t.Errorf("plain codex: %s %q %+v", p, argv, st)
	}
	if st := readAgentSandboxStatus(dir)["codex-start"]; st.State != sandbox.AgentSkipped {
		t.Errorf("recorded %+v", st)
	}
}

// `personad launch` wraps a managed Claude, says why it does not wrap an agent whose own sandbox is not known, and
// inside the assistant's sandbox runs as it is.
func TestLaunchExec(t *testing.T) {
	agentSandboxFakes(t, macSandbox)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".persona")
	writeFile(t, filepath.Join(dir, sandbox.AgentOnFile), "")
	t.Chdir(home)
	argv := []string{"claude", "--model", "m"}
	exe, got, env, st := launchExec("claude", "/opt/bin/claude", argv, []string{"K=v"})
	if st.State != sandbox.AgentActive || exe != got[0] || !inSandbox(got) || got[3] != "/opt/bin/claude" || !slices.Equal(got[4:], argv[1:]) || !slices.Contains(env, sandbox.AgentEnv+"=1") {
		t.Errorf("claude: %s %q %q %+v", exe, got, env, st)
	}
	if exe, got, _, st := launchExec("agy", "/opt/bin/agy", []string{"agy"}, nil); exe != "/opt/bin/agy" || !slices.Equal(got, []string{"agy"}) || st.State != sandbox.AgentSkipped || st.Why == "" {
		t.Errorf("agy: %s %q %+v", exe, got, st)
	}
	exe, got, _, st = launchExec("claude", "/opt/bin/claude", argv, []string{sandbox.AgentEnv + "=1"})
	if exe != "/opt/bin/claude" || !slices.Equal(got, argv) || st.State != sandbox.AgentActive {
		t.Errorf("inside the assistant's sandbox: %s %q %+v", exe, got, st)
	}
	all := readAgentSandboxStatus(dir)
	if all["launch claude"].State != sandbox.AgentActive || all["launch agy"].State != sandbox.AgentSkipped {
		t.Errorf("recorded %v", all)
	}
}

// Everything the path guard calls a secret of the data directory, the sandbox closes too: they share the list
// (identity/secrets.go). The sandbox closes a few more things the path guard leaves (docs/server/agent-sandbox.md "Which
// files"), and neither touches a project's files below the data directory.
func TestAgentSandboxCoversPathGuard(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".persona")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	profile, err := sandbox.DarwinAgent(sandbox.AgentPlan{DataDirs: []string{dir}, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	real := realOf(t, dir)
	var res []*regexp.Regexp
	var subs []string
	block, _, _ := strings.Cut(profile, "(deny network-outbound")
	_, block, _ = strings.Cut(block, "(deny file-read* file-write*")
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), ")")
		if r, ok := strings.CutPrefix(line, `(regex #"`); ok {
			res = append(res, regexp.MustCompile(strings.TrimSuffix(r, `"`)))
		} else if s, ok := strings.CutPrefix(line, "(subpath "); ok {
			p, err := strconv.Unquote(s)
			if err != nil {
				t.Fatal(line)
			}
			subs = append(subs, p)
		}
	}
	if len(res) == 0 || len(subs) == 0 {
		t.Fatalf("no rules in\n%s", profile)
	}
	closed := func(rel string) bool {
		p := real + "/" + rel
		for _, re := range res {
			if re.MatchString(p) {
				return true
			}
		}
		for _, s := range subs {
			if p == s || strings.HasPrefix(p, s+"/") {
				return true
			}
		}
		return false
	}
	both := []string{"secret.key", "secret.key.bak", "handles.key~", "launch.key", "hub.key-old", "hub.json", "hub.json.bak", "link.json",
		"tailcat.json", "tailcat-hub.json", "daemons.json", "manager.json", "apns.json", "apns.p8", "relay.json", "relay.secret",
		"relay-tunnel.token", "edge.token", "edge.secret", "edge.password", "edge.token.bak",
		"devices.json", "mail_accounts.json.1", "tls/hub.key", "tls/hub.crt", "tls/.tls-1", "link-tls/id.key",
		"docker/db-password",
		"launcher-settings/claude-x.json", "managers/m1/manager.json", "managers/m1/secret.key", "managers/m1/tls/hub.key",
		"extensions/data/notes/token", "extensions/data/notes/sub/session.json", "extensions/staging/uninstall-x-1/0/token",
		"plugins/notes/manifest.json", "plugins/data/notes/token", "node-plugins/notes/data/session.json"}
	neither := []string{"mcp.json", "claude-settings.json", "assistant/notes.md", "data/proj/link.json", "data/certs/dev.pem",
		"extensions/notes/manifest.json", "data/extensions/data/x",
		"data/talk.key", "bin/personad", "uploads/a.png", "runtime-packages/x/node_modules/y/index.js", "node.sock",
		"data/proj/edge.token"}
	sandboxOnly := []string{".hub.json.12345", "managers/m1/manager.log", "extensions/run/notes/proxy.sock", "run/hub-edge.sock"}
	for _, rel := range both {
		if !dataSecret(rel) || !closed(rel) {
			t.Errorf("%s: path guard %v, sandbox %v; want both", rel, dataSecret(rel), closed(rel))
		}
	}
	for _, rel := range neither {
		if dataSecret(rel) || closed(rel) {
			t.Errorf("%s: path guard %v, sandbox %v; want neither", rel, dataSecret(rel), closed(rel))
		}
	}
	for _, rel := range sandboxOnly {
		if dataSecret(rel) || !closed(rel) {
			t.Errorf("%s: path guard %v, sandbox %v; want the sandbox only", rel, dataSecret(rel), closed(rel))
		}
	}
}
