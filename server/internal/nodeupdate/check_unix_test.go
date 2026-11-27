//go:build unix

package nodeupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// scannedCheckFixture is a node-update-check that, like a file under endpoint protection that scans new executables,
// is slow (delay) only on its first run and answers at once after it. With "$0.hang" present it never answers.
func scannedCheckFixture(t *testing.T, dir, delay string) string {
	t.Helper()
	path := filepath.Join(dir, "persona-node")
	script := "#!/bin/sh\nif [ ! -e \"$0.scanned\" ]; then sleep " + delay + "; : >\"$0.scanned\"; fi\n" +
		"if [ -e \"$0.hang\" ]; then exec sleep 30; fi\n" +
		`printf '%s\n' '{"version":"next","os":"` + runtime.GOOS + `","arch":"` + runtime.GOARCH + `","program":"persona-node"}'` + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func withCheckLimits(t *testing.T, limits CheckLimits) {
	t.Helper()
	old := checkLimits
	checkLimits = limits
	t.Cleanup(func() { checkLimits = old })
}

func TestSelfCheckRepeatKeepsShortLimit(t *testing.T) {
	withCheckLimits(t, CheckLimits{FirstRun: 5 * time.Second, Repeat: 200 * time.Millisecond})
	path := scannedCheckFixture(t, t.TempDir(), "0")
	if _, err := runCheck(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	// The file that passed now hangs: the repeat limit, not the first-run one, ends it.
	if err := os.WriteFile(path+".hang", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := runCheck(t.Context(), path)
	if err == nil || !strings.Contains(err.Error(), "node-update-check timed out after 200ms (this file passed before)") {
		t.Fatalf("repeat hang: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("repeat hang waited %s", elapsed)
	}
}
