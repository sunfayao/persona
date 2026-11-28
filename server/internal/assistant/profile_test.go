package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The settings are the assistant's: hub.json's (the hello) are their defaults, a saved machine the hub no longer has
// is not used, and the hub gets what its features need in every snapshot (the profile, with the conversations its
// nodes keep listing).
func TestSettingsAndTheProfile(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	f.hello.Assistant.Launcher, f.hello.OwnerNotes = "hub-l", "  hub.json 的备注 "
	s := newTestService(t, f)
	if v := s.settings(ctx); v.MachineID != "server" || v.Launcher != "hub-l" || s.notes(ctx) != "hub.json 的备注" || !s.mainTasksQuiet(ctx) {
		t.Fatalf("defaults: %+v %q", v, s.notes(ctx))
	}
	notes := "我自己写的"
	setSettings(t, s, func(v *Settings) {
		v.MachineID, v.Main, v.Notes, v.Timezone = "gone", "main-1", &notes, "Asia/Shanghai"
		v.Former = []FormerMain{{MachineID: "box", SessionID: "old-1"}}
	})
	if v := s.settings(ctx); v.MachineID != "server" || s.notes(ctx) != notes || s.location(ctx).String() != "Asia/Shanghai" {
		t.Fatalf("saved: %+v", v)
	}
	s.publish(ctx)
	p := f.lastSnapshot().Profile
	want := []SessionRef{{MachineID: "server", SessionID: "main-1"}, {MachineID: "box", SessionID: "old-1"}}
	if p == nil || p.MachineID != "server" || p.Main != "main-1" || p.Launcher != "hub-l" || p.Timezone != "Asia/Shanghai" ||
		!slices.Equal(p.Keep, want) {
		t.Fatalf("profile: %+v", p)
	}
	// The main conversation is "Persona" on the lists the apps get.
	if i := slices.IndexFunc(f.lastSnapshot().Sessions, func(v SessionView) bool { return v.SessionID == "main-1" }); i < 0 ||
		f.lastSnapshot().Sessions[i].Title != mainTitle {
		t.Fatalf("sessions: %+v", f.lastSnapshot().Sessions)
	}
}

// PUT /api/v1/assistant checks what it is given (a machine the hub has, a time zone, a launcher that runs Claude
// Code, Codex or an assistant runtime), puts a launcher into Model configuration › Main chat too, and answers the view.
func TestPutSettings(t *testing.T) {
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "server", Name: "server", Kind: "local", Home: "/home/me"}}
	f.hello.Assistant.Machines = []string{"server"}
	s := newTestService(t, f)
	put := func(body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(body)))
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	for _, bad := range []string{`{"machine_id":"nowhere"}`, `{"timezone":"Mars/Base"}`, `{"launcher":"gemini-l"}`} {
		if code, out := put(bad); code != http.StatusBadRequest {
			t.Fatalf("%s: %d %v", bad, code, out)
		}
	}
	code, out := put(`{"launcher":"codex-l","timezone":"Asia/Shanghai","main":"m1","main_tasks_quiet":false,"notes":"关于我"}`)
	if code != http.StatusOK || out["launcher"] != "codex-l" || out["timezone"] != "Asia/Shanghai" || out["main"] != "m1" ||
		out["main_tasks_quiet"] != false || out["notes_effective"] != "关于我" || out["cwd"] != "/home/me/.persona/assistant" {
		t.Fatalf("PUT: %d %v", code, out)
	}
	if r := modelRecordOf(t, s, "assistant"); r.Binding.Provider != "launcher.codex-l" {
		t.Fatalf("模型配置 › 主对话: %+v", r)
	}
	if p := f.lastSnapshot().Profile; p == nil || p.Main != "m1" || p.Timezone != "Asia/Shanghai" {
		t.Fatalf("the hub is told: %+v", p)
	}
	if code, out := put(`{}`); code != http.StatusOK || out["main_background_folded"] != true {
		t.Fatalf("main_background_folded default: %d %v", code, out["main_background_folded"])
	}
	if code, out := put(`{"main_background_folded":false}`); code != http.StatusOK || out["main_background_folded"] != false {
		t.Fatalf("main_background_folded off: %d %v", code, out["main_background_folded"])
	}
	if code, out := put(`{"main":"-"}`); code != http.StatusOK || out["main"] != nil {
		t.Fatalf("clear main: %d %v", code, out)
	}
}

