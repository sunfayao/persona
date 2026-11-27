package node

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// cancelHub is a hub that never answers a card and notes the cards the node withdraws.
type cancelHub struct {
	mu        sync.Mutex
	asked     []string
	toolIDs   []string
	cancelled []string
}

func (h *cancelHub) counts() (asked, cancelled int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.asked), len(h.cancelled)
}

func (h *cancelHub) toolUseIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.toolIDs...)
}

func withCancelHub(t testing.TB, n *Node) *cancelHub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	h := &cancelHub{}
	hub.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != "interaction.create" {
			return map[string]any{}, nil
		}
		var p struct {
			Interaction proto.Interaction `json:"interaction"`
		}
		_ = json.Unmarshal(params, &p)
		h.mu.Lock()
		h.asked = append(h.asked, p.Interaction.ID)
		h.toolIDs = append(h.toolIDs, p.Interaction.ToolUseID)
		h.mu.Unlock()
		<-ctx.Done() // nobody on the phone answers
		return nil, ctx.Err()
	}
	hub.OnEvent = func(event string, params json.RawMessage) {
		if event != "interaction.cancel" {
			return
		}
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(params, &p)
		h.mu.Lock()
		h.cancelled = append(h.cancelled, p.ID)
		h.mu.Unlock()
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	peer := n.peer
	go func() { _ = peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return h
}

// fakeAgent says the claude 4242 started "linux:7" lives until dead is set.
func fakeAgent(t *testing.T) *atomic.Bool {
	t.Helper()
	var dead atomic.Bool
	oldGone, oldWatch := agentGone, agentWatch
	agentGone = func(pid int, start string) bool { return pid != 4242 || start != "linux:7" || dead.Load() }
	agentWatch = 10 * time.Millisecond
	t.Cleanup(func() { agentGone, agentWatch = oldGone, oldWatch })
	return &dead
}

// runHookAsync runs a hook in the background; its result arrives on the channel. The test waits for it before its
// cleanups put the package variables back.
func runHookAsync(t *testing.T, n *Node, in map[string]any) <-chan any {
	t.Helper()
	raw, _ := json.Marshal(in)
	done := make(chan any, 1)
	finished := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(finished)
		out, _ := n.handleHook(ctx, sockRequest{Kind: "hook", Input: raw, Attempt: "a1", AgentPID: 4242, AgentStart: "linux:7"})
		done <- out
	}()
	t.Cleanup(func() { cancel(); <-finished })
	return done
}

// watchFor runs watchAgent on req for 30 looks with gone deciding each look; true: it ended the wait on its own.
func watchFor(t *testing.T, req sockRequest, gone func(int, string) bool) bool {
	oldGone, oldWatch := agentGone, agentWatch
	agentGone, agentWatch = gone, 5*time.Millisecond
	defer func() { agentGone, agentWatch = oldGone, oldWatch }()
	ctx, stop := codexTestNode(t).watchAgent(context.Background(), req)
	time.Sleep(30 * agentWatch)
	ended := ctx.Err() != nil // before stop, which ends ctx itself
	stop()
	return ended
}
