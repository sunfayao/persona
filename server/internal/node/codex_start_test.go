package node

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Starting a Codex from the phone (codex_start.go) against a fake herdr: the launcher runs through codex-start with the
// first message in a file, the start answers with the thread that message began, and the pane lists as that one
// session.

// specialPrompt has what a shell would take apart: quotes, a newline, $, backticks, Chinese, and a lot of it.
var specialPrompt = "fix \"the\" 'build' $HOME `whoami` \\n;\n第二行：中文 && rm -rf / # " + strings.Repeat("长", 3000)

const startThread = "01a19a00-0000-7000-8000-00000000c0de"

var startFileRE = regexp.MustCompile(` codex-start '?([^' ]+?)'?$`)

// startRolloutVersion is the Codex version codexStartHerdr's rollouts name.
var startRolloutVersion = "0.169.0"

// ran is the launcher's words the last start's file held, ranAgent its HERDR_AGENT.
var (
	ran      []string
	ranAgent string
)

// codexStartHerdr is a node on a fake herdr whose new tab's launcher "runs" Codex: once the command comes in, a rollout
// for a new thread with the file's message appears in ~/.codex (after delay).
func codexStartHerdr(t *testing.T, delay time.Duration) (*Node, *fakeHerdr, string, *[]string) {
	t.Helper()
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	f.reply("workspace.list", map[string]any{"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "proj"}}})
	f.reply("session.snapshot", snapshot(nil, nil, []any{agentOf("codex", "", proto.StatusWorking, "w1:p2", "w1:t2", "w1", cwd, "")}, nil))
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	f.setScreen("w1:p2", "")
	var mu sync.Mutex
	var cmds []string
	ran, ranAgent = nil, ""
	f.after(func(c herdrCall) {
		if c.Method != "pane.send_input" || str(c.Params["pane_id"]) != "w1:p2" {
			return
		}
		cmd := str(c.Params["text"])
		mu.Lock()
		cmds = append(cmds, cmd)
		mu.Unlock()
		m := startFileRE.FindStringSubmatch(cmd)
		if m == nil {
			return
		}
		var file codexStartFile
		if b, err := os.ReadFile(m[1]); err != nil || json.Unmarshal(b, &file) != nil {
			return
		}
		mu.Lock()
		ran, ranAgent = file.Argv, file.Agent
		mu.Unlock()
		prompt := file.Prompt
		go func() {
			time.Sleep(delay)
			day := time.Now()
			dir := mkdir(t, filepath.Join(home, ".codex", "sessions", day.Format("2006"), day.Format("01"), day.Format("02")))
			now := time.Now().UTC().Format(time.RFC3339Nano)
			resolved, _ := filepath.EvalSymlinks(cwd) // Codex records the shell's working directory, resolved
			meta, _ := json.Marshal(map[string]any{"timestamp": now, "type": "session_meta", "payload": map[string]any{"id": startThread, "cwd": resolved, "cli_version": startRolloutVersion}})
			content := []any{}
			if prompt != "" {
				content = append(content, map[string]any{"type": "text", "text": prompt})
			}
			for _, path := range file.Images {
				content = append(content, map[string]any{"type": "local_image", "path": path})
			}
			user, _ := json.Marshal(map[string]any{"timestamp": now, "type": "event_msg", "payload": map[string]any{"type": "item_completed",
				"item": map[string]any{"type": "UserMessage", "id": "u1", "content": content}}})
			_ = os.WriteFile(filepath.Join(dir, "rollout-2026-11-02T10-00-00-"+startThread+".jsonl"), []byte(string(meta)+"\n"+string(user)+"\n"), 0o600)
		}()
	})
	return n, f, cwd, &cmds
}

func withCodexLaunchers(t *testing.T, launchers map[string][]string) {
	t.Helper()
	launcherCache.Lock()
	old, oldAt, oldAliases := launcherCache.codex, launcherCache.at, launcherCache.codexAliases
	aliases := make(map[string]bool, len(launchers))
	for name := range launchers {
		aliases[name] = true
	}
	launcherCache.codex, launcherCache.at, launcherCache.codexAliases = launchers, time.Now(), aliases
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.codex, launcherCache.at, launcherCache.codexAliases = old, oldAt, oldAliases
		launcherCache.Unlock()
	})
}

