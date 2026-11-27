package node

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/nodeclient"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// approvalPeer uses a real RPC transport; disconnecting it is not a refusal.
func approvalPeer(t *testing.T, n *Node) (<-chan proto.Interaction, chan<- proto.Resolution, func()) {
	t.Helper()
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	n.peer = rpc.New("node", a)
	hub := rpc.New("hub", b)
	asked := make(chan proto.Interaction, 8)
	answers := make(chan proto.Resolution, 1)
	hub.OnRequest = func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method != "interaction.create" {
			return map[string]any{}, nil
		}
		var req struct {
			Interaction proto.Interaction `json:"interaction"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		asked <- req.Interaction
		select {
		case answer := <-answers:
			return answer, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	peer := n.peer
	go func() { _ = peer.Serve(ctx, a) }()
	go func() { _ = hub.Serve(ctx, b) }()
	closePeer := func() { cancel(); _ = a.Close(); _ = b.Close() }
	t.Cleanup(closePeer)
	return asked, answers, closePeer
}

func receiveApproval(t *testing.T, ch <-chan proto.Interaction) proto.Interaction {
	t.Helper()
	select {
	case ix := <-ch:
		return ix
	case <-time.After(3 * time.Second):
		t.Fatal("approval not received")
		return proto.Interaction{}
	}
}

func TestCodexDisconnectedApprovalStopsWaiting(t *testing.T) {
	oldRetry, oldWait := nodeclient.ResumeRetry, nodeclient.ResumeWait
	nodeclient.ResumeRetry, nodeclient.ResumeWait = time.Millisecond, 30*time.Millisecond
	t.Cleanup(func() { nodeclient.ResumeRetry, nodeclient.ResumeWait = oldRetry, oldWait })
	for _, reason := range []string{"stop", "expiry", "reconnect-timeout"} {
		t.Run(reason, func(t *testing.T) {
			n := codexTestNode(t)
			n.keeper = newAssistantKeeper()
			p := &assistantProc{exited: make(chan struct{})}
			c := &codexConn{n: n, p: p}
			asked, _, disconnect := approvalPeer(t, n)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			expiry := time.Now().Add(time.Hour)
			if reason == "expiry" {
				expiry = time.Now().Add(20 * time.Millisecond)
			}
			done := make(chan map[string]any, 1)
			go func() {
				done <- c.askPhone(ctx, proto.Interaction{ID: "approval", ExpiresAt: expiry}, "accept", "acceptForSession", "decline")
			}()
			receiveApproval(t, asked)
			disconnect()
			if reason == "stop" {
				cancel()
			}
			select {
			case out := <-done:
				if out["decision"] != "cancel" {
					t.Fatalf("decision: %v", out)
				}
			case <-time.After(time.Second):
				t.Fatal("approval still waiting")
			}
		})
	}
}

func TestCodexReportsToAdoptedNode(t *testing.T) {
	a, b := codexTestNode(t), codexTestNode(t)
	oldHub, newHub := withFakeHub(t, a), withFakeHub(t, b)
	p, c, _ := newFakeCodex(t, a, t.TempDir())
	p.id = testThread
	p.owner.Store(b)
	c.notification("item/agentMessage/delta", json.RawMessage(`{"delta":"continued"}`))
	waitFor(t, "delta on new connection", func() bool {
		newHub.mu.Lock()
		defer newHub.mu.Unlock()
		return len(newHub.deltas) == 1 && newHub.deltas[0] == "continued"
	})
	oldHub.mu.Lock()
	defer oldHub.mu.Unlock()
	if len(oldHub.deltas) != 0 {
		t.Fatal("delta reached the disconnected owner")
	}
}
