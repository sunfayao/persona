package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
)

// The table-driven hub settings: settings.list names each with its current value, get reads it, and a set is held to
// the route's checks before any card (a bad value, a no-op and an unknown device never reach the phone).
func TestHubValueSettingsListGetAndRefuse(t *testing.T) {
	h := newTestHub(t)
	connectAssistant(t, h)
	m := &Machine{cfg: MachineConfig{ID: "box"}, hub: h}
	h.push.mu.Lock()
	if h.push.devices == nil {
		h.push.devices = map[string]Device{}
	}
	h.push.devices["d1"] = Device{DeviceID: "d1", Name: "Phone", Kind: "ios", Credential: "c1"}
	h.push.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	raw, ok, err := h.assistantAppCall(ctx, assistantwire.AssistantID, assistantwire.AppCall{AppID: assistantwire.AppSettings, Method: assistantwire.SettingsList})
	if !ok || err != nil {
		t.Fatalf("settings.list: %v %v", ok, err)
	}
	var list struct {
		Settings []assistantwire.Setting `json:"settings"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	byKey := map[string]assistantwire.Setting{}
	for _, s := range list.Settings {
		byKey[s.Key] = s
	}
	for _, key := range []string{"notifications.devices", "agent_settings.claude", "agent_settings.codex"} {
		s, ok := byKey[key]
		if !ok || s.Title == "" || s.About == "" || s.Value == "" || strings.Join(s.Actions, ",") != "get,set" {
			t.Errorf("settings.list entry %s: %+v", key, s)
		}
	}
	if !strings.Contains(byKey["notifications.devices"].Current, "d1") {
		t.Errorf("current value of notifications.devices: %q", byKey["notifications.devices"].Current)
	}

	run := func(args string) (string, error) {
		return h.toolPersonaSettings(ctx, assistantwire.AssistantID, m, "s1", json.RawMessage(args), nil)
	}
	if out, err := run(`{"action":"get","key":"notifications.devices"}`); err != nil || !strings.Contains(out, "Phone") {
		t.Fatalf("get: %q %v", out, err)
	}
	for _, bad := range []string{
		`{"device_id":"d1"}`,                          // nothing to change
		`{"device_id":"nope","input":true}`,           // unknown device
		`{"device_id":"d1","quiet":{"enabled":true}}`, // quiet hours need a window
		`"d1"`,
	} {
		if out, err := run(`{"action":"set","key":"notifications.devices","value":` + bad + `}`); err == nil && out != tableText(h.userLang(), "no_change", i18n.Args{"title": tableText(h.userLang(), "title_notifications_devices")}) {
			t.Errorf("value %s reached the card: %q", bad, out)
		}
	}
	if len(h.pendingInteractions()) != 0 {
		t.Fatal("a refused value made a card")
	}
	// the prefs the device has already: no change, no card
	cur := h.push.prefs("d1")
	same, _ := json.Marshal(map[string]any{"device_id": "d1", "input": cur.Input, "done": cur.Done})
	if out, err := run(`{"action":"set","key":"notifications.devices","value":` + string(same) + `}`); err != nil || out != tableText(h.userLang(), "no_change", i18n.Args{"title": tableText(h.userLang(), "title_notifications_devices")}) || len(h.pendingInteractions()) != 0 {
		t.Fatalf("same prefs: %q %v", out, err)
	}
}
