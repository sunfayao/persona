package assistant

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFailedStartSettingsWordingKeepsWordBoundaries(t *testing.T) {
	s, _ := newQuietService(t)
	want := map[string]string{
		"assistant.main_supervises_own_tasks": "failed starts with no current leader and of what",
	}
	for _, setting := range s.ownSettings() {
		fragment, ok := want[setting.Key]
		if !ok {
			continue
		}
		if !strings.Contains(setting.About, fragment) {
			t.Errorf("%s lost a word boundary in %q", setting.Key, setting.About)
		}
		if strings.Contains(setting.About, "themain") || strings.Contains(setting.About, "ofwhat") {
			t.Errorf("%s joined adjacent words: %q", setting.Key, setting.About)
		}
		delete(want, setting.Key)
	}
	for key := range want {
		t.Errorf("setting %s was not found", key)
	}
}

type failedBatchCASStore struct {
	Store
	compareMu     sync.Mutex
	beforeCompare func()
}

func (st *failedBatchCASStore) CompareSetting(ctx context.Context, key string, revision int64, value any) (bool, error) {
	if st.beforeCompare != nil {
		st.beforeCompare()
	}
	st.compareMu.Lock()
	defer st.compareMu.Unlock()
	var current failedStartBatchDoc
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

type failedBatchRelayFailStore struct {
	Store
	mu   sync.Mutex
	fail bool
}

func (st *failedBatchRelayFailStore) PutSetting(ctx context.Context, key string, value any) error {
	st.mu.Lock()
	fail := st.fail && key == relayKey
	st.mu.Unlock()
	if fail {
		return errors.New("synthetic relay persistence failure")
	}
	return st.Store.PutSetting(ctx, key, value)
}

func (st *failedBatchRelayFailStore) setFail(fail bool) {
	st.mu.Lock()
	st.fail = fail
	st.mu.Unlock()
}
