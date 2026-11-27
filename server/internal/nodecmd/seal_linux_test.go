package nodecmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
)

// TestMain lets the test binary be the node of TestNodeSealsInherited (nodeEntryEnv) and the commands of
// TestNodeCapsProcs (procsEntryEnv).
func TestMain(m *testing.M) {
	if arg := os.Getenv(nodeEntryEnv); arg != "" {
		os.Exit(nodeEntry(arg))
	}
	if args := os.Getenv(procsEntryEnv); args != "" {
		os.Exit(procsEntry(args))
	}
	os.Exit(m.Run())
}

// nodeEntryEnv runs the test binary as `node <value>` (--stdio, --attach), or, with "hold", as a process that only
// holds what it was handed until stdin closes.
const nodeEntryEnv = "PERSONA_TEST_NODE_ENTRY"

func nodeEntry(arg string) int {
	if arg == "hold" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	}
	if _, err := Run(context.Background(), func() {}, []string{"node", arg}, Build{Version: "test"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
