//go:build !windows

package devices

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/rpc"
)

// TestFakeStoppingNode is not a test: run as a subprocess (PERSONA_FAKE_STOPPING_NODE=<file>) it is a node on stdio
// that, once its stdin ends, takes a moment (stopping its conversations) and writes the file before it exits; with
// PERSONA_FAKE_NODE_HANG=1 it never exits on its own.
func TestFakeStoppingNode(t *testing.T) {
	mark := os.Getenv("PERSONA_FAKE_STOPPING_NODE")
	if mark == "" {
		t.Skip("helper process")
	}
	p := rpc.New("n", os.Stdout)
	p.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return map[string]any{"pid": os.Getpid()}, nil
	}
	_ = p.Serve(context.Background(), os.Stdin)
	if os.Getenv("PERSONA_FAKE_NODE_HANG") == "1" {
		select {} // hangs until the node kills it
	}
	time.Sleep(300 * time.Millisecond) // sleep-ok: the fake node takes a moment to exit, which the test measures
	_ = os.WriteFile(mark, []byte("stopped"), 0o600)
	os.Exit(0)
}