// A zone picked by hand stays (#1855): before any pick every write lands, as before; once a zone is picked, a
// device's own sync (a zone without timezone_picked, as every app sent before) leaves it and the answer still says
// it, while a new pick or going back to the devices' zone (false) lands; the hub's own reading follows.
func TestPickedTimezoneStays(t *testing.T) {
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "server", Name: "server", Kind: "local", Home: "/home/me"}}
	f.hello.Assistant.Machines = []string{"server"}
	s := newTestService(t, f)
	put := func(body string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(body)))
		out := map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
		return out
	}
	steps := []struct {
		body, zone string
		picked     any // the answer's timezone_picked (nil: not there)
	}{
		{`{"timezone":"America/New_York"}`, "America/New_York", nil},                     // never picked: a device's sync lands
		{`{"timezone":"Europe/Berlin","timezone_picked":true}`, "Europe/Berlin", true},   // picked on Android
		{`{"timezone":"America/New_York"}`, "Europe/Berlin", true},                       // an iPhone's sync: kept
		{`{"notes":"关于我"}`, "Europe/Berlin", true},                                       // another setting: kept
		{`{"timezone":"Asia/Tokyo","timezone_picked":true}`, "Asia/Tokyo", true},         // another pick lands
		{`{"timezone_picked":true}`, "Asia/Tokyo", true},                                 // a pick without a zone keeps it
		{`{"timezone":"Asia/Shanghai","timezone_picked":false}`, "Asia/Shanghai", false}, // Match phone again
		{`{"timezone":"America/New_York"}`, "America/New_York", false},                   // the devices' syncs land again
	}
	for _, st := range steps {
		out := put(st.body)
		if out["timezone"] != st.zone || out["timezone_picked"] != st.picked {
			t.Fatalf("%s: timezone %v, timezone_picked %v; want %s, %v", st.body, out["timezone"], out["timezone_picked"], st.zone, st.picked)
		}
		if got := s.settings(context.Background()).Timezone; got != st.zone {
			t.Fatalf("%s: the hub reads %s", st.body, got)
		}
	}
}

// Read-only devices (an answer-only watch) may view the assistant but cannot change its settings or write/delete
// memory: the three mutating routes answer 403 before touching anything. No role (a test's own request) and "full"
// keep working, and GET stays open.
func TestSettingsRequireFullRole(t *testing.T) {
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "server", Name: "server", Kind: "local", Home: "/home/me"}}
	f.hello.Assistant.Machines = []string{"server"}
	s := newTestService(t, f)
	do := func(method, path, role, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if role != "" {
			r.Header.Set(RoleHeader, role)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec.Code
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodPut, "/api/v1/assistant", `{}`},
	} {
		// Read-only devices and extensions are stopped by the role; full devices go through (the fake serves
		// the setting, so it answers 200).
		for _, role := range []string{"answer", roleExtension} {
			if code := do(tc.method, tc.path, role, tc.body); code != http.StatusForbidden {
				t.Fatalf("%s %s as %s: %d, want 403", tc.method, tc.path, role, code)
			}
		}
		if code := do(tc.method, tc.path, "full", tc.body); code != http.StatusOK {
			t.Fatalf("%s %s as full: %d, want 200", tc.method, tc.path, code)
		}
	}
	// No role header is a full-role request; the empty PUT is a valid no-op settings change.
	if code := do(http.MethodPut, "/api/v1/assistant", "", `{}`); code != http.StatusOK {
		t.Fatalf("PUT without a role header: %d, want 200", code)
	}
	// Reading the settings stays open to a read-only device.
	for _, path := range []string{"/api/v1/assistant"} {
		if code := do(http.MethodGet, path, "answer", ""); code != http.StatusOK {
			t.Fatalf("GET %s as answer: %d, want 200", path, code)
		}
	}
}

