package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func fastLauncherRecovery(t *testing.T) {
	t.Helper()
	old := launcherRecoveryPoll
	launcherRecoveryPoll = 2 * time.Millisecond
	t.Cleanup(func() { launcherRecoveryPoll = old })
}

func setLauncherRecovery(f *fakeHub, waitSeconds int, fallbacks map[string][]string) {
	f.hello.Models.LauncherRecovery = map[string]LauncherRecovery{
		"server": {WaitSeconds: waitSeconds, Fallbacks: fallbacks},
	}
}

func launcherListCalls(f *fakeHub) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.launcherListParams...)
}

func TestMainChatLauncherRecoversWithFreshDiscovery(t *testing.T) {
	fastLauncherRecovery(t)
	s, f := kindFixture(t)
	setLauncherRecovery(f, 1, nil)
	f.launcherList = []proto.Launcher{{Name: "cx", Agent: "codex"}}
	params := map[string]any{"launcher": "wrap", "model": "claude-opus-5-5"}
	type result struct {
		note string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		note, err := s.finishMainChatModel(context.Background(), "server", "assistant", params, false)
		done <- result{note: note, err: err}
	}()
	waitFor(t, "a fresh launcher-discovery retry", func() bool {
		for _, call := range launcherListCalls(f) {
			if call["refresh"] == true {
				return true
			}
		}
		return false
	})
	f.mu.Lock()
	f.launcherList = []proto.Launcher{{Name: "wrap"}, {Name: "cx", Agent: "codex"}}
	f.mu.Unlock()
	select {
	case got := <-done:
		if got.err != nil || got.note != "" || params["launcher"] != "wrap" || params["model"] != "claude-opus-5-5" {
			t.Fatalf("recovery: params %v, note %q, err %v", params, got.note, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recovery did not finish after the launcher returned")
	}
	calls := launcherListCalls(f)
	if len(calls) < 2 || calls[0]["refresh"] == true {
		t.Fatalf("discovery calls = %v, want an ordinary initial read followed by fresh retries", calls)
	}
}

func TestMainChatLauncherIncompleteRefreshDoesNotCountAsRecovery(t *testing.T) {
	s, f := kindFixture(t)
	// An older node, or a response whose shell entries are all hidden, may contain only managed/built-in entries.
	// Those entries can be explicit substitutes, but they do not prove that the requested shell alias is back.
	f.launcherList = []proto.Launcher{{Name: "managed", Source: "persona"}}
	r := s.mainChatFitState(context.Background(), "server", "wrap", true)
	if r.fit != mainChatGone || !r.transient {
		t.Fatalf("incomplete forced catalog released the original send: %+v", r)
	}
}

func TestMainChatIncompatibleLauncherDoesNotWaitForDiscovery(t *testing.T) {
	s, _ := mainChatFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	params := map[string]any{"launcher": "cc-relay"}
	_, err := s.finishMainChatModel(ctx, "server", "assistant", params, false)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "cc-relay") {
		t.Fatalf("incompatible launcher waited or lost its refusal: %v", err)
	}
}

func TestMainChatLauncherRecoveryHonorsCancellation(t *testing.T) {
	fastLauncherRecovery(t)
	s, f := kindFixture(t)
	setLauncherRecovery(f, 30, map[string][]string{"wrap": {"claude"}})
	f.launcherList = []proto.Launcher{{Name: "cx", Agent: "codex"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := map[string]any{"launcher": "wrap", "model": "claude-opus-5-5"}
	done := make(chan error, 1)
	go func() {
		_, err := s.finishMainChatModel(ctx, "server", "assistant", params, false)
		done <- err
	}()
	waitFor(t, "the first fresh discovery", func() bool {
		for _, call := range launcherListCalls(f) {
			if call["refresh"] == true {
				return true
			}
		}
		return false
	})
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation left launcher recovery blocked")
	}
	if params["launcher"] != "wrap" {
		t.Fatalf("cancellation selected a substitute: %v", params)
	}
}

func TestMainChatLauncherFallbackDiscoveryHonorsCancellation(t *testing.T) {
	fastLauncherRecovery(t)
	s, f := kindFixture(t)
	setLauncherRecovery(f, 1, map[string][]string{"wrap": {"claude"}})
	f.launcherList = []proto.Launcher{{Name: "cx", Agent: "codex"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := map[string]any{"launcher": "wrap", "model": "claude-opus-5-5"}
	done := make(chan error, 1)
	go func() {
		_, err := s.finishMainChatModel(ctx, "server", "assistant", params, false)
		done <- err
	}()
	waitFor(t, "the first fresh discovery", func() bool {
		for _, call := range launcherListCalls(f) {
			if call["refresh"] == true {
				return true
			}
		}
		return false
	})
	gate := make(chan struct{})
	f.mu.Lock()
	f.launcherGate = gate
	f.mu.Unlock()
	t.Cleanup(func() { close(gate) })
	waitFor(t, "the fallback discovery", func() bool { return len(launcherListCalls(f)) >= 4 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("fallback discovery cancellation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation left fallback discovery blocked")
	}
}

func TestLauncherRecoveryWaitDefaultsAndIsBounded(t *testing.T) {
	s, f := kindFixture(t)
	if wait, _ := s.launcherRecovery(context.Background(), "server", "wrap"); wait != 30*time.Second {
		t.Fatalf("default wait = %s", wait)
	}
	setLauncherRecovery(f, 999, nil)
	if wait, _ := s.launcherRecovery(context.Background(), "server", "wrap"); wait != 2*time.Minute {
		t.Fatalf("configured wait was not capped: %s", wait)
	}
}

func TestTaskBaselineWaitsForRemoteLauncherRecovery(t *testing.T) {
	fastLauncherRecovery(t)
	f := newFakeHub().withMachine()
	f.sessions["remote"] = nil
	f.zshrc = map[string][]proto.Launcher{
		"server": {{Name: "main", Source: "zshrc"}},
		"remote": {{Name: "remote", Source: "zshrc"}},
	}
	s := newTestService(t, f)
	setModel(t, s, "assistant", ModelBinding{Provider: "launcher.main", Model: "selected-model"})
	f.hello.Models.LauncherRecovery = map[string]LauncherRecovery{
		"remote": {WaitSeconds: 1, Fallbacks: map[string][]string{"main": {"remote"}}},
	}
	type result struct {
		binding ModelBinding
		err     error
	}
	done := make(chan result, 1)
	go func() {
		binding, err := s.taskModelBaseline(context.Background(), "remote")
		done <- result{binding: binding, err: err}
	}()
	waitFor(t, "a fresh remote launcher read", func() bool {
		for _, call := range launcherListCalls(f) {
			if call["refresh"] == true {
				return true
			}
		}
		return false
	})
	f.mu.Lock()
	f.zshrc["remote"] = []proto.Launcher{{Name: "main", Source: "zshrc"}, {Name: "remote", Source: "zshrc"}}
	f.mu.Unlock()
	select {
	case got := <-done:
		if got.err != nil || got.binding.Provider != "launcher.main" {
			t.Fatalf("remote recovery was replaced by its configured fallback: %+v, err %v", got.binding, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remote launcher recovery did not finish")
	}
}
