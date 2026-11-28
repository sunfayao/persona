package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
)

// persona_settings notifications.low_distraction (#8157): settings.list names it, get answers what GET
// /api/v1/push/ln-mode answers, and set changes the switch only once the owner approves the phone card: while the card
// waits and after a decline the switch is as it was; an approval turns it on and tells the managers (Hello.QuietPush).
func TestPushGradingSettingChangesTheSwitchOnlyOnApproval(t *testing.T) {
	h := newTestHub(t)
	h.push.store = newGradingStore()
	a := connectAssistant(t, h)
	m := &Machine{cfg: MachineConfig{ID: "box"}, hub: h}
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
	listed := false
	for _, s := range list.Settings {
		if s.Key == pushGradingSelfSettingKey {
			listed = strings.Join(s.Actions, ",") == "get,set" && s.Title != "" && s.About != ""
		}
	}
	if !listed {
		t.Fatalf("settings.list does not offer %s with get and set: %s", pushGradingSelfSettingKey, raw)
	}

	mux := http.NewServeMux()
	h.pushGradingRoutes(mux)
	route := func() bool {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/push/ln-mode", nil))
		var out struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("GET ln-mode: %d %s", rec.Code, rec.Body.String())
		}
		return out.Enabled
	}
	run := func(args string) string {
		t.Helper()
		out, err := h.toolPersonaSettings(ctx, assistantwire.AssistantID, m, "s1", json.RawMessage(args), nil)
		if err != nil {
			out = "error: " + err.Error()
		}
		if pending := h.pendingInteractions(); len(pending) != 0 {
			t.Fatalf("unexpected approval card for %s: %+v", args, pending)
		}
		return out
	}
	get := `{"action":"get","key":"notifications.low_distraction"}`
	set := `{"action":"set","key":"notifications.low_distraction","value":true}`

	if out := run(get); out != "false" || route() {
		t.Fatalf("get = %q, route = %v; want both off", out, route())
	}
	out := run(set)
	wantSummary := i18n.T(h.userLang(), "hub-self_settings_lnmode.change", i18n.Args{
		"before": i18n.T(h.userLang(), "hub-self_settings_lnmode.off"),
		"after":  i18n.T(h.userLang(), "hub-self_settings_lnmode.on"),
		"what":   i18n.T(h.userLang(), "hub-self_settings_lnmode.when_on"),
	})
	if !strings.Contains(out, wantSummary) || !h.push.pushGradingOn() || !route() || !strings.Contains(out, "true") {
		t.Fatalf("direct set: %q, on %v, route %v", out, h.push.pushGradingOn(), route())
	}
	if got := run(get); got != "true" {
		t.Fatalf("get after the change = %q, want what the route says (true)", got)
	}
	eventually(t, 5*time.Second, "the manager told the switch is on", func() bool {
		hello, _ := a.bh.Hello(context.Background())
		return hello.QuietPush
	})
	if out := run(set); !strings.Contains(out, "already") {
		t.Fatalf("setting what it already is: %q", out)
	}
	if out := run(`{"action":"set","key":"notifications.low_distraction","value":"yes"}`); !strings.HasPrefix(out, "error:") || !route() {
		t.Fatalf("a value that is not true or false: %q", out)
	}
}
