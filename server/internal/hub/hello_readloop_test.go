package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

// helloStore can hold the loads and writes behind client_hello and register_phone. Its gates stay closed until the
// test releases them, even when a store operation's context expires: the operation deadline must never race the
// response assertion. Test cleanup releases any handler still blocked after a failure.
type helloStore struct {
	*settingsStore
	loadStarted    chan string
	putStarted     chan string
	toolsStarted   chan db.PhoneToolRegistration
	loadRelease    chan struct{}
	putRelease     chan struct{}
	toolsRelease   chan struct{}
	releaseOnce    sync.Once
	holdLoads      bool
	failAppsLoad   atomic.Bool
	failDeviceLoad atomic.Bool
	failUserLoad   atomic.Bool
	failAppsPut    atomic.Bool

	stateMu       sync.Mutex
	toolCalls     int
	registrations []db.PhoneToolRegistration
	putsByKey     map[string]int
	attemptsByKey map[string]int
}

func newHelloStore() *helloStore {
	return &helloStore{
		settingsStore: &settingsStore{vals: map[string][]byte{}},
		loadStarted:   make(chan string, 32),
		putStarted:    make(chan string, 32),
		toolsStarted:  make(chan db.PhoneToolRegistration, 32),
		loadRelease:   make(chan struct{}),
		putRelease:    make(chan struct{}),
		toolsRelease:  make(chan struct{}),
		putsByKey:     map[string]int{},
		attemptsByKey: map[string]int{},
	}
}

func (s *helloStore) release() {
	s.releaseOnce.Do(func() {
		close(s.loadRelease)
		close(s.putRelease)
		close(s.toolsRelease)
	})
}

func (s *helloStore) ReplacePhoneTools(ctx context.Context, registration db.PhoneToolRegistration) error {
	s.stateMu.Lock()
	s.toolCalls++
	call := s.toolCalls
	s.stateMu.Unlock()
	select {
	case s.toolsStarted <- registration:
	default:
	}
	// Only the older write waits. If a broken implementation runs the newer write concurrently, it can finish and
	// the older one then gets its chance to overwrite it: the final-state assertions catch that ordering bug.
	if call == 1 {
		<-s.toolsRelease
	}
	s.stateMu.Lock()
	s.registrations = append(s.registrations, registration)
	s.stateMu.Unlock()
	return nil
}

func (s *helloStore) savedRegistrations() []db.PhoneToolRegistration {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return append([]db.PhoneToolRegistration(nil), s.registrations...)
}

func (s *helloStore) putCount(key string) int {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.putsByKey[key]
}

func (s *helloStore) putAttempts(key string) int {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.attemptsByKey[key]
}

func (s *helloStore) savedSetting(t *testing.T, key string, out any) {
	t.Helper()
	s.settingsStore.mu.Lock()
	raw := append([]byte(nil), s.settingsStore.vals[key]...)
	s.settingsStore.mu.Unlock()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode saved %s %q: %v", key, raw, err)
	}
}

func waitHelloWrites(t *testing.T, h *Hub) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		h.waitHelloWrites()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("hello and phone-registration consumers did not become idle")
	}
}

func waitClientMessage(t *testing.T, c *client, messageType string) map[string]any {
	t.Helper()
	timer := time.NewTimer(readLoopLimit)
	defer timer.Stop()
	for {
		select {
		case raw := <-c.out:
			var msg map[string]any
			if json.Unmarshal(raw, &msg) == nil && msg["type"] == messageType {
				return msg
			}
		case <-timer.C:
			t.Fatalf("%s was not answered within %s", messageType, readLoopLimit)
			return nil
		}
	}
}

func waitHelloLoop(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(readLoopLimit):
		t.Fatalf("%s remained blocked", what)
	}
}

