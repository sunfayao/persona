package extmgr

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// extensionSettingsHub is a manager with an extension of every kind of field, one without a form and one set in
// manager.json.
func extensionSettingsHub(t *testing.T) *Manager {
	t.Helper()
	h := newTestManager(t)
	reg := extension.NewRegistry(t.TempDir(), nil, nil, 0)
	add := func(id, name, schema string) {
		m := &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: id, Name: name, Version: "1.0.0",
			Engines: extension.Engines{ExtensionProtocol: extension.Protocol}, Configuration: json.RawMessage(schema)}
		if err := reg.AddBuiltin(m, "/opt/example", true); err != nil {
			t.Fatal(err)
		}
	}
	add("example-chat", "示例聊天", `{"type":"object","properties":{
		"agent":{"type":"boolean","title":"聊天里的 /agent","default":false,"description":"发 /agent 交给助手","x-confirm":"会以你的身份回复"},
		"agent_machine":{"type":"string","format":"machine","title":"在哪台电脑上跑"},
		"agent_launcher":{"type":"string","format":"launcher","title":"用的启动器"},
		"reply_mode":{"type":"string","title":"回复方式","default":"quote","oneOf":[{"const":"quote","title":"引用回复"},{"const":"thread","title":"回复到话题"}]},
		"hours":{"type":"integer","title":"接着多久","minimum":1,"maximum":72,"default":4},
		"allow":{"type":"array","title":"白名单","items":{"type":"string","maxLength":200},"uniqueItems":true},
		"proxy":{"type":"object","title":"代理","properties":{"host":{"type":"string","title":"主机"},"port":{"type":"integer","title":"端口","default":8080}}}}}`)
	add("example-bare", "示例无设置", `{"type":"object","properties":{}}`)
	if err := reg.AddHubJSON(&extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: "example-fixed", Name: "示例固定", Version: "1.0.0",
		Engines: extension.Engines{ExtensionProtocol: extension.Protocol}}, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := reg.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.extensions = newExtensions(t.TempDir(), nil, h.logf)
	h.extensions.reg = reg
	if err := reg.SetConfig(context.Background(), "example-chat", json.RawMessage(`{"hours":8,"allow":["产品群"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	return h
}

// Every configurable extension with a form is extension.<id> in persona_settings; get reads each field with its kind,
// value and default for the model.
func TestSelfSettingsExtensionList(t *testing.T) {
	h := extensionSettingsHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := h.hubCall(ctx, extwire.HubSettingsList, nil)
	raw, _ := json.Marshal(out)
	list := string(raw)
	if err != nil || !strings.Contains(list, `"key":"extension.example-chat","title":"示例聊天的设置"`) || !strings.Contains(list, `"actions":["get","set"]`) ||
		!strings.Contains(list, `"current":"enabled: true"`) {
		t.Fatalf("list: %v\n%s", err, list)
	}
	if strings.Contains(list, "example-bare") || strings.Contains(list, "example-fixed") {
		t.Fatalf("an extension without a form, or set in manager.json, is listed:\n%s", list)
	}
	got, err := h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"get","key":"extension.example-chat"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`- enabled: true or false; now true (the extension itself`,
		`- agent ("聊天里的 /agent"): true or false; now false (the default)`,
		`  发 /agent 交给助手`,
		`- agent_machine ("在哪台电脑上跑"): a machine id (machines_list); now not set`,
		`- agent_launcher ("用的启动器"): a launcher name of this hub; now not set`,
		`- reply_mode ("回复方式"): one of "quote" (引用回复), "thread" (回复到话题); now "quote" (the default)`,
		`- hours ("接着多久"): a whole number from 1 up to 72; now 8`,
		`- allow ("白名单"): a list, each text of at most 200 characters; now ["产品群"]`,
		`- proxy ("代理"): a group of these fields`,
		`  - port ("端口"): a whole number; now 8080 (the default)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("get lacks %q:\n%s", want, got)
		}
	}
	// manager.json's is not reachable by key either.
	if _, err := h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"get","key":"extension.example-fixed"}`)); err == nil {
		t.Fatal("manager.json's extension read through persona_settings")
	}
}

// set is held to the schema before any card, shows each changed field before → after (with the sentence a switch
// asks before it is turned on), and only approved saves exactly those fields; null puts one back to its default.
func TestSelfSettingsExtensionSet(t *testing.T) {
	h := extensionSettingsHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	th := hubOf(h)
	var asked []proto.Interaction
	allow := true
	th.mu.Lock()
	th.answer = func(ix proto.Interaction) proto.Resolution {
		asked = append(asked, ix)
		a := allow
		return proto.Resolution{Allow: &a}
	}
	th.mu.Unlock()
	set := func(value string) (string, error) {
		return h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"set","key":"extension.example-chat","value":`+value+`}`))
	}
	config := func() map[string]any {
		var c map[string]any
		_ = json.Unmarshal(h.extensions.reg.SavedConfig(h.extensions.reg.Get("example-chat")), &c)
		return c
	}

	// Refused before a card: not an object, a field it doesn't have, a value outside the schema.
	for _, bad := range []string{`true`, `{"agnet":true}`, `{"hours":100}`, `{"reply_mode":"email"}`, `{"agent_launcher":""}`,
		`{"proxy":{"user":"me"}}`, `{"proxy":"host"}`} {
		if _, err := set(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	if _, err := set(`{"hours":100}`); err == nil || !strings.Contains(err.Error(), "hours") {
		t.Errorf("the refusal says which field: %v", err)
	}
	if len(asked) != 0 {
		t.Fatalf("a card for a refused value: %+v", asked)
	}
	// The same values: no card.
	if out, err := set(`{"hours":8}`); err != nil || !strings.Contains(out, "没有变化") || len(asked) != 0 {
		t.Fatalf("unchanged: %q %v %d", out, err, len(asked))
	}

	// Non-secret fields apply directly with no card, returning before → after and preserving untouched fields.
	out, err := set(`{"agent":true,"reply_mode":"thread"}`)
	if err != nil || !strings.Contains(out, "updated") || len(asked) != 0 {
		t.Fatalf("direct set: %q %v asked=%d", out, err, len(asked))
	}
	if want := "改聊天里的 /agent：关（默认） → 开\n会以你的身份回复\n改回复方式：引用回复（默认） → 回复到话题"; !strings.Contains(out, want) {
		t.Fatalf("result lacks before→after\n%s\nwant\n%s", out, want)
	}
	if c := config(); c["agent"] != true || c["reply_mode"] != "thread" || c["hours"] != float64(8) {
		t.Fatalf("saved: %v", c)
	}

	out, err = set(`{"agent_launcher":"work_gw","agent_machine":"studio","proxy":{"host":"proxy.example.com"}}`)
	if err != nil || !strings.Contains(out, "updated") || len(asked) != 0 {
		t.Fatalf("second set: %q %v asked=%d", out, err, len(asked))
	}
	if want := "改在哪台电脑上跑：（无） → studio\n改用的启动器：（无） → work_gw\n改代理 · 主机：（无） → proxy.example.com"; !strings.Contains(out, want) {
		t.Fatalf("result lacks before→after\n%s\nwant\n%s", out, want)
	}
	c := config()
	if c["agent"] != true || c["agent_launcher"] != "work_gw" || c["agent_machine"] != "studio" || c["hours"] != float64(8) ||
		c["proxy"].(map[string]any)["host"] != "proxy.example.com" || len(c["allow"].([]any)) != 1 {
		t.Fatalf("saved: %v", c)
	}
	// What the extension gets has the defaults filled in.
	var eff map[string]any
	_ = json.Unmarshal(h.extensions.reg.Config(h.extensions.reg.Get("example-chat")), &eff)
	if eff["agent"] != true || eff["reply_mode"] != "thread" || eff["proxy"].(map[string]any)["port"] != float64(8080) {
		t.Fatalf("effective: %v", eff)
	}

	// null puts a field back to its default; a list joined.
	out, err = set(`{"agent":null,"hours":null,"allow":["产品群","值班群"]}`)
	if err != nil || !strings.Contains(out, "updated") || len(asked) != 0 {
		t.Fatalf("reset: %q %v asked=%d", out, err, len(asked))
	}
	if want := "改聊天里的 /agent：开 → 关（默认）\n改接着多久：8 → 4（默认）\n改白名单：产品群 → 产品群、值班群"; !strings.Contains(out, want) {
		t.Fatalf("reset lacks before→after\n%s\nwant\n%s", out, want)
	}
	if c := config(); c["agent"] != nil || c["hours"] != nil || len(c["allow"].([]any)) != 2 || c["agent_launcher"] != "work_gw" {
		t.Fatalf("after reset: %v", c)
	}
}

// An extension's own switch (enabled) is persona_settings' too, apart from its settings: one set for a Mac but switched
// off shows enabled: false in list and get; set {"enabled": true} asks on the phone first, and only approved switches it
// as the Settings page does, which tells the running conversations (ToolsChanged), the answer naming the tools it brought.
// Declined or unanswered, nothing changes; switching it off, here with one of its settings, goes the same way.
func TestSelfSettingsExtensionSwitch(t *testing.T) {
	h, _ := extensionTestHub(t)
	var changes atomic.Int64
	h.opts.ToolsChanged = func() { changes.Add(1) }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.extensions.reg.SetConfig(ctx, egoExtensionID, json.RawMessage(`{"machines":["mac-example"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	on := func() bool { return h.extensions.reg.Snapshot(egoExtensionID).Record.Enabled }
	listed := func() string {
		for _, s := range h.settingsList(ctx) {
			if s.Key == "extension."+egoExtensionID {
				return s.Current
			}
		}
		return "not listed"
	}
	got, err := h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"get","key":"extension.ego"}`))
	if err != nil || !strings.Contains(got, "- enabled: true or false; now false (the extension itself") ||
		!strings.Contains(got, `now ["mac-example"]`) || listed() != "enabled: false" {
		t.Fatalf("get: %v\n%s\nlist: %s", err, got, listed())
	}

	th := hubOf(h)
	var asked []proto.Interaction
	var answer proto.Resolution
	th.mu.Lock()
	th.answer = func(ix proto.Interaction) proto.Resolution {
		asked = append(asked, ix)
		return answer
	}
	th.mu.Unlock()
	set := func(value string) (string, error) {
		return h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"set","key":"extension.ego","value":`+value+`}`))
	}
	// Refused before a card: a switch that is not true or false. Already off: said so, and no card.
	for _, bad := range []string{`{"enabled":"true"}`, `{"enabled":1}`, `{"enabled":null}`} {
		if _, err := set(bad); err == nil || !strings.Contains(err.Error(), "enabled takes true or false") {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if out, err := set(`{"enabled":false}`); err != nil || out != "浏览器（ego lite） is already switched off (enabled: false): nothing to change." {
		t.Fatalf("already off: %q %v", out, err)
	}
	if len(asked) != 0 {
		t.Fatalf("a card with nothing to change: %+v", asked)
	}

	before := changes.Load()
	out, err := set(`{"enabled":true}`)
	if err != nil || len(asked) != 0 ||
		!strings.Contains(out, "改浏览器（ego lite）：关 → 开") ||
		!strings.Contains(out, "浏览器（ego lite） is switched on. New tools: browser_look, browser_run. Running conversations are sent their new tool list.") ||
		!on() || h.browserTools() == nil || changes.Load() == before || listed() != "enabled: true" {
		t.Fatalf("switched on: %q %v, asked=%d, on %v, list %s", out, err, len(asked), on(), listed())
	}
	before = changes.Load()
	out, err = set(`{"enabled":false,"confirm":true}`)
	if err != nil || len(asked) != 0 ||
		!strings.Contains(out, "改浏览器（ego lite）：开 → 关\n改操作前在手机上确认：关（默认） → 开") ||
		!strings.Contains(out, "浏览器（ego lite） is switched off. Tools no longer offered: browser_look, browser_run.") ||
		on() || h.browserTools() != nil || changes.Load() == before {
		t.Fatalf("switched off: %q %v, asked=%d, on %v", out, err, len(asked), on())
	}
	var c map[string]any
	_ = json.Unmarshal(h.extensions.reg.SavedConfig(h.extensions.reg.Get(egoExtensionID)), &c)
	if c["confirm"] != true || len(c["machines"].([]any)) != 1 {
		t.Fatalf("saved: %v", c)
	}
}
