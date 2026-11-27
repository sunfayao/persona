package assistant

import (
	"context"
	"sync"
)

// delayedWaitTestStore adds revision CAS only to the durable-wait fixtures. The shared memStore deliberately keeps
// its older, PutSetting-only capabilities because other APIs use those capabilities to reject unsupported writes.
type delayedWaitTestStore struct {
	Store
	mu sync.Mutex
}

func (st *delayedWaitTestStore) CompareSetting(ctx context.Context, key string, revision int64, value any) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var current delayedWaitDoc
	if _, err := st.Store.Setting(ctx, key, &current); err != nil {
		return false, err
	}
	if current.Revision != revision {
		return false, nil
	}
	if err := st.Store.PutSetting(ctx, key, value); err != nil {
		return false, err
	}
	return true, nil
}

func withDelayedWaitCAS(st Store) Store { return &delayedWaitTestStore{Store: st} }

// copyReceiptFixtureTasks mirrors the task/link snapshot that PostgreSQL reloads. planMemStore has no to-do tables;
// the new manager's receipt and turn-ledger memory deliberately stays separate.
func copyReceiptFixtureTasks(first, second *Service) {
	first.todos.mu.RLock()
	defer first.todos.mu.RUnlock()
	second.todos.mu.Lock()
	defer second.todos.mu.Unlock()
	second.todos.todos = map[string]*Todo{}
	for id, todo := range first.todos.todos {
		copy := copyTodoLocked(todo)
		second.todos.todos[id] = &copy
	}
	second.todos.activeBySession = map[[2]string]string{}
	for ref, id := range first.todos.activeBySession {
		second.todos.activeBySession[ref] = id
	}
	second.todos.spacesByKey = map[string]string{}
	for ref, id := range first.todos.spacesByKey {
		second.todos.spacesByKey[ref] = id
	}
	second.todos.loaded = true
}