func TestCodexExecutionArgsBeforePromptSeparator(t *testing.T) {
	got := codexExecutionArgs([]string{"codex", "--"}, modelExecution{Model: "purpose", Effort: "high"})
	want := []string{"codex", "--model", "purpose", "-c", "model_reasoning_effort=high", "--"}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestCodexPromptOnlyTaskKeepsLauncherPermissions(t *testing.T) {
	args := []string{"codex", "-c", `sandbox_mode="danger-full-access"`, "--ask-for-approval", "on-request"}
	withCodexLaunchers(t, map[string][]string{"task-codex": args})
	n, _, _, _ := codexStartHerdr(t, 0)
	_, err := request(t, n, "session.start", map[string]any{
		"cwd": "~/proj", "launcher": "task-codex", "permission_mode": "default",
		"prompt": "Inspect the project. Do not edit files, commit, or make external changes.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ran, args) {
		t.Fatalf("prompt-only launch changed runtime permissions: %q, want %q", ran, args)
	}
}

func TestCodexStartRefusals(t *testing.T) {
	withCodexLaunchers(t, map[string][]string{"codex": {"codex"}})
	n, _, _, cmds := codexStartHerdr(t, 0)
	_, err := request(t, n, "session.start", map[string]any{"cwd": "~/proj", "launcher": "codex", "prompt": "hi", "async": true})
	if code, _ := refusalOf(t, err); code != "codex_needs_prompt" {
		t.Errorf("async: %v", err)
	}
	if len(*cmds) != 0 {
		t.Errorf("ran %q", *cmds)
	}
}

func TestCodexStartWaitFromEnv(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 180 * time.Second},
		{" 4m30s ", 270 * time.Second},
		{"invalid", 180 * time.Second},
		{"0s", 180 * time.Second},
		{"-1s", 180 * time.Second},
	} {
		t.Setenv(codexStartWaitEnv, tc.value)
		if got := codexStartWaitFromEnv(); got != tc.want {
			t.Errorf("%s=%q: got %s, want %s", codexStartWaitEnv, tc.value, got, tc.want)
		}
	}
}

// codexTrustDialogs are Codex's folder trust dialogs by version: 0.167's as `herdr pane read --source visible` read it
// off a new tab (Codex 0.167.0 in a fresh git repository, 2026-11-09), 0.163's, and 0.153's as the earlier match knew it.
func codexTrustDialogs(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "codexstart", "trust-0.167.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"0.167": string(b),
		"0.163": "  Trust this folder? Codex can read, edit, and run files here.\n\n› 1. Trust and continue\n  2. Quit\n\n  enter continue · esc quit\n",
		"0.153": "> You are in /tmp/proj\n\n  Do you trust the contents of this directory? Working with untrusted contents comes with higher risk of prompt injection.\n\n" +
			"› 1. Yes, continue\n  2. No, quit\n\n  Press enter to continue\n",
	}
}

// tallPane answers pane.read the way herdr does for a pane rows tall whose screen is drawn from its top (fakeHerdr's
// screen): "visible" leaves out the blank rows under it, "recent" counts them, so the last 40 recent rows of a 51-row
// pane showing Codex 0.167's trust dialog are all blank (#2351).
func tallPane(f *fakeHerdr, rows int) {
	f.handle("pane.read", func(p map[string]any) any {
		f.mu.Lock()
		text := f.screen[str(p["pane_id"])]
		f.mu.Unlock()
		all := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		for len(all) < rows {
			all = append(all, "")
		}
		if p["source"] == "visible" {
			for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
				all = all[:len(all)-1]
			}
		}
		if lines, _ := p["lines"].(float64); lines > 0 && int(lines) < len(all) {
			all = all[len(all)-int(lines):]
		}
		return map[string]any{"read": map[string]any{"text": strings.Join(all, "\n") + "\n"}}
	})
}

