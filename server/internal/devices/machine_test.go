package devices

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/sunfayao/persona/server/internal/rpc"
)

// TestFakeNode is not a test: run as a subprocess (PERSONA_FAKE_NODE=1) it is a node on stdio answering every call.
func TestFakeNode(t *testing.T) {
	if os.Getenv("PERSONA_FAKE_NODE") != "1" {
		t.Skip("helper process")
	}
	p := rpc.New("n", os.Stdout)
	p.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return map[string]any{"pid": os.Getpid()}, nil
	}
	_ = p.Serve(context.Background(), os.Stdin)
	os.Exit(0)
}
