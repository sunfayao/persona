//go:build !windows

package node

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// writeGateScript writes body as a bash script in a fresh directory and returns its path.
func writeGateScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gate.sh")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func resultText(r proto.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// killPidFileAtCleanup kills the pid the script writes to pidFile when the test ends, even one that failed before
// reading it.
func killPidFileAtCleanup(t *testing.T, pidFile string) {
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

// A script printing far more than the cap: only the cap is kept in memory, then the truncation notice.
func TestGateScriptOutputIsCappedWhileReading(t *testing.T) {
	script := writeGateScript(t, "head -c 50000000 /dev/zero\n")
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	res := runGateScript(context.Background(), "gate", script, nil, filepath.Dir(script), nil)
	runtime.ReadMemStats(&after)
	alloc := after.TotalAlloc - before.TotalAlloc
	text := resultText(res)
	t.Logf("output %d bytes, allocated %.1f MiB while running", len(text), float64(alloc)/(1<<20))
	if res.IsError {
		t.Fatalf("error result: %q", text[:min(len(text), 200)])
	}
	if !strings.HasPrefix(text, strings.Repeat("\x00", gateOutputCap)) || !strings.HasSuffix(text, gateOutputNotice) {
		t.Errorf("want the first %d bytes then the notice, got %d bytes", gateOutputCap, len(text))
	}
	if len(text) != gateOutputCap+len(gateOutputNotice) {
		t.Errorf("output %d bytes, want %d", len(text), gateOutputCap+len(gateOutputNotice))
	}
	if alloc > 8<<20 {
		t.Errorf("allocated %d bytes for a 50 MB output, want under 8 MiB", alloc)
	}
}

// Exit codes keep their meaning: a failing script is an error result carrying its output, a short one is not cut.
func TestGateScriptExitStatus(t *testing.T) {
	ok := runGateScript(context.Background(), "gate", writeGateScript(t, "echo \"$1\" \"$PERSONA_GATE_T\"\n"), []string{"hi"}, t.TempDir(), []string{"PERSONA_GATE_T=env"})
	if ok.IsError || resultText(ok) != "hi env\n" {
		t.Errorf("success: %q (error %v)", resultText(ok), ok.IsError)
	}
	bad := runGateScript(context.Background(), "gate", writeGateScript(t, "echo nope >&2\nexit 3\n"), nil, t.TempDir(), nil)
	if !bad.IsError || resultText(bad) != "nope\n" {
		t.Errorf("failure: %q (error %v)", resultText(bad), bad.IsError)
	}
	missing := runGateScript(context.Background(), "gate", writeGateScript(t, "true\n"), nil, filepath.Join(t.TempDir(), "no-such-dir"), nil)
	if !missing.IsError || !strings.HasPrefix(resultText(missing), "gate: ") {
		t.Errorf("start failure: %q (error %v)", resultText(missing), missing.IsError)
	}
}