// codexTrustKeys walks the cursor to the option that trusts the folder, and answers no other dialog.
func TestCodexTrustKeys(t *testing.T) {
	dialogs := codexTrustDialogs(t)
	onSecond := strings.Replace(strings.Replace(dialogs["0.167"], "› 1. Trust", "  1. Trust", 1), "  2. Back", "› 2. Back", 1)
	trustSecond := "  Trust this folder?\n\n› 1. Back to Agent Command Center\n  2. Trust and continue\n\n  enter continue · esc back\n"
	for name, tc := range map[string]struct {
		screen string
		want   []string
	}{
		"0.167":                      {dialogs["0.167"], []string{"enter"}},
		"0.163":                      {dialogs["0.163"], []string{"enter"}},
		"0.153":                      {dialogs["0.153"], []string{"enter"}},
		"cursor on the other option": {onSecond, []string{"up", "enter"}},
		"trust listed second":        {trustSecond, []string{"down", "enter"}},
		"no cursor drawn yet":        {strings.Replace(dialogs["0.167"], "› 1.", "  1.", 1), nil},
		"no trust option":            {"  Trust this folder?\n\n› 1. Back to Agent Command Center\n  2. Quit\n", nil},
		"update prompt":              {"  ✨ Update available! 0.166.0 -> 0.167.0\n\n› 1. Update now\n  2. Skip\n  3. Skip until next version\n\n  Press enter to continue\n", nil},
		"approval of a command naming trust": {"  Would you like to run the following command?\n\n  $ cat trust.txt\n\n› 1. Yes, proceed (y)\n" +
			"  2. No, and tell Codex what to do differently (esc)\n\n  Press enter to confirm or esc to cancel\n", nil},
		"composer": {"› Ask Codex to do anything\n\n  gpt-6 default · ~/proj\n", nil},
	} {
		if got := codexTrustKeys(tc.screen); !slices.Equal(got, tc.want) {
			t.Errorf("%s: keys %q, want %q", name, got, tc.want)
		}
	}
}

// codex-start passes the message on byte for byte, as one argument after "--", with the launcher's variables set.
func TestCodexStartHelper(t *testing.T) {
	if os.Getenv("PERSONA_CODEX_START_HELPER") == "1" {
		var args []string
		_ = json.Unmarshal([]byte(os.Getenv("PERSONA_CODEX_START_ARGS")), &args)
		err := CodexStart(args)
		fmt.Println(err)
		os.Exit(3)
	}
	dir := t.TempDir()
	file, out := filepath.Join(dir, "start.json"), filepath.Join(dir, "out")
	sh, _ := exec.LookPath("sh")
	b, _ := json.Marshal(codexStartFile{Argv: []string{"OUT=" + out, sh, "-c", `printf '%s|%s' "$1" "$2" > "$OUT"`, "sh"}, Prompt: specialPrompt})
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal([]string{file})
	cmd := exec.Command(os.Args[0], "-test.run=^TestCodexStartHelper$")
	cmd.Env = append(os.Environ(), "PERSONA_CODEX_START_HELPER=1", "PERSONA_CODEX_START_ARGS="+string(args))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "--|"+specialPrompt {
		t.Errorf("passed %q", got)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("the file was left behind")
	}
}

