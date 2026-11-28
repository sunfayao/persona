package node

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// The node's tests never reach what runs on the machine that runs them: its botmux, gh, Codex, Claude config and
// Persona state stay out of reach unless a test gives its own.

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
	// Nor the user's botmux: a node registers no botmux host unless a test gives it one.
	botmuxBin = func() string { return "" }
	// Nor the user's gh outside the test's PATH (ghpath.go).
	ghFallbackDirs = func() []string { return nil }
	// Nor the user's Codex catalog: a model recovery on a Codex list runs no `codex debug models` (a test that wants
	// windows gives its own, assistant_fallback_window.go).
	codexDebugModels = func(context.Context, *Node, string) ([]byte, error) { return nil, errors.New("no codex in tests") }
	// Subscription usage tests provide their own app-server instead of polling a real account.
	codexUsageCLI = func() (string, error) { return "", errors.New("no codex in tests") }
	// Nor the ChatGPT app's bundled codex: a test that wants one sets its own (codex_bundled_test.go).
	bundledCodexCLI = func() string { return "" }
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