// Renaming one of its conversations: not the main one (400), a Codex one only in the assistant's titles.
func TestRename(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	f.setSessions("server", proto.Session{SessionID: "cx", Agent: "codex", Assistant: true}, proto.Session{SessionID: "cl", Assistant: true})
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.Main = "main-1" })
	if _, err := s.Rename(ctx, RenameParams{MachineID: "server", SessionID: "main-1", Params: map[string]any{"title": "x"}}); err == nil ||
		!strings.Contains(err.Error(), "主对话") {
		t.Fatalf("main: %v", err)
	}
	if _, err := s.Rename(ctx, RenameParams{MachineID: "server", SessionID: "cx", Params: map[string]any{"title": "codex 的"}}); err != nil || f.sent() != 0 {
		t.Fatalf("codex: %v, %d node calls", err, f.sent())
	}
	if _, err := s.Rename(ctx, RenameParams{MachineID: "server", SessionID: "cl", Params: map[string]any{"title": "claude 的"}}); err != nil || f.sent() != 1 {
		t.Fatalf("claude: %v, %d node calls", err, f.sent())
	}
	s.publish(ctx)
	titles := map[string]string{}
	for _, v := range f.lastSnapshot().Sessions {
		titles[v.SessionID] = v.Title
	}
	if titles["cx"] != "codex 的" || titles["cl"] != "claude 的" {
		t.Fatalf("titles: %v", titles)
	}
}

// persona_settings: the list has the assistant's settings and the hub's; the assistant's own are read, and changed
// once the user approves on the phone (its own card); the hub's go to the hub.
func TestSettingsTool(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	f.launcherList = []proto.Launcher{{Name: "claude"}, {Name: "fast", Model: "m-fast"}}
	s := newTestService(t, f)
	setModel(t, s, "assistant", ModelBinding{Provider: "launcher.claude", Model: "chosen", Effort: "high"})
	tool := func(args string) string {
		r := s.Tool(ctx, ToolCall{Name: "persona_settings", Arguments: json.RawMessage(args), MachineID: "server", SessionID: "c1"})
		return r.Content[0].Text
	}
	if out := tool(`{}`); !strings.Contains(out, "- assistant.launcher (Persona 的启动器)") || !strings.Contains(out, "- inbox.rules (收件箱分拣规则)") ||
		strings.Index(out, "assistant.notes") > strings.Index(out, "inbox.rules") {
		t.Fatalf("list:\n%s", out)
	}
	if out := tool(`{"action":"get","key":"inbox.rules"}`); !strings.HasPrefix(out, "the hub's: ") {
		t.Fatalf("a hub setting: %s", out)
	}
	if out := tool(`{"action":"set","key":"assistant.notes","value":"示例备注"}`); len(s.pendingQuestions()) != 0 ||
		!strings.Contains(out, "→ 示例备注") || !strings.Contains(out, "updated") || s.notes(ctx) != "示例备注" {
		t.Fatalf("notes direct set: %s (pending=%d)", out, len(s.pendingQuestions()))
	}
	if saved, _ := s.savedSettings(ctx); saved.Launcher != "" {
		t.Fatal("notes changed legacy model inheritance")
	}
	if out := tool(`{"action":"set","key":"assistant.launcher","value":"fast"}`); len(s.pendingQuestions()) != 0 || !strings.Contains(out, "fast") ||
		modelRecordOf(t, s, "assistant").Binding != (ModelBinding{Provider: "launcher.fast"}) {
		t.Fatalf("launcher: %s %+v", out, modelRecordOf(t, s, "assistant"))
	}
	if out := tool(`{"action":"set","key":"assistant.launcher","value":"nope"}`); !strings.Contains(out, "no launcher") {
		t.Fatalf("unknown launcher: %s", out)
	}
	if out := tool(`{"action":"get","key":"assistant.main_tasks_quiet"}`); out != "true" {
		t.Fatalf("quiet: %s", out)
	}
}

