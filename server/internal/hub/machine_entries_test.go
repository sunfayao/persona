package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The owners of the tests below: lap is m-0a0a0a's, every other machine the carrier's.
func lapMovedOwners() *ownersStore {
	return &ownersStore{kept: map[string]json.RawMessage{machineOwnersKey: json.RawMessage(`{"lap":"m-0a0a0a"}`)}}
}

func keptEntries(t *testing.T, st Store) reportedMachines {
	t.Helper()
	var r reportedMachines
	if _, err := st.Setting(context.Background(), machineEntriesKey, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// entriesFailStore fails the first reads of what the managers reported.
type entriesFailStore struct {
	*meshStore
	mu    sync.Mutex
	fails int
	reads int
}

func (s *entriesFailStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == machineEntriesKey {
		s.mu.Lock()
		s.reads++
		fail := s.fails > 0
		if fail {
			s.fails--
		}
		s.mu.Unlock()
		if fail {
			return false, errors.New("database restarting")
		}
	}
	return s.meshStore.Setting(ctx, key, out)
}

func (s *entriesFailStore) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// The warm start ends for every manager's machines: one manager back does not keep another's shown as before.
func TestWarmStartEndsForEveryManager(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	st := newMeshStore()
	h.store = st
	h.bus.idents = lapMovedOwners()
	h.cfg.Machines = []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}, {ID: "lap", Name: "Lap", Kind: "ssh"}}
	_ = st.PutSetting(ctx, devicesWarmKey, map[string]any{"saved_at": time.Now(), "machines": map[string]any{
		"box": map[string]any{"info": proto.NodeInfo{Hostname: "box"}, "sessions": []proto.Session{{SessionID: "s1", Status: proto.StatusIdle}}},
		"lap": map[string]any{"info": proto.NodeInfo{Hostname: "lap"}, "sessions": []proto.Session{{SessionID: "s2", Status: proto.StatusIdle}}},
	}})
	h.startDevices(ctx)
	lap := h.machineByID("lap")
	if !lap.snapshot().Online || lap.ownerOrCarrier() != "m-0a0a0a" {
		t.Fatalf("lap before the managers: %+v", lap.snapshot())
	}
	h.applyDeviceStatesFrom(devicesOwner, devicewire.States{Machines: []devicewire.State{{
		Machine: proto.Machine{ID: "box", Name: "Box", Kind: "ssh", Online: true}, Connected: true, ConnectedAt: time.Now()}}})
	h.devicesWarmEnded()
	if lap.snapshot().Online {
		t.Fatal("m-0a0a0a's machine still shown as before the restart")
	}
	if ss := lap.sessionList(); len(ss) != 1 || ss[0].Status != proto.StatusUnknown {
		t.Fatalf("its sessions: %+v", ss)
	}
	if !h.machineByID("box").snapshot().Online {
		t.Fatal("the carrier's machine went offline with it")
	}
}
