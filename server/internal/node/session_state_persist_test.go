package node

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/rpc"
)

type stateRPCBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *stateRPCBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *stateRPCBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes())
}

// This uses the production Node.persistState and rpc.Peer.Notify path. The old record is still being JSON-encoded
// when the smaller switch record reaches the writer semaphore; the semaphore alone does not restore mutation order.
func TestStatePersistenceOrdersActualRPCNotifications(t *testing.T) {
	wire := new(stateRPCBuffer)
	n := &Node{state: newState()}
	n.peer = rpc.New("n", wire)
	n.state.persist = n.persistState
	const id = "00000000-0000-4000-8000-000000000001"
	n.state.Sessions[id] = sessionState{Launcher: "initial", Cwd: "/tmp", StartedAt: time.Now()}
	old := sessionState{
		Launcher:       "claude",
		Model:          "claude-sonnet-5-5",
		Cwd:            "/tmp",
		StartedAt:      time.Now(),
		PendingHandoff: &pendingHandoff{Text: strings.Repeat("handoff", 4<<20)},
	}
	oldDone := make(chan struct{})
	go func() {
		n.state.put(id, old)
		close(oldDone)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		current, _ := n.state.get(id)
		if current.Launcher == "claude" {
			break // put committed its old snapshot and released state.mu
		}
		if time.Now().After(deadline) {
			t.Fatal("the older put did not commit its in-memory snapshot")
		}
		runtime.Gosched()
	}
	n.state.adoptSwitch(id, sessionState{
		Launcher:  "codex",
		Model:     "gpt-5.3-codex",
		Cwd:       "/private/tmp",
		StartedAt: old.StartedAt.Add(time.Minute),
	})
	<-oldDone
	current, _ := n.state.get(id)
	if current.Launcher != "codex" {
		t.Fatalf("in-memory launcher = %q, want codex", current.Launcher)
	}
	var launchers []string
	for _, line := range bytes.Split(bytes.TrimSpace(wire.Bytes()), []byte("\n")) {
		var event struct {
			Event  string `json:"event"`
			Params struct {
				State sessionState `json:"state"`
			} `json:"params"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode RPC line: %v", err)
		}
		if event.Event == "state.put" {
			launchers = append(launchers, event.Params.State.Launcher)
		}
	}
	if len(launchers) != 2 {
		t.Fatalf("state.put count = %d, want 2", len(launchers))
	}
	if launchers[1] != current.Launcher {
		t.Fatalf("wire order %v leaves durable launcher %q; memory has %q", launchers, launchers[1], current.Launcher)
	}
}