func TestCodexStartPreservesNativeShellFunction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows launchers do not use a Unix shell")
	}
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not installed")
	}
	dir := t.TempDir()
	file, out := filepath.Join(dir, "start.json"), filepath.Join(dir, "out")
	program := filepath.Join(dir, "codex")
	script := "#!/bin/sh\nprintf '%s\\n' \"$PERSONA_TEST_LAUNCHER_MARK\" \"$@\" > \"$OUT\"\n"
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	rc := `export PATH="$PERSONA_TEST_CODEX_BIN:$PATH"
function codex { PERSONA_TEST_LAUNCHER_MARK=from-function command codex --function-flag "$@"; }
alias codex='codex --alias-already-expanded'
`
	if err := os.WriteFile(filepath.Join(dir, ".zshrc"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, prompt string }{
		{"with_first_message", specialPrompt},
		{"without_first_message", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := json.Marshal(codexStartFile{Argv: []string{"codex", "--alias-already-expanded"}, Prompt: tt.prompt})
			if err := os.WriteFile(file, b, 0o600); err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal([]string{file})
			cmd := exec.Command(os.Args[0], "-test.run=^TestCodexStartHelper$")
			cmd.Env = append(os.Environ(), "HOME="+dir, "ZDOTDIR="+dir, "SHELL=/bin/zsh",
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "OUT="+out, "PERSONA_TEST_CODEX_BIN="+dir,
				"PERSONA_HOME="+filepath.Join(dir, "state"), "PERSONA_AGENT_SANDBOX=off",
				"PERSONA_CODEX_START_HELPER=1", "PERSONA_CODEX_START_ARGS="+string(args))
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, b)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			want := "from-function\n--function-flag\n--alias-already-expanded\n"
			if tt.prompt != "" {
				want += "--\n" + tt.prompt + "\n"
			}
			if string(got) != want {
				t.Fatalf("launcher environment/flags or literal prompt lost: got %d bytes, want %d; prefix %q",
					len(got), len(want), got[:min(100, len(got))])
			}
		})
	}
}

func TestCodexStartHonorsLoginShellWithBothRCFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows launchers do not use a Unix shell")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	for _, shell := range []struct {
		name, path, marker, flag string
	}{
		{"bash", bash, "from-bash", "--from-bash"},
		{"zsh", zsh, "from-zsh", "--from-zsh"},
	} {
		for _, prompt := range []struct {
			name  string
			value string
		}{
			{"with_first_message", specialPrompt},
			{"without_first_message", ""},
		} {
			for _, lookup := range []string{"inherited_path", "rc_path", "function_only"} {
				t.Run(shell.name+"/"+prompt.name+"/"+lookup, func(t *testing.T) {
					root := t.TempDir()
					home := filepath.Join(root, "home")
					bin := filepath.Join(root, "bin")
					if err := os.MkdirAll(home, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(bin, 0o700); err != nil {
						t.Fatal(err)
					}
					for name, path := range map[string]string{"bash": bash, "zsh": zsh} {
						if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
							t.Fatal(err)
						}
					}
					out := filepath.Join(root, "fake-codex.out")
					launcherBin := bin
					if lookup != "inherited_path" {
						launcherBin = filepath.Join(root, "launchers")
						if err := os.MkdirAll(launcherBin, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					program := filepath.Join(launcherBin, "codex")
					script := "#!/bin/sh\n{\nprintf 'RC=%s\\n' \"$PERSONA_TEST_RC\"\nprintf 'FUNCTION=%s\\n' \"$PERSONA_TEST_FUNCTION\"\nfor arg do printf 'arg=%s\\n' \"$arg\"; done\n} > \"$OUT\"\n"
					if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
						t.Fatal(err)
					}
					rcPath, command := "", "command codex"
					if lookup == "rc_path" {
						rcPath = "export PATH=" + shellQuote(launcherBin) + ":\"$PATH\"\n"
					} else if lookup == "function_only" {
						command = "command " + shellQuote(program)
					}
					bashRC := rcPath + "export PERSONA_TEST_RC=from-bash\n" +
						"codex() { export PERSONA_TEST_FUNCTION=from-bash; " + command + " --from-bash \"$@\"; }\n"
					zshRC := rcPath + "export PERSONA_TEST_RC=from-zsh\n" +
						"codex() { export PERSONA_TEST_FUNCTION=from-zsh; " + command + " --from-zsh \"$@\"; }\n"
					if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(bashRC), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(zshRC), 0o600); err != nil {
						t.Fatal(err)
					}
					file := filepath.Join(root, "start.json")
					b, err := json.Marshal(codexStartFile{
						Argv:   []string{"codex", "--launcher-flag", "argument with spaces"},
						Prompt: prompt.value,
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, b, 0o600); err != nil {
						t.Fatal(err)
					}
					args, err := json.Marshal([]string{file})
					if err != nil {
						t.Fatal(err)
					}
					cmd := exec.Command(os.Args[0], "-test.run=^TestCodexStartHelper$")
					cmd.Dir = root
					cmd.Env = []string{
						"HOME=" + home,
						"ZDOTDIR=" + home,
						"SHELL=" + shell.path,
						"PATH=" + bin,
						"PWD=" + root,
						"OUT=" + out,
						"PERSONA_HOME=" + filepath.Join(home, "persona"),
						"PERSONA_AGENT_SANDBOX=off",
						"PERSONA_CODEX_START_HELPER=1",
						"PERSONA_CODEX_START_ARGS=" + string(args),
					}
					if b, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("CodexStart helper: %v: %s", err, b)
					}
					got, err := os.ReadFile(out)
					if err != nil {
						t.Fatal(err)
					}
					want := "RC=" + shell.marker + "\nFUNCTION=" + shell.marker + "\n"
					want += "arg=" + shell.flag + "\narg=--launcher-flag\narg=argument with spaces\n"
					if prompt.value != "" {
						want += "arg=--\narg=" + prompt.value + "\n"
					}
					if string(got) != want {
						t.Fatalf("launcher shell, arguments, or literal prompt differ: got %q, want %q", got, want)
					}
				})
			}
		}
	}
}

