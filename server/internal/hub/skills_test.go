package hub

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
)

// profileManagerStore keeps only the roster and settings needed to select a profile.
type profileManagerStore struct {
	managerStore
	settings settingsStore
	ids      map[string]bool
}

func (s *profileManagerStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	return s.settings.Setting(ctx, key, out)
}

func (s *profileManagerStore) PutSetting(ctx context.Context, key string, value any) error {
	return s.settings.PutSetting(ctx, key, value)
}

func (s *profileManagerStore) BusIdentityByID(_ context.Context, id string) (*db.BusIdentity, error) {
	if !s.ids[id] {
		return nil, nil
	}
	return &db.BusIdentity{ID: id, Role: bus.RoleManager}, nil
}

func newProfileTestHub(t *testing.T, ids ...string) (*Hub, *profileManagerStore) {
	t.Helper()
	h := newTestHub(t)
	s := &profileManagerStore{settings: settingsStore{vals: map[string][]byte{}}, ids: map[string]bool{assistantwire.AssistantID: true}}
	for _, id := range ids {
		s.ids[id] = true
	}
	h.bus.idents = s
	return h, s
}
