//go:build unix

package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/sunfayao/persona/server/internal/osproc/osproctest"
	"github.com/sunfayao/persona/server/internal/update"
)

// listenHelperEnv runs the test binary as a hub that systemd started with one socket at descriptor 3 (the variables
// but LISTEN_PID come from the test; it sets that one to itself, as systemd does). The value is what it does before
// it starts a child that reports what it inherited (osproctest): "run" (Run), "serve" (Serve), "exec"
// (execKeepingListeners on a program that is not there) or "none" (nothing: the control).
const listenHelperEnv = "PERSONA_TEST_LISTEN_HELPER"

const listenHelperPrefix = "listen-helper: "

type listenHelperResult struct {
	Err    string            `json:"err"`    // what Run, Serve or the exec returned
	Sealed bool              `json:"sealed"` // descriptor 3 is close-on-exec here afterwards
	Edge   bool              `json:"edge"`   // "serve": the edge was on (edgeOff said nothing)
	Child  osproctest.Report `json:"child"`
}

// listenHelpers is TestMain's first step: it returns unless the binary was started as a reporter or a helper, and
// exits in those cases.
func listenHelpers() {
	osproctest.ReportIfAsked()
	mode := os.Getenv(listenHelperEnv)
	if mode == "" {
		return
	}
	_ = os.Unsetenv(listenHelperEnv)
	_ = os.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	var res listenHelperResult
	errText := func(err error) string {
		if err == nil {
			return "<nil>"
		}
		return err.Error()
	}
	switch mode {
	case "run":
		res.Err = errText(Run(context.Background(), update.Build{Version: "test"}))
	case "serve":
		res.Edge = edgeOff(update.Build{Version: "test"}) == ""
		res.Err = errText(Serve(context.Background(), update.Build{Version: "test"}))
	case "exec":
		// A hub's updater execs only after Run, Serve or RunSlot claimed what the process was handed; taking only
		// the listeners here handed the rest on to the child.
		claimInherited()
		res.Err = errText(execKeepingListeners(filepath.Join(os.TempDir(), "persona-no-such-program"), []string{"x"}, os.Environ()))
	case "none":
	default:
		fmt.Fprintf(os.Stderr, "unknown %s %q\n", listenHelperEnv, mode)
		os.Exit(2)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, 3, syscall.F_GETFD, 0)
	res.Sealed = errno == 0 && flags&syscall.FD_CLOEXEC != 0
	child, err := osproctest.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	res.Child = child
	b, _ := json.Marshal(res)
	fmt.Printf("%s%s\n", listenHelperPrefix, b)
	os.Exit(0)
}