func TestCodexStartDefersLookupOnlyWithASupportedShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows launchers do not use a Unix shell")
	}
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit_path=%t", explicit), func(t *testing.T) {
			root := t.TempDir()
			bin := mkdir(t, filepath.Join(root, "empty-path"))
			out := filepath.Join(root, "out")
			program := filepath.Join(root, "codex")
			if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf executed > \"$OUT\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			command := "codex"
			if explicit {
				command = program
			}
			file := filepath.Join(root, "start.json")
			b, _ := json.Marshal(codexStartFile{Argv: []string{command}})
			if err := os.WriteFile(file, b, 0o600); err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal([]string{file})
			cmd := exec.Command(os.Args[0], "-test.run=^TestCodexStartHelper$")
			cmd.Dir = root
			cmd.Env = []string{
				"HOME=" + root, "ZDOTDIR=" + root, "SHELL=/bin/sh", "PATH=" + bin, "OUT=" + out,
				"PERSONA_HOME=" + filepath.Join(root, "persona"), "PERSONA_AGENT_SANDBOX=off",
				"PERSONA_CODEX_START_HELPER=1", "PERSONA_CODEX_START_ARGS=" + string(args),
			}
			output, err := cmd.CombinedOutput()
			if explicit {
				if err != nil {
					t.Fatalf("explicit launcher without a supported shell: %v: %s", err, output)
				}
				if got, err := os.ReadFile(out); err != nil || string(got) != "executed" {
					t.Fatalf("explicit launcher output %q: %v", got, err)
				}
			} else {
				if err == nil || !strings.Contains(string(output), "codex not found in PATH") {
					t.Fatalf("native lookup without a supported shell: %v: %s", err, output)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("a relative codex outside PATH was executed, or stat failed: %v", err)
				}
			}
		})
	}
}

func TestCodexStartExecKeepsExplicitAssignmentsOutOfArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows launchers do not use a Unix shell")
	}
	t.Setenv("PERSONA_AGENT_SANDBOX", "off")
	secretValue := "private value with spaces"
	secretAssignment := "CODEX_HOME=" + secretValue
	_, argv, env, _ := codexStartExec("/opt/bin/codex", []string{"codex", "--flag"}, nil, []string{secretAssignment})
	joinedArgv := strings.Join(argv, "\x00")
	if strings.Contains(joinedArgv, secretAssignment) || strings.Contains(joinedArgv, secretValue) {
		t.Fatalf("explicit launcher secret appeared in helper argv: %q", argv)
	}
	var carrier string
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(name, "__PERSONA_CODEX_START_") && value == secretAssignment {
			carrier = name
		}
	}
	if carrier == "" {
		t.Fatalf("explicit assignment was not preserved in an environment carrier")
	}
	if !slices.Contains(argv, carrier) {
		t.Fatalf("helper argv does not refer to its environment carrier %q: %q", carrier, argv)
	}
}