// The connection reads these messages in order. Neither its ping nor another app's hello may wait for either store
// write: nothing in this sequence has a reply whose contents depend on the writes finishing (#6889).
func TestHelloAndRegisterPhoneLeaveReadLoopWhileStoreWritesAreHeld(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	h.store = store
	t.Cleanup(store.release)

	// Keep ReplacePhoneTools occupied too, so the ping behind the registration would wait for both writes in the old
	// code rather than merely slipping past a write that had not started yet.
	seed := &client{hub: h, device: "dev_seed", role: roleFull, out: make(chan []byte, 16)}
	seedDone := make(chan struct{})
	go func() {
		defer close(seedDone)
		h.onClientMessage(context.Background(), seed, []byte(`{"type":"register_phone","device_id":"seed","tools":[{"name":"phone_device_info"}]}`))
	}()
	select {
	case <-store.toolsStarted:
	case <-time.After(readLoopLimit):
		t.Fatal("the first phone-tool write did not start")
	}

	phone := &client{hub: h, device: "dev_phone", role: roleFull, out: make(chan []byte, 16)}
	phoneDone := make(chan struct{})
	go func() {
		defer close(phoneDone)
		for _, raw := range []string{
			`{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`,
			`{"type":"register_phone","device_id":"phone","tools":[{"name":"phone_contacts"}]}`,
			`{"type":"ping","id":"behind-writes"}`,
		} {
			h.onClientMessage(context.Background(), phone, []byte(raw))
		}
	}()
	select {
	case <-store.putStarted:
	case <-time.After(readLoopLimit):
		t.Fatal("the client-apps write did not start")
	}

	waitClientMessage(t, phone, "pong")
	other := &client{hub: h, device: "dev_other", role: roleFull, out: make(chan []byte, 16)}
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		h.onClientMessage(context.Background(), other, []byte(`{"type":"client_hello","platform":"android","version":"2.0","locale":"ja"}`))
	}()
	waitClientMessage(t, other, "client_hello_ack")

	store.release()
	for _, done := range []chan struct{}{seedDone, phoneDone, otherDone} {
		waitHelloLoop(t, done, "a connection")
	}
	waitHelloWrites(t, h)
}

// A missing user_locale is not permission to save one when the device history needed to decide it could not be read.
// The next hello retries, keeps the stored device history, and only then makes and saves the first-language decision.
func TestDeviceLocaleLoadFailureSkipsUserSave(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.release()
	existing := map[string]deviceLocale{"dev_keep": {Locale: i18n.Ja, At: time.Now().UTC()}}
	raw, _ := json.Marshal(existing)
	store.vals[deviceLocalesSetting] = raw
	store.failDeviceLoad.Store(true)
	h.store = store

	first := &client{hub: h, device: "dev_dropped", role: roleFull, out: make(chan []byte, 8)}
	h.onClientHello(first, []byte(`{"locale":"ja"}`))
	waitHelloWrites(t, h)
	if got := store.putCount(deviceLocalesSetting) + store.putCount(userLocaleSetting); got != 0 {
		t.Fatalf("a failed device-locale load caused %d locale saves", got)
	}
	if h.userLang() != i18n.ZhHans {
		t.Fatalf("the failed batch published a user language: %s", h.userLang())
	}

	next := &client{hub: h, device: "dev_new", role: roleFull, out: make(chan []byte, 8)}
	h.onClientHello(next, []byte(`{"locale":"en"}`))
	waitHelloWrites(t, h)
	if got := store.putCount(deviceLocalesSetting); got != 1 {
		t.Fatalf("the device-locale retry made %d saves, want one", got)
	}
	if got := store.putCount(userLocaleSetting); got != 1 {
		t.Fatalf("the user-locale retry made %d saves, want one", got)
	}
	var saved map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &saved)
	if len(saved) != 2 || saved["dev_keep"].Locale != i18n.Ja || saved["dev_new"].Locale != i18n.En {
		t.Fatalf("the retry did not preserve old and new device locales: %+v", saved)
	}
	if _, retried := saved["dev_dropped"]; retried {
		t.Fatalf("the report whose load failed was not skipped: %+v", saved)
	}
	if h.userLang() != i18n.En {
		t.Fatalf("the successful first report did not set the user language: %s", h.userLang())
	}
}

// Latest-per-device is not a valid locale coalescing rule. A changes to zh-Hans, B then changes to zh-Hans, and A
// changes back to en. Applying all three in order finishes at en; dropping A's intermediate change would finish at zh.
func TestLocaleBurstPreservesCrossDeviceOrder(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.holdLoads = true
	h.store = store
	t.Cleanup(store.release)
	at := time.Now().UTC()
	storedLocales := map[string]deviceLocale{"dev_a": {Locale: i18n.En, At: at}, "dev_b": {Locale: i18n.Ja, At: at}}
	raw, _ := json.Marshal(storedLocales)
	store.vals[deviceLocalesSetting] = raw
	store.vals[userLocaleSetting] = []byte(`"en"`)
	h.loadUserLang(context.Background())

	a := &client{hub: h, device: "dev_a", role: roleFull, out: make(chan []byte, 8)}
	b := &client{hub: h, device: "dev_b", role: roleFull, out: make(chan []byte, 8)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, report := range []struct {
			c      *client
			locale string
		}{{a, "zh-Hans"}, {b, "zh-Hans"}, {a, "en"}} {
			h.onClientHello(report.c, []byte(`{"locale":"`+report.locale+`"}`))
		}
	}()
	waitClientMessage(t, a, "client_hello_ack")
	waitClientMessage(t, b, "client_hello_ack")
	waitClientMessage(t, a, "client_hello_ack")
	store.release()
	waitHelloLoop(t, done, "the cross-device locale burst")
	waitHelloWrites(t, h)
	if h.userLang() != i18n.En {
		t.Fatalf("the coalesced locale batch reordered or dropped intermediate changes: %s", h.userLang())
	}
	for _, key := range []string{deviceLocalesSetting, userLocaleSetting} {
		if got := store.putCount(key); got != 1 {
			t.Fatalf("%s was written %d times, want one final snapshot", key, got)
		}
	}
	var saved map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &saved)
	if saved["dev_a"].Locale != i18n.En || saved["dev_b"].Locale != i18n.ZhHans {
		t.Fatalf("the final device locales are wrong: %+v", saved)
	}
	var savedUser string
	store.savedSetting(t, userLocaleSetting, &savedUser)
	if savedUser != string(i18n.En) {
		t.Fatalf("the persisted user locale is %q, want en", savedUser)
	}
}

