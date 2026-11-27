package devices

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// A real subprocess keeps stdout open until the hub terminates its transport.
func TestMachinePeerHelper(t *testing.T) {
	if os.Getenv("PERSONA_TEST_MACHINE_PEER") != "1" {
		return
	}
	peer := rpc.New("n", os.Stdout)
	peer.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		switch method {
		case "node.info":
			return proto.NodeInfo{Hostname: "test-node"}, nil
		case "sessions.list":
			return map[string]any{"sessions": []proto.Session{}}, nil
		}
		return map[string]any{}, nil
	}
	_ = peer.Serve(context.Background(), os.Stdin)
	os.Exit(0)
}
