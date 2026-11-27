package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// A machine that answered before and is offline now keeps its launchers, marked stale, with its rc file.
func TestOfflineMachinesLaunchersAreStale(t *testing.T) {
	s, f, _ := modelService(t)
	f.mu.Lock()
	f.devices = []Device{{ID: "away", Name: "Away", Online: true, Reachable: true}}
	f.zshrc = map[string][]proto.Launcher{"away": {{Name: "kcc", BaseURL: "https://gw.example.com", Source: "zshrc"}}}
	f.sessions["away"] = nil
	f.mu.Unlock()
	if code, raw := launcherCall(t, s, "GET", "/api/v1/launchers", ""); code != 200 {
		t.Fatal(code, raw)
	}
	f.mu.Lock()
	f.devices = []Device{{ID: "away", Name: "Away"}}
	f.mu.Unlock()
	var raw string
	waitFor(t, "the offline launcher refresh", func() bool {
		_, raw = launcherCall(t, s, "GET", "/api/v1/launchers", "")
		var res struct {
			Launchers []LauncherView `json:"launchers"`
			Sync      []LauncherSync `json:"sync"`
		}
		_ = json.Unmarshal([]byte(raw), &res)
		i := slices.IndexFunc(res.Launchers, func(v LauncherView) bool { return v.Name == "kcc" })
		return i >= 0 && slices.Equal(res.Launchers[i].Stale, []string{"away"}) && slices.Equal(res.Launchers[i].RCFiles, []string{"~/.zshrc"}) &&
			len(res.Sync) == 1 && res.Sync[0].State == "offline" && res.Sync[0].SyncedAt != nil
	})
}

// A restored snapshot still names an offline machine before its background refresh can answer (#3198).
func TestRestoredLauncherListKeepsOfflineStale(t *testing.T) {
	s, f, _ := modelService(t)
	f.mu.Lock()
	f.devices = []Device{{ID: "away", Name: "Away", Online: true, Reachable: true}}
	f.zshrc = map[string][]proto.Launcher{"away": {{Name: "kcc", BaseURL: "https://gw.example.com", Source: "zshrc"}}}
	f.sessions["away"] = nil
	f.mu.Unlock()
	_ = s.zshrcLaunchers(t.Context(), true)
	s.models.zshrcMu.Lock()
	s.models.zshrc, s.models.zshrcSync = nil, nil
	s.models.zshrcMu.Unlock()
	f.mu.Lock()
	f.devices = []Device{{ID: "away", Name: "Away"}}
	f.mu.Unlock()

	code, raw := launcherCall(t, s, "GET", "/api/v1/launchers", "")
	var res struct {
		Launchers []LauncherView `json:"launchers"`
		Sync      []LauncherSync `json:"sync"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(raw), &res) != nil {
		t.Fatalf("%d %s", code, raw)
	}
	i := slices.IndexFunc(res.Launchers, func(v LauncherView) bool { return v.Name == "kcc" })
	if i < 0 || !slices.Equal(res.Launchers[i].Stale, []string{"away"}) {
		t.Fatalf("the restored row lost its offline mark: %s", raw)
	}
	if len(res.Sync) != 1 || res.Sync[0].State != "offline" || res.Sync[0].SyncedAt == nil {
		t.Fatalf("the restored sync lost its offline state: %+v", res.Sync)
	}
}

// A slow node never holds the list: the last snapshot answers while one refresh runs behind it (#3198).
func TestLauncherListAnswersLastSeenWhileRefreshRuns(t *testing.T) {
	s, f, _ := modelService(t)
	f.mu.Lock()
	f.devices = []Device{{ID: "box", Online: true, Reachable: true}, {ID: "slow", Online: true, Reachable: true}}
	f.zshrc = map[string][]proto.Launcher{"box": {{Name: "before"}}, "slow": {}}
	for _, id := range []string{"box", "slow"} {
		f.sessions[id] = nil
	}
	f.mu.Unlock()
	_ = s.zshrcLaunchers(t.Context(), true) // prime the last-seen snapshot without the route under test
	s.models.zshrcMu.Lock()
	s.models.zshrc, s.models.zshrcSync = nil, nil // a restart keeps the persisted snapshot, not the memory
	s.models.zshrcMu.Unlock()

	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(release)
	f.mu.Lock()
	f.zshrc = map[string][]proto.Launcher{"box": {{Name: "after"}}, "slow": {}}
	f.launcherGate = gate
	f.mu.Unlock()

	type answer struct {
		code int
		raw  string
	}
	asyncCall := func() <-chan answer {
		ch := make(chan answer, 1)
		go func() {
			r := httptest.NewRequest("GET", "/api/v1/launchers", strings.NewReader(""))
			ctx, cancel := context.WithCancel(r.Context())
			recorder := httptest.NewRecorder()
			s.ServeHTTP(recorder, r.WithContext(ctx))
			cancel() // the background refresh must outlive the client request
			ch <- answer{recorder.Code, recorder.Body.String()}
		}()
		return ch
	}
	await := func(ch <-chan answer) answer {
		t.Helper()
		select {
		case a := <-ch:
			return a
		case <-time.After(2 * time.Second):
			t.Fatal("the list waited for the gated node instead of answering last seen")
			return answer{}
		}
	}

	countCalls := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		calls := 0
		for _, call := range f.calls {
			if call.Method == DeviceLaunchersList {
				calls++
			}
		}
		return calls
	}
	first := await(asyncCall())
	waitFor(t, "the background refresh to reach both nodes", func() bool { return countCalls() >= 4 })
	second := await(asyncCall())
	for _, a := range []answer{first, second} {
		if a.code != http.StatusOK || !strings.Contains(a.raw, "before") || strings.Contains(a.raw, "after") {
			t.Fatalf("cached answer: %d %s", a.code, a.raw)
		}
	}
	if calls := countCalls(); calls != 4 { // two to prime, then one per machine for the single background refresh
		t.Fatalf("launchers.list calls = %d, want 4 (concurrent reads started another refresh)", calls)
	}

	release()
	waitFor(t, "the refreshed launcher snapshot", func() bool {
		snapshot, _ := s.zshrcLaunchersLastSeen(t.Context())
		for _, machine := range snapshot {
			for _, launcher := range machine.Launchers {
				if launcher.Name == "after" {
					return true
				}
			}
		}
		return false
	})
	code, raw := launcherCall(t, s, "GET", "/api/v1/launchers", "")
	if code != http.StatusOK || !strings.Contains(raw, "after") {
		t.Fatalf("the refreshed snapshot did not reach the route: %d %s", code, raw)
	}
}