// Delayed persistence must keep the receipt time, not stamp every report with the consumer's time and change which
// device history is pruned. The older changed device is the one that falls out of this full snapshot.
func TestLocaleBatchUsesReceiptTimesForPruning(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.release()
	h.store = store
	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	stored := make(map[string]deviceLocale, deviceLocalesMax)
	stored["dev_a"] = deviceLocale{Locale: i18n.En, At: base}
	for i := 1; i < deviceLocalesMax; i++ {
		stored[fmt.Sprintf("dev_%02d", i)] = deviceLocale{Locale: i18n.En, At: base.Add(time.Duration(i+1) * time.Second)}
	}
	raw, _ := json.Marshal(stored)
	store.vals[deviceLocalesSetting] = raw
	store.vals[userLocaleSetting] = []byte(`"en"`)
	h.writeUserLangs([]userLangUpdate{
		{key: "dev_a", lang: i18n.Ja, at: base.Add(time.Second)},
		{key: "dev_b", lang: i18n.Ja, at: base.Add(100 * time.Second)},
	})
	var saved map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &saved)
	if len(saved) != deviceLocalesMax {
		t.Fatalf("saved %d device locales, want %d", len(saved), deviceLocalesMax)
	}
	if _, kept := saved["dev_a"]; kept {
		t.Fatal("consumer-side timestamps kept the oldest report instead of pruning it")
	}
	if saved["dev_b"].At != base.Add(100*time.Second) || saved["dev_01"].Locale != i18n.En {
		t.Fatalf("the batch did not preserve receipt times and newer history: %+v", saved)
	}
}

// Replacing A's pending registration must move it behind B: the surviving messages are B1, A2, not A2, B1.
func TestRegisterPhoneCoalescingKeepsLatestMessageOrder(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	h.store = store
	t.Cleanup(store.release)
	seed := &client{hub: h, device: "dev_seed", role: roleFull, out: make(chan []byte, 16)}
	h.onClientMessage(context.Background(), seed, []byte(`{"type":"register_phone","device_id":"seed","tools":[{"name":"phone_device_info"}]}`))
	select {
	case <-store.toolsStarted:
	case <-time.After(readLoopLimit):
		t.Fatal("the seed phone-tool write did not start")
	}
	a := &client{hub: h, device: "dev_a", role: roleFull, out: make(chan []byte, 16)}
	b := &client{hub: h, device: "dev_b", role: roleFull, out: make(chan []byte, 16)}
	for _, report := range []struct {
		c   *client
		raw string
	}{
		{a, `{"type":"register_phone","device_id":"a","tools":[{"name":"phone_contacts"}]}`},
		{b, `{"type":"register_phone","device_id":"b","tools":[{"name":"phone_contacts"}]}`},
		{a, `{"type":"register_phone","device_id":"a","tools":[{"name":"phone_contacts"},{"name":"phone_reminders_list"}]}`},
	} {
		h.onClientMessage(context.Background(), report.c, []byte(report.raw))
	}
	store.release()
	waitHelloWrites(t, h)
	registrations := store.savedRegistrations()
	if len(registrations) != 3 {
		t.Fatalf("got %d registrations, want seed plus the two surviving snapshots: %+v", len(registrations), registrations)
	}
	for i, want := range []string{"seed", "b", "a"} {
		if registrations[i].DeviceID != want {
			t.Fatalf("registration order = %+v, want seed, b, a", registrations)
		}
	}
	if got := registrations[2].Tools; len(got) != 2 || got[1].Name != "phone_reminders_list" {
		t.Fatalf("the last registration is not A's newest snapshot: %+v", got)
	}
}
