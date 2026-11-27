package hub

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// failingMutes is a store whose reads fail while fail is set.
type failingMutes struct {
	settingsStore
	fail bool
}

func (s *failingMutes) Setting(ctx context.Context, key string, out any) (bool, error) {
	if s.fail {
		return false, errors.New("database hiccup")
	}
	return s.settingsStore.Setting(ctx, key, out)
}

// Mutes the hub could not read are not handed to the managers (an empty set would unmute every agent there); once
// read they go, and stay the ones read.
func TestMutesNotSentWhenUnreadable(t *testing.T) {
	h := newTestHub(t)
	st := &failingMutes{settingsStore: settingsStore{vals: map[string][]byte{}}, fail: true}
	h.store = st
	if err := st.PutSetting(context.Background(), "mutes", Pins{Spaces: []string{"m/app"}, Sessions: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.mutesEvent(); ok {
		t.Fatal("unreadable mutes would be sent")
	}
	st.fail = false
	m, ok := h.mutesEvent()
	if !ok || m.Mutes == nil || !slices.Equal(m.Mutes.Spaces, []string{"m/app"}) {
		t.Fatalf("mutes once readable: %+v %v", m.Mutes, ok)
	}
	st.fail = true // kept in memory once read
	if m, ok := h.mutesEvent(); !ok || !slices.Equal(m.Mutes.Spaces, []string{"m/app"}) {
		t.Fatalf("mutes read before: %+v %v", m.Mutes, ok)
	}
}
