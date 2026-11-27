package hub

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistant"
	"github.com/sunfayao/persona/server/internal/proto"
)

// hubAgentDirs keeps hidden Agent folders for the assistant the tests connect, as the devices do: per machine, the
// assistant told after each change.
type hubAgentDirs struct {
	mu      sync.Mutex
	rules   map[string][]proto.HiddenAgentDir
	changed func(machine string)
}

func (d *hubAgentDirs) HiddenAgentDirs(_ context.Context, id string) ([]proto.HiddenAgentDir, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.rules[id]), "", nil
}

func (d *hubAgentDirs) EditHiddenAgentDirs(_ context.Context, id string, edit func([]proto.HiddenAgentDir) ([]proto.HiddenAgentDir, error)) ([]proto.HiddenAgentDir, error) {
	d.mu.Lock()
	next, err := edit(slices.Clone(d.rules[id]))
	if err == nil {
		d.rules[id] = slices.Clone(next)
	}
	changed := d.changed
	d.mu.Unlock()
	if err == nil && changed != nil {
		changed(id)
	}
	return next, err
}

// testAgentDirs are the hidden folders the tests' assistants read, by hub (dialAssistant passes them on).
var testAgentDirs sync.Map

func testAgentDirsOf(h *Hub) assistant.AgentDirs {
	if d, ok := testAgentDirs.Load(h); ok {
		return d.(*hubAgentDirs)
	}
	return nil
}

// withAgentDirs gives the assistant connected to h from now on hidden Agent folders the test changes.
func withAgentDirs(t *testing.T, h *Hub) *hubAgentDirs {
	t.Helper()
	d := &hubAgentDirs{rules: map[string][]proto.HiddenAgentDir{}}
	testAgentDirs.Store(h, d)
	t.Cleanup(func() { testAgentDirs.Delete(h) })
	return d
}
