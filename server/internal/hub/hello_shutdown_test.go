package hub

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// shutdownOrderStore records the production cleanup order. Closing saved also releases the finite node worker.
type shutdownOrderStore struct {
	*helloStore
	hub                 *Hub
	mu                  sync.Mutex
	events              []string
	helloStoppedAtClose bool
	saved               chan struct{}
	saveOnce            sync.Once
}

func newShutdownOrderStore() *shutdownOrderStore {
	base := newHelloStore()
	base.release()
	return &shutdownOrderStore{helloStore: base, saved: make(chan struct{})}
}

func (s *shutdownOrderStore) CloseContext(context.Context) error {
	s.hub.helloWrites.mu.Lock()
	stopped := s.hub.helloWrites.stopped
	s.hub.helloWrites.mu.Unlock()
	s.mu.Lock()
	s.helloStoppedAtClose = stopped
	s.mu.Unlock()
	s.record("database closed")
	return nil
}

func (s *shutdownOrderStore) PutSetting(ctx context.Context, key string, value any) error {
	err := s.helloStore.PutSetting(ctx, key, value)
	if key == userLocaleSetting && err == nil {
		s.record("hello saved")
		s.releaseNode()
	}
	return err
}

func (s *shutdownOrderStore) releaseNode() { s.saveOnce.Do(func() { close(s.saved) }) }

func (s *shutdownOrderStore) record(event string) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func (s *shutdownOrderStore) snapshot() ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...), s.helloStoppedAtClose
}

// Drive runHub's named cleanup itself. The cancel seam represents a connection that is still open after deploy
// cancellation; the real sequence must save its report before the finite-worker join and before closing the Store.
func TestHubCleanupDrainsHelloBeforeNodeJoinAndDatabaseClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deployCtx, cancelDeploy := context.WithCancel(context.Background())
		t.Cleanup(cancelDeploy)
		h := newTestHubWithDeployCtx(t, deployCtx)
		h.assistantOf(assistantwire.AssistantID).updating.Store(true)
		budget := newShutdownBudget(h.cfg)
		h.shutdown = budget
		t.Cleanup(budget.close)
		store := newShutdownOrderStore()
		store.hub = h
		h.store = store
		t.Cleanup(func() {
			store.releaseNode()
			h.nodeWorkers.Wait()
		})
		h.nodeWorkers.Add(1)
		go func() {
			defer h.nodeWorkers.Done()
			<-store.saved
			store.record("node joined")
		}()

		cancel := func() {
			cancelDeploy()
			c := &client{hub: h, device: "dev_cleanup", role: roleFull, out: make(chan []byte, 16)}
			h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`))
		}
		h.closeDatabaseAfterWorkers(cancel, store.CloseContext)
		got, stoppedAtClose := store.snapshot()
		if want := []string{"hello saved", "node joined", "database closed"}; !slices.Equal(got, want) {
			t.Errorf("hub cleanup order = %v, want %v", got, want)
		}
		if !stoppedAtClose {
			t.Error("the database closed before the hello-writer shutdown step")
		}
	})
}