// codex-start's environment: the file's agent replaces a HERDR_AGENT the shell had (appended, the inherited one would
// come first, the one getenv and herdr read); no agent (tab-start) leaves HERDR_AGENT as it was. The launcher's own
// words go in after what the shell had, as before. `env` prints the environment exactly as it was exec'd.
func TestCodexStartReplacesAnInheritedHerdrAgent(t *testing.T) {
	envBin, err := exec.LookPath("env")
	if err != nil {
		t.Skip("no env(1)")
	}
	for _, tc := range []struct{ name, agent, want string }{
		{"codex", "codex", "HERDR_AGENT=codex"},
		{"tab-start", "", "HERDR_AGENT=inherited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "start.json")
			b, _ := json.Marshal(codexStartFile{Argv: []string{"CODEX_MARK=launcher", envBin}, Agent: tc.agent})
			if err := os.WriteFile(file, b, 0o600); err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal([]string{file})
			cmd := exec.Command(os.Args[0], "-test.run=^TestCodexStartHelper$")
			cmd.Env = append(os.Environ(), "HERDR_AGENT=inherited", "PERSONA_CODEX_START_HELPER=1", "PERSONA_CODEX_START_ARGS="+string(args))
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			var agents []string
			mark := false
			for _, kv := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(kv, "HERDR_AGENT=") {
					agents = append(agents, kv)
				}
				mark = mark || kv == "CODEX_MARK=launcher"
			}
			if !slices.Equal(agents, []string{tc.want}) || !mark {
				t.Errorf("environment has %q (launcher's variable: %v), want %s", agents, mark, tc.want)
			}
		})
	}
}

// Codex's launchers from ~/.zshrc: aliases whose one command is codex, with their words; one that does more is not.
func TestCodexAliasLaunchers(t *testing.T) {
	out := asNode(t, `alias cx='codex -m gpt-6 -c model_reasoning_effort=low'
alias cxr="CODEX_HOME=/tmp/codex-two codex --remote unix:///tmp/s.sock"
alias cxe='export OPENAI_BASE_URL=http://127.0.0.1:9; codex -c model="gpt-6-mini"'
alias cdcx='cd /tmp && codex'
alias dmcc='ANTHROPIC_BASE_URL=http://127.0.0.1:8 claude'
`, launcherScript)
	list, codex, _, _ := mergeLaunchers(out)
	got := map[string]proto.Launcher{}
	for _, l := range list {
		got[l.Name] = l
	}
	if got["cx"].Agent != "codex" || got["cx"].Model != "gpt-6" || got["cxe"].Model != "gpt-6-mini" || got["dmcc"].Agent != "" {
		t.Errorf("launchers %+v", list)
	}
	if _, ok := got["cdcx"]; ok {
		t.Error("an alias that does more than run codex became a launcher")
	}
	want := map[string][]string{
		"cx":  {"codex", "-m", "gpt-6", "-c", "model_reasoning_effort=low"},
		"cxr": {"CODEX_HOME=/tmp/codex-two", "codex", "--remote", "unix:///tmp/s.sock"},
		"cxe": {"OPENAI_BASE_URL=http://127.0.0.1:9", "codex", "-c", "model=gpt-6-mini"},
	}
	for name, w := range want {
		if strings.Join(codex[name], "\x00") != strings.Join(w, "\x00") {
			t.Errorf("%s runs as %q, want %q", name, codex[name], w)
		}
	}
	if codexOwnProcess(codex["cx"]) != "-c" || codexOwnProcess(codex["cxr"]) != "" || codexOwnProcess([]string{"codex", "-c", "a=1", "--remote", "unix:///s"}) != "" {
		t.Errorf("own process: %q %q", codexOwnProcess(codex["cx"]), codexOwnProcess(codex["cxr"]))
	}
	// Plain codex on the PATH is a launcher of its own.
	list, codex, _, _ = mergeLaunchers(launcherBegin + "\x1d\x1d/usr/local/bin/codex\n" + launcherEnd)
	if len(list) != 2 || list[1].Name != "codex" || list[1].Agent != "codex" || len(codex["codex"]) != 1 {
		t.Errorf("plain codex: %+v %v", list, codex)
	}
}
