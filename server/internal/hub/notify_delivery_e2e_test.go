package hub

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// notify_user over the real hub stack (#8138): the hub's HTTP and WebSocket, the assistant manager over the bus with
// its state in Postgres, a fake node for the assistant's machine. With no device that takes notifications the tool
// says the user was not alerted and records why. With an app connected it reports delivery and records that outcome.
func TestNotifyUserSaysWhetherADeviceGotItOverTheBus(t *testing.T) {
	h, srv, _ := newBusHub(t)
	store := managerDB(t, h)
	ctx := context.Background()
	keys := []string{"assistant", "assistant.tasks", "assistant.routines", "assistant.followups", "assistant.runs"}
	clear := func() {
		for _, k := range keys {
			_ = store.PutSetting(context.Background(), k, nil)
		}
	}
	clear()
	t.Cleanup(clear)
	n := newAssistantNode(t, h)
	n.m.info.Home = "/home/me"
	editSessions(n.m, func([]proto.Session) []proto.Session {
		return []proto.Session{{SessionID: "main-1", Cwd: convDir, Title: "Main", UpdatedAt: time.Now().UTC()}}
	})
	n.known["main-1"] = true
	if err := store.PutSetting(ctx, "assistant", assistant.Settings{MachineID: "server", Main: "main-1", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	dialAssistant(t, h, srv.URL, false)

	type reading struct {
		delivered int
		noDevice  int
	}
	since := time.Now()
	delivery := func() reading {
		t.Helper()
		events, err := store.WorkEventsBetween(ctx, since, time.Now(), []string{"notify_user"})
		if err != nil {
			t.Fatal(err)
		}
		var result reading
		for _, event := range events {
			if event.SessionID != "main-1" {
				continue
			}
			switch event.Detail["missed"] {
			case "no_device":
				result.noDevice++
			case nil, "":
				result.delivered++
			}
		}
		return result
	}
	notify := func(title string) string {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"title": title, "body": "The report is ready."})
		params, _ := json.Marshal(map[string]any{"name": "notify_user", "args": json.RawMessage(raw), "session_id": "main-1", "assistant": true})
		res, err := nodeAsks(ctx, n.m, "mcp.call", params)
		if err != nil {
			t.Fatal(err)
		}
		r := res.(proto.ToolResult)
		if r.IsError {
			t.Fatalf("notify_user: %+v", r)
		}
		return textOf(r)
	}
	inAnyLanguage := func(key string, args ...i18n.Args) []string {
		var out []string
		for _, l := range []i18n.Lang{"en", "zh-Hans", "ja"} {
			out = append(out, i18n.T(l, key, args...))
		}
		return out
	}

	// No phone, computer or browser: not "Notification sent.", and the model is told the user was not alerted.
	got := notify("Nobody gets this")
	if want := inAnyLanguage("assistant-notify.no_device_notice"); !slices.Contains(want, got) {
		t.Fatalf("with no device notify_user said %q, want one of %q", got, want)
	}

	// An app that takes notify events: it gets it, and the tool says one device did.
	app := dialTestApp(t, h, srv)
	got = notify("The app gets this")
	if want := inAnyLanguage("assistant-notify.sent_notice", i18n.Args{"count": 1}); !slices.Contains(want, got) {
		t.Fatalf("with an app notify_user said %q, want one of %q", got, want)
	}
	app.wait(t, "the push", func(m map[string]any) bool {
		return m["type"] == "notify" && m["title"] == "The app gets this"
	})

	var after reading
	eventually(t, 10*time.Second, "both notification outcomes recorded", func() bool {
		after = delivery()
		return after.delivered >= 1 && after.noDevice >= 1
	})
	if after.delivered != 1 || after.noDevice != 1 {
		t.Fatalf("notification outcomes: %+v, want one delivered and one with no device", after)
	}
}