// persona_settings' on/off settings (setBoolSetting): the same value changes nothing; another applies directly with
// before → after and no card; a value that is not a bool is refused.
func TestBoolSettingsSet(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, newFakeHub().withMachine())
	tool := func(args string) (string, bool) {
		r := s.Tool(ctx, ToolCall{Name: "persona_settings", Arguments: json.RawMessage(args), MachineID: "server", SessionID: "c1"})
		return r.Content[0].Text, r.IsError
	}
	word := map[bool]string{true: i18n.T(i18n.Default, "assistant-settings_tool.on"), false: i18n.T(i18n.Default, "assistant-settings_tool.off")}
	for _, c := range []struct {
		name string
		def  bool
	}{{"main_tasks_quiet", true}, {"start_requested_tasks", true}, {"interject_background", false}, {"agent_tasks", false}, {"close_ended_terminals", false}, {"main_background_folded", true}, {"inbox_signals", false}} {
		key := "assistant." + c.name
		get := func() string { out, _ := tool(`{"action":"get","key":"` + key + `"}`); return out }
		set := func(value string) (string, bool) {
			return tool(`{"action":"set","key":"` + key + `","value":` + value + `}`)
		}
		if out := get(); out != fmt.Sprint(c.def) {
			t.Fatalf("%s default: %s", key, out)
		}
		if out, isErr := set(fmt.Sprint(c.def)); isErr || !(strings.Contains(out, "没有变化") || strings.Contains(out, "is unchanged")) || len(s.pendingQuestions()) != 0 {
			t.Fatalf("%s set to what it is: %s", key, out)
		}
		if out, isErr := set(`"yes"`); !isErr || !strings.Contains(out, "true or false") {
			t.Fatalf("%s set to a string: %s", key, out)
		}
		summary := i18n.T(i18n.Default, "assistant-settings_tool.change_"+c.name, i18n.Args{"before": word[c.def], "after": word[!c.def]})
		if out, isErr := set(fmt.Sprint(!c.def)); isErr || len(s.pendingQuestions()) != 0 ||
			!strings.Contains(out, summary) || !strings.Contains(out, key+" is "+fmt.Sprint(!c.def)) || get() != fmt.Sprint(!c.def) {
			t.Fatalf("%s direct set: %s (err=%v, pending=%d)", key, out, isErr, len(s.pendingQuestions()))
		}
	}
	// The legacy keys left persona_settings: delivery_gate is the one switch, and main supervises its tasks. A stored
	// done_needs_delivery is still read (deliveryGateOf), but nothing sets it any more.
	for _, key := range []string{"assistant.done_needs_delivery", "assistant.main_supervises_own_tasks", "assistant.background_supervisor_keeps_context"} {
		if out, _ := tool(`{"action":"list"}`); strings.Contains(out, key) {
			t.Errorf("persona_settings still lists %s", key)
		}
	}
}

// The agents of open todos are kept listed however long they have been quiet (#4510): a busy machine's newest
// transcripts crowded a finished, exited task agent out of the list and its task showed "Starting Agent" for ever. A
// done todo's or a released agent is not kept.
func TestProfileKeepsOpenTodoAgents(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.Main = "main-1" })
	f.setSessions("box", proto.Session{SessionID: "a1", Status: proto.StatusIdle}, proto.Session{SessionID: "a2", Status: proto.StatusIdle},
		proto.Session{SessionID: "a3", Status: proto.StatusIdle})
	e := &todoEnv{t: t, s: s, f: f}
	open, done, released := e.create("Open"), e.create("Done"), e.create("Released")
	e.must("PUT", "/api/v1/todos/"+open.ID+"/agents/box/a1", "")
	e.must("PUT", "/api/v1/todos/"+done.ID+"/agents/box/a2", "")
	e.must("PUT", "/api/v1/todos/"+released.ID+"/agents/box/a3", "")
	e.must("PATCH", "/api/v1/todos/"+done.ID, `{"done":true}`)
	e.must("DELETE", "/api/v1/todos/"+released.ID+"/agents/box/a3", "")
	s.publish(ctx)
	p := f.lastSnapshot().Profile
	want := []SessionRef{{MachineID: "server", SessionID: "main-1"}, {MachineID: "box", SessionID: "a1"}}
	if p == nil || !slices.Equal(p.Keep, want) {
		t.Fatalf("keep: %+v", p)
	}
}
