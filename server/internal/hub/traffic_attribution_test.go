package hub

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrafficAttribution(t *testing.T) {
	for _, tc := range []struct {
		name, ua, kind, client, source, platform, surface, platformBasis, sourceBasis string
	}{
		{"legacy ios", "Sample/1 CFNetwork/1490 Darwin/25", "phone", "", "", "unknown", "unknown", "unknown", "unknown"},
		{"widget ua", "SampleWidget/1 CFNetwork/1490 Darwin/25", "phone", "", "", "unknown", "widget", "unknown", "user-agent"},
		{"activity ua", "SampleLiveActivity/1 CFNetwork/1490 Darwin/25", "phone", "", "", "unknown", "liveactivity", "unknown", "user-agent"},
		{"android", "Dalvik/2.1.0 (Linux; Android 16)", "phone", "", "", "unknown", "unknown", "unknown", "unknown"},
		{"okhttp", "okhttp/5.0", "other", "", "", "unknown", "unknown", "unknown", "unknown"},
		{"browser android", "Mozilla/5.0 (Linux; Android 16) Chrome/140", "phone", "", "", "web", "app", "user-agent", "browser"},
		{"browser ios", "Mozilla/5.0 (iPhone) Version/26 Safari/605", "phone", "", "", "web", "app", "user-agent", "browser"},
		{"harmony", "", "phone", "harmony/1.0.0", "app", "harmony", "app", "client-header", "source-header"},
		{"explicit app", "Mozilla/5.0 (iPhone)", "phone", "ios/1", "app", "ios", "app", "client-header", "source-header"},
		{"explicit widget", "CFNetwork/1 Darwin/1", "phone", "ios-widget/1", "", "ios", "widget", "client-header", "client-header"},
		{"explicit activity", "CFNetwork/1 Darwin/1", "phone", "ios/1", "liveactivity", "ios", "liveactivity", "client-header", "source-header"},
		{"watch", "Sample/1 CFNetwork/1 Darwin/1", "watch", "", "", "unknown", "unknown", "unknown", "unknown"},
		{"mac", "Sample/1 CFNetwork/1 Darwin/1", "other", "", "", "unknown", "unknown", "unknown", "unknown"},
		{"curl", "curl/8.0", "phone", "", "", "unknown", "diagnostic", "unknown", "user-agent"},
		{"python", "Python-urllib/3.11", "phone", "", "", "unknown", "diagnostic", "unknown", "user-agent"},
		{"unidentified", "", "other", "", "", "unknown", "unknown", "unknown", "unknown"},
		{"oversized ua", strings.Repeat("widget", 200), "other", "", "", "unknown", "unknown", "unknown", "unknown"},
		{"malformed header", "curl/1", "phone", "ios/1\nprivate-token", "", "unknown", "diagnostic", "unknown", "user-agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/todos", nil)
			r.Header.Set("User-Agent", tc.ua)
			r.Header.Set("X-Persona-Client", tc.client)
			r.Header.Set("X-Persona-Source", tc.source)
			a := (&Hub{}).requestTrafficAttribution(r, principal{Kind: tc.kind})
			want := trafficAttribution{tc.platform, tc.surface, tc.platformBasis, tc.sourceBasis}
			if a != want {
				t.Fatalf("got %+v, want %+v", a, want)
			}
		})
	}
}
