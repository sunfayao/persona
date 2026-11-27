package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// imessageNode is a Mac's node over the real RPC pipe, answering connector.* from answer (method → result or error).
func imessageNode(t *testing.T, h *Manager, answer func(method string, params json.RawMessage) (any, error)) (*Machine, *[]string) {
	t.Helper()
	builtin := fakeNodesBuiltin
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide := rpc.New("h", h2nW)
	nodeSide := rpc.New("n", n2hW)
	var mu sync.Mutex
	calls := &[]string{}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		method, params = fakeBuiltinRoute(t, builtin, method, params) // either way the hub asks, the same answers (#1830 step 6)
		mu.Lock()
		*calls = append(*calls, method)
		mu.Unlock()
		return answer(method, params)
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: "mac", Name: "示例 Mac", Kind: "ssh"})
	m.peer = hubSide
	m.info = proto.NodeInfo{OS: "darwin", BuiltinPrograms: fakeBuiltinPrograms(builtin)}
	h.byID["mac"] = m
	h.machines = append(h.machines, m)
	return m, calls
}
