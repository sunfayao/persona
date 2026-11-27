//go:build linux

package nodecmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// procsEntryEnv runs the test binary as the command in its value (words split on spaces) with stdin closed, then
// prints GOMAXPROCS before and after it.
const procsEntryEnv = "PERSONA_TEST_PROCS_ENTRY"

func procsEntry(args string) int {
	before := runtime.GOMAXPROCS(0)
	if _, err := Run(context.Background(), func() {}, strings.Fields(args), Build{Version: "test"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("\nprocs %d %d\n", before, runtime.GOMAXPROCS(0))
	return 0
}
