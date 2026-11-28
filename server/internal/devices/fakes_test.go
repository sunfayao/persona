package devices

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// settingsStore keeps settings in memory like the settings table.
type settingsStore struct {
	Store
	mu   sync.Mutex
	vals map[string][]byte
	puts int
}

func (s *settingsStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (s *settingsStore) PutSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[key] = b
	s.puts++
	return nil
}

func (s *settingsStore) TakeSetting(ctx context.Context, key string, out any) (bool, error) {
	return s.Setting(ctx, key, out)
}

// recordingNode is a node answering the manager over pipes, recording the methods it was called with.
type recordingNode struct {
	mu    sync.Mutex
	calls []string
}

func (f *recordingNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, method+" "+string(params))
	f.mu.Unlock()
	if method == "sessions.list" {
		return map[string]any{"sessions": []proto.Session{}}, nil
	}
	return map[string]any{}, nil
}

func (f *recordingNode) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// machineDevices is a manager with settings in memory and one machine, m1.
func machineDevices(t *testing.T) (*Devices, *settingsStore, *Machine) {
	t.Helper()
	h := newTestDevices(t)
	st := &settingsStore{vals: map[string][]byte{}}
	h.opts.Store = st
	m := newMachine(h, MachineConfig{ID: "m1", Name: "box"})
	h.addEntry(m)
	return h, st, m
}

// connectNode gives m a node connection.
func connectNode(t *testing.T, m *Machine) (*recordingNode, *rpc.Peer) {
	t.Helper()
	f := &recordingNode{}
	toNode, nodeIn := io.Pipe()
	toHub, hubIn := io.Pipe()
	hubPeer := rpc.New("h", nodeIn)
	nodePeer := rpc.New("n", hubIn)
	nodePeer.OnRequest = f.handle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); toNode.Close(); toHub.Close() })
	go hubPeer.Serve(ctx, toHub)
	go nodePeer.Serve(ctx, toNode)
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.info.Engines = []proto.Engine{{ID: "herdr", Name: "herdr", Available: true, Version: "0.9.3"}}
	m.mu.Unlock()
	return f, hubPeer
}
