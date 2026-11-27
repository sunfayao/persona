package nodecmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGateManagerUnavailableFallsBackExplicitly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell transport fixture")
	}
	old := resourceCall
	t.Cleanup(func() { resourceCall = old })
	resourceCall = func(context.Context, string, any, any) error { return errors.New("unreachable") }
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PERSONA_GATE_SSH_TARGET", "runner.example.com")
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nprintf 'RESULT=example-run\\nEXIT_CODE=7\\n'\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	code := gateCommand(context.Background(), []string{"example", strings.Repeat("a", 40), "web"}, strings.NewReader(""), &out, &errout)
	if code != 7 || !strings.Contains(errout.String(), "DEGRADED") || !strings.Contains(out.String(), "EXIT_CODE=7") {
		t.Fatalf("%d %q %q", code, out.String(), errout.String())
	}
}
func TestGateCaptureRetainsResultBeforeLargeLogs(t *testing.T) {
	var capture gateCapture
	_, _ = capture.Write([]byte("RES"))
	_, _ = capture.Write([]byte("ULT=example-run\n"))
	_, _ = capture.Write([]byte(strings.Repeat("x", 300<<10)))
	if capture.result() != "example-run" || len(capture.data) > 256<<10 {
		t.Fatalf("result=%q length=%d", capture.result(), len(capture.data))
	}
}
