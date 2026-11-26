package hub

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/nwverse/persona/server/internal/db"
)

// deviceRows records the device rows the pusher writes (the rest of Store is unused).
type deviceRows struct {
	sharedStore
	mu      sync.Mutex
	rows    map[string]db.Device
	deletes int
}

func (s *deviceRows) SetDevicePrefs(_ context.Context, id, _ string, prefs json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.rows[id]
	d.DeviceID, d.Prefs = id, prefs
	s.rows[id] = d
	return nil
}

func (s *deviceRows) DeleteDevice(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	s.deletes++
	return nil
}

func (s *deviceRows) row(id string) (db.Device, bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.rows[id]
	return d, ok, s.deletes
}
