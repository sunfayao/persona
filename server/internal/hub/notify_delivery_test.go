package hub

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

func TestDeliverNotifyNoDevice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Hub)
	}{
		{"no pusher", func(h *Hub) { h.push = nil }},
		{"no registration", func(*Hub) {}},
		{"preferences without a connection", func(h *Hub) {
			if err := h.push.setPrefs("phone", NotifyPrefs{Input: false}); err != nil {
				t.Fatal(err)
			}
		}},
		{"client without notify capability", func(h *Hub) {
			addTestClient(h, &client{out: make(chan []byte, 1)})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			tc.setup(h)
			got, err := h.deliverNotify(notifyRequest{Title: "Build finished", Level: levelActive}, "", "", "")
			if err != nil || got["sent"] != false || got["reason"] != "no_device" {
				t.Fatalf("notify = %v, %v; want not sent with no_device", got, err)
			}
		})
	}
}

func TestDeliverNotifyFiltered(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prefs   NotifyPrefs
		desktop bool
	}{
		{name: "needs you off", prefs: NotifyPrefs{Input: false}},
		{name: "manager muted", prefs: NotifyPrefs{Input: true, MutedManagers: []string{"manager-example"}}},
		{name: "phone muted", prefs: NotifyPrefs{Input: true, MuteWhileDesktop: true}, desktop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			addTestClient(h, &client{device: "phone", out: make(chan []byte, 1), meta: clientMeta{features: map[string]bool{"ws_notify": true}}})
			if tc.desktop {
				addTestClient(h, &client{desktop: true})
			}
			h.push.desktopOnline = h.desktopOnline
			if err := h.push.setPrefs("phone", tc.prefs); err != nil {
				t.Fatal(err)
			}
			got, err := h.deliverNotify(notifyRequest{Title: "Build finished", Level: levelActive}, "", "", "manager-example")
			if err != nil || got["sent"] != false || got["reason"] != "filtered" {
				t.Fatalf("notify = %v, %v; want not sent with filtered", got, err)
			}
			for c := range h.clients {
				if len(c.out) != 0 {
					t.Fatal("filtered notification reached a WebSocket")
				}
			}
		})
	}
}

func TestNotifyWSRecipientCounts(t *testing.T) {
	h := newTestHub(t)
	for _, id := range []string{"selected", "filtered", "legacy"} {
		c := &client{device: id, out: make(chan []byte, 1), meta: clientMeta{features: map[string]bool{"ws_notify": id != "legacy"}}}
		addTestClient(h, c)
		if err := h.push.setPrefs(id, NotifyPrefs{Input: id != "filtered"}); err != nil {
			t.Fatal(err)
		}
	}
	build := func(i18n.Lang) map[string]any {
		return notifyPayload(notifyRequest{Title: "Build finished", Level: levelActive}, "", "")
	}
	reach := h.notifyWSFrom("", kindAlert, build, pushWants{Kind: "input"}.fn(), quietNone)
	if reach.selected != 1 || reach.reachable != 2 {
		t.Fatalf("WebSocket counts = %d, %d; want 1, 2", reach.selected, reach.reachable)
	}
	for c := range h.clients {
		if c.device == "selected" && len(c.out) == 0 {
			t.Fatal("selected WebSocket got no notification")
		}
	}
}

// An extension's id replaces only that extension's earlier alert: the same id from another extension, from the same
// extension of another manager, or from a user's credential goes out under an id of its own, and no caller's own id can
// be the extension's. A user's id goes out as it came.
func TestDeliverNotifyExtensionIDIsTheExtensions(t *testing.T) {
	h := newTestHub(t)
	c := &client{device: "android", out: make(chan []byte, 1), meta: clientMeta{features: map[string]bool{"ws_notify": true}}}
	addTestClient(h, c)
	if err := h.push.setPrefs("android", NotifyPrefs{Input: true}); err != nil {
		t.Fatal(err)
	}
	send := func(extID, manager, id string) string {
		t.Helper()
		got, err := h.deliverNotify(notifyRequest{Title: "Build finished", Level: levelActive, ID: id}, "", extID, manager)
		if err != nil || got["sent"] != true {
			t.Fatalf("notify from %q of %q: %v, %v", extID, manager, got, err)
		}
		var m map[string]any
		if err := json.Unmarshal(<-c.out, &m); err != nil {
			t.Fatal(err)
		}
		ws, _ := m["id"].(string)
		return ws
	}
	ws := send("gateway", "manager-a", "progress")
	if !strings.HasPrefix(ws, "ext:") || len(ws) <= notifyIDMost {
		t.Fatalf("an extension's id: WebSocket %q", ws)
	}
	if againWS := send("gateway", "manager-a", "progress"); againWS != ws {
		t.Fatalf("the same id again: WebSocket %q; want %q", againWS, ws)
	}
	for _, other := range [][2]string{{"other", "manager-a"}, {"gateway", "manager-b"}} {
		if otherWS := send(other[0], other[1], "progress"); otherWS == ws {
			t.Fatalf("%s of %s: WebSocket %q, the same as gateway of manager-a's", other[0], other[1], otherWS)
		}
	}
	if userWS := send("", "manager-a", "progress"); userWS != "progress" {
		t.Fatalf("a user's id: WebSocket %q, want it as it came", userWS)
	}
	if noneWS := send("gateway", "manager-a", ""); noneWS != "" {
		t.Fatalf("an extension's alert without an id: WebSocket %q", noneWS)
	}
	idOf := func(manager, extension, id string) string { return extensionNotifyIDs(manager, extension, id) }
	if idOf("ab", "c", "d") == idOf("a", "bc", "d") || idOf("a", "b", "cd") == idOf("a", "bc", "d") {
		t.Fatal("the manager, the extension and the id are not kept apart")
	}
}
