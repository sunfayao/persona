package devices

import (
	"encoding/json"
	"fmt"
	"testing"
)

func seqOf(m *Machine, id string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seq, ok := m.lastSeq[id]
	return seq, ok
}

// A session nobody watches any more keeps no last seq: on a long-running manager every session an app ever opened
// would stay otherwise, per machine. A batch that comes in late, after the unwatch, does not bring it back.
func TestUnwatchedSessionsForgetTheirSeq(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "box", Name: "box"})
	for i := range 100 {
		id := json.RawMessage(fmt.Sprintf(`{"session_id":"s%d"}`, i))
		items := json.RawMessage(fmt.Sprintf(`{"session_id":"s%d","items":[{"seq":3}]}`, i))
		m.noteWatch("session.watch", id)
		m.onEvent("session.items", items)
		if seq, _ := seqOf(m, fmt.Sprintf("s%d", i)); seq != 3 {
			t.Fatalf("s%d while watched: seq %d, want 3", i, seq)
		}
		m.noteWatch("session.unwatch", id)
		m.onEvent("session.items", items) // late, after the unwatch
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.lastSeq) != 0 {
		t.Fatalf("%d seqs kept for unwatched sessions", len(m.lastSeq))
	}
}
