package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
)

// statsRequest serves one GET against the push grading routes with an optional principal (nil is a full credential).
func statsRequest(t *testing.T, h *Hub, p *principal, target string) (int, []byte) {
	t.Helper()
	mux := http.NewServeMux()
	h.pushGradingRoutes(mux)
	r := httptest.NewRequest("GET", target, nil)
	if p != nil {
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, *p))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}

func extPrincipal(manager string) *principal {
	return &principal{Role: roleExtension, Manager: manager}
}

// An extension token with the notify scope may read the switch state; only a full credential may change it. The PUT
// guard itself is covered in TestPushGradingInterceptsHeldToManager.
func TestPushGradingSwitchReadableByNotifyExtension(t *testing.T) {
	h := gradingHub(t)
	if err := h.push.setPushGrading(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	code, body := statsRequest(t, h, extPrincipal("manager-a"), "/api/v1/push/ln-mode")
	if code != http.StatusOK {
		t.Fatalf("an extension token read the switch: %d %s", code, body)
	}
	var got struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &got); err != nil || !got.Enabled {
		t.Fatalf("switch answer = %s, want enabled true", body)
	}
}

// The neutral switch alias and the legacy route share one handler: GET/PUT parity, extension authorization and the
// original single storage key are pinned together so the alias cannot drift or introduce a persistence migration.
func TestPushGradingRouteAliasMatchesLegacy(t *testing.T) {
	h := gradingHub(t)
	if err := h.push.setPushGrading(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/api/v1/push/ln-mode", "/api/v1/push/grading"}
	for _, path := range paths {
		for _, p := range []*principal{nil, extPrincipal("manager-a")} {
			code, body := statsRequest(t, h, p, path)
			if code != http.StatusOK || string(body) != "{\"enabled\":true}\n" {
				t.Fatalf("GET %s (extension=%v) = %d %s, want the same enabled response", path, p != nil, code, body)
			}
		}
	}
	put := func(path string, p *principal, enabled bool) (int, []byte) {
		t.Helper()
		mux := http.NewServeMux()
		h.pushGradingRoutes(mux)
		value := "false"
		if enabled {
			value = "true"
		}
		r := httptest.NewRequest("PUT", path, strings.NewReader(`{"enabled":`+value+`}`))
		if p != nil {
			r = r.WithContext(context.WithValue(r.Context(), principalKey{}, *p))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	oldCode, oldBody := put(paths[0], extPrincipal("manager-a"), false)
	newCode, newBody := put(paths[1], extPrincipal("manager-a"), false)
	if oldCode != http.StatusForbidden || newCode != oldCode || !bytes.Equal(newBody, oldBody) {
		t.Fatalf("extension PUT parity = old %d %s, new %d %s; want identical 403", oldCode, oldBody, newCode, newBody)
	}
	if code, body := put(paths[1], nil, false); code != http.StatusOK || string(body) != "{\"enabled\":false}\n" {
		t.Fatalf("full-credential PUT on the alias = %d %s", code, body)
	}
	if code, body := statsRequest(t, h, nil, paths[0]); code != http.StatusOK || string(body) != "{\"enabled\":false}\n" {
		t.Fatalf("legacy GET after alias PUT = %d %s", code, body)
	}
	if code, body := put(paths[0], nil, true); code != http.StatusOK {
		t.Fatalf("full-credential PUT on the legacy route = %d %s", code, body)
	}
	if code, body := statsRequest(t, h, nil, paths[1]); code != http.StatusOK || string(body) != "{\"enabled\":true}\n" {
		t.Fatalf("alias GET after legacy PUT = %d %s", code, body)
	}
	store := h.push.store.(*gradingStore)
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.vals) != 1 || string(store.vals[gradingSwitchSettingKey]) != "true" {
		t.Fatalf("stored settings = %#v, want only the original grading switch key set to true", store.vals)
	}
}

// gradingZonedClock installs one race-free clock whose instant and UTC offset can move independently.
func gradingZonedClock(h *Hub) (instant, offset *atomic.Int64) {
	instant, offset = &atomic.Int64{}, &atomic.Int64{}
	instant.Store(time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC).UnixNano())
	h.push.now = func() time.Time {
		return time.Unix(0, instant.Load()).In(time.FixedZone("", int(offset.Load())))
	}
	return instant, offset
}

func TestPushDailyStatsWindowUsesUTCCalendarDay(t *testing.T) {
	read := func(t *testing.T, h *Hub, target string) (string, []pushDayStats) {
		t.Helper()
		code, body := statsRequest(t, h, extPrincipal("manager-a"), target)
		if code != http.StatusOK {
			t.Fatalf("stats: %d %s", code, body)
		}
		var out struct {
			Since string         `json:"since"`
			Days  []pushDayStats `json:"days"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out.Since, out.Days
	}

	t.Run("UTC+8 does not start in the future", func(t *testing.T) {
		h := gradingHub(t)
		clock, offset := gradingZonedClock(h)
		clock.Store(time.Date(2026, 11, 14, 23, 30, 0, 0, time.UTC).UnixNano())
		offset.Store(8 * 60 * 60) // local 2026-11-15 07:30 +08:00
		if _, err := h.deliverPushFrom("manager-a", gradingNotification(assistantwire.LevelP1, "utc-east-day", "one")); err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{"/api/v1/push/stats/daily", "/api/v1/push/stats/daily?days=1"} {
			since, days := read(t, h, target)
			if since != "2026-11-14T00:00:00Z" || len(days) != 1 || days[0].Date != "2026-11-14" ||
				days[0].Manager != "manager-a" || days[0].Sent != 1 {
				t.Fatalf("%s: since %s days %+v, want the current UTC day with one sent push", target, since, days)
			}
		}
	})

	t.Run("UTC-8 does not include the previous UTC day", func(t *testing.T) {
		h := gradingHub(t)
		clock, offset := gradingZonedClock(h)
		clock.Store(time.Date(2026, 11, 14, 23, 30, 0, 0, time.UTC).UnixNano())
		offset.Store(8 * 60 * 60) // the previous notification is at local 2026-11-15 07:30 +08:00
		if _, err := h.deliverPushFrom("manager-a", gradingNotification(assistantwire.LevelP1, "utc-previous-day", "old")); err != nil {
			t.Fatal(err)
		}
		clock.Store(time.Date(2026, 11, 15, 0, 30, 0, 0, time.UTC).UnixNano())
		offset.Store(-8 * 60 * 60) // the current clock reads local 2026-11-14 16:30 -08:00
		if _, err := h.deliverPushFrom("manager-a", gradingNotification(assistantwire.LevelP1, "utc-current-day", "new")); err != nil {
			t.Fatal(err)
		}
		since, days := read(t, h, "/api/v1/push/stats/daily?days=1")
		if since != "2026-11-15T00:00:00Z" || len(days) != 1 || days[0].Date != "2026-11-15" ||
			days[0].Manager != "manager-a" || days[0].Sent != 1 {
			t.Fatalf("since %s days %+v, want only the current UTC day with one sent push", since, days)
		}
	})
}

// The stats endpoint answers only the extension's own manager, understands since/days and rejects bad windows. Days
// bucket by UTC calendar day.
func TestPushDailyStatsHeldToManager(t *testing.T) {
	h := gradingHub(t)
	base := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	setGradingClock(h, base)
	if _, err := h.deliverPushFrom("manager-a", gradingNotification(assistantwire.LevelP1, "a-today", "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.deliverPushFrom("manager-b", gradingNotification(assistantwire.LevelP1, "b-today", "b")); err != nil {
		t.Fatal(err)
	}
	moveGradingClock(h, base.AddDate(0, 0, -1))
	if _, err := h.deliverPushFrom("manager-b", gradingNotification(assistantwire.LevelP1, "b-yesterday", "b old")); err != nil {
		t.Fatal(err)
	}
	moveGradingClock(h, base)
	// The switch is off, so all three went to the fake APNs; wait out their goroutines before the server closes.

	read := func(target string) (int, struct {
		Since string         `json:"since"`
		Days  []pushDayStats `json:"days"`
	}) {
		code, body := statsRequest(t, h, extPrincipal("manager-b"), target)
		var out struct {
			Since string         `json:"since"`
			Days  []pushDayStats `json:"days"`
		}
		if code == http.StatusOK {
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatal(err)
			}
		}
		return code, out
	}
	code, out := read("/api/v1/push/stats/daily")
	if code != http.StatusOK {
		t.Fatalf("default window: %d", code)
	}
	if len(out.Days) != 1 || out.Days[0].Manager != "manager-b" || out.Days[0].Sent != 1 || out.Days[0].Date != "2026-11-14" {
		t.Fatalf("today for manager-b = %+v, want one delivered push on 2026-11-14", out)
	}
	code, out = read("/api/v1/push/stats/daily?days=2")
	if code != http.StatusOK || len(out.Days) != 2 {
		t.Fatalf("days=2: code %d days %+v", code, out.Days)
	}
	if out.Days[0].Date != "2026-11-13" || out.Days[1].Date != "2026-11-14" {
		t.Fatalf("day buckets = %+v, want ascending UTC days", out.Days)
	}
	for _, d := range out.Days {
		if d.Manager != "manager-b" {
			t.Fatalf("manager-b's stats contain %q", d.Manager)
		}
	}
	since := base.AddDate(0, 0, -1).Format(time.RFC3339)
	if code, out = read("/api/v1/push/stats/daily?since=" + since); code != http.StatusOK || len(out.Days) != 2 {
		t.Fatalf("since: code %d days %+v", code, out.Days)
	}
	for _, bad := range []string{"/api/v1/push/stats/daily?days=0", "/api/v1/push/stats/daily?days=99", "/api/v1/push/stats/daily?days=two",
		"/api/v1/push/stats/daily?since=not-a-time"} {
		if code, _ := read(bad); code != http.StatusBadRequest {
			t.Fatalf("%s answered %d, want 400", bad, code)
		}
	}
	// A full credential sees every manager.
	code, body := statsRequest(t, h, nil, "/api/v1/push/stats/daily?days=2")
	if code != http.StatusOK {
		t.Fatalf("full credential: %d %s", code, body)
	}
	var all struct {
		Days []pushDayStats `json:"days"`
	}
	if err := json.Unmarshal(body, &all); err != nil || len(all.Days) != 3 {
		t.Fatalf("full credential days = %+v (%s), want three manager-day rows", all.Days, body)
	}
}

// flakyPutStore fails the first failsLeft PutSetting and AddPushSent calls, then delegates to the real in-memory store.
type flakyPutStore struct {
	*gradingStore
	mu        sync.Mutex
	failsLeft int
}
