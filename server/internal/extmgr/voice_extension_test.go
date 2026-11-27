package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/voice"
)

// voiceTestStore is what the built-in voice extension's tests run on: the hub's store with the extensions' tables.
type voiceTestStore interface {
	Store
	extension.Store
}

// onVoiceStores runs f on the in-memory store, and on Postgres when PERSONA_TEST_DATABASE_URL is set
// (scripts/test/server_test_env.py): the rows and the sealed key of the extensions ids, cleared before and after.
func onVoiceStores(t *testing.T, ids []string, f func(t *testing.T, store voiceTestStore)) {
	t.Run("memory", func(t *testing.T) { f(t, newExtensionStore()) })
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("PERSONA_TEST_DATABASE_URL")
		if url == "" {
			t.Skip("PERSONA_TEST_DATABASE_URL not set (use scripts/test/server_test_env.py)")
		}
		database, err := db.Open(context.Background(), url, db.ManagerSet)
		if err != nil {
			t.Fatal(err)
		}
		clear := func() {
			for _, id := range ids {
				if err := database.DeleteExtension(context.Background(), id); err != nil {
					t.Error(err)
				}
			}
		}
		clear()
		t.Cleanup(func() {
			clear()
			database.Close()
		})
		f(t, database)
	})
}

// voiceEntry is hub.json's entry of `personad voice`, as written before the built-in took it over.
func voiceEntry(id string) ExtensionConfig {
	return ExtensionConfig{ID: id, Name: "语音", Kind: "voice", Command: "personad", Args: []string{"voice"},
		Env: map[string]string{"VOICE_TEST_ENV": "from-hub-json"},
		Config: json.RawMessage(`{"providers":[{"id":"rt","name":"实时服务","url":"wss://rt.example.com/v1/live","models":["m1"],` +
			`"key_command":"printf hub-json-credential"}],"owner":"hub owner"}`)}
}

func voiceHub(t *testing.T, store voiceTestStore, cfgs ...ExtensionConfig) *Manager {
	h := newTestManager(t)
	h.store = store
	startTestExtensions(t, h, cfgs...)
	return h
}

// envOf is the environment the hub starts extension id with.
func envOf(t *testing.T, h *Manager, id string) map[string]string {
	t.Helper()
	p := h.extensions.reg.Get(id)
	s, err := h.extensions.spec(context.Background(), p, &hubExtension{id: id})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, kv := range s.Env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

func viewOf(h *Manager, id string) ExtensionView {
	return h.extensions.view(context.Background(), h.extensions.reg.Get(id))
}

func waitVoiceStatus(t *testing.T, h *Manager, id, status string) ExtensionView {
	t.Helper()
	var v ExtensionView
	waitLong(t, id+" "+status, func() bool {
		v = viewOf(h, id)
		return v.Status == status
	})
	return v
}

func secretSet(out map[string]any, name string) bool {
	secrets, _ := out["secrets"].([]any)
	for _, s := range secrets {
		if m := s.(map[string]any); m["name"] == name {
			return m["set"] == true
		}
	}
	return false
}

// The manifest is one an installed extension could have: it validates, its form is in the subset, the key is a
// secret, and what runs commands or reaches other machines is hub.json's only.
func TestVoiceManifest(t *testing.T) {
	h := newTestManager(t)
	h.version = "1.2.3"
	m := voiceManifest(h, "voice", "")
	raw, _ := json.Marshal(m)
	parsed, err := extension.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Kind() != "voice" || parsed.Name != "实时语音" || !parsed.Permissions.FullHub() || !slices.Equal(parsed.Activation(), []string{extension.OnStartup}) {
		t.Fatalf("%+v", parsed)
	}
	if len(parsed.Secrets) != 1 || parsed.Secrets[0].Name != voiceSecretKey || parsed.Secrets[0].Required {
		t.Fatalf("the key is a secret, not required (hub.json's upstreams need none): %+v", parsed.Secrets)
	}
	schema, err := extension.ParseSchema(parsed.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{`{}`, `{"url":"wss://live.example.com/v1/live","dialect":"codex","models":["gpt-live-1"],"input_rate":24000,"owner":"me"}`,
		`{"url":"ws://gateway.example.com:8080/v1/live"}`} {
		if err := schema.Check(json.RawMessage(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{`{"url":"https://live.example.com"}`, `{"url":"wss://"}`, `{"dialect":"fake"}`, `{"input_rate":8000}`,
		`{"models":["has space"]}`, `{"key_command":"cat ~/.ssh/id_rsa"}`, `{"ssh":"elsewhere"}`, `{"key_file":"/etc/shadow"}`,
		`{"tools":true}`, `{"providers":[]}`, `{"key":"in the config"}`} {
		if err := schema.Check(json.RawMessage(bad)); err == nil {
			t.Errorf("%s: taken", bad)
		}
	}
	if m := voiceManifest(h, "talk", "语音"); m.ID != "talk" || m.Name != "语音" || m.Items("voice")[0] != "talk" {
		t.Fatalf("%+v", m)
	}
}

// hub.json's disabled: always off, whatever the row says; switching it on says why. The hold never reaches the row:
// settings and a key set meanwhile keep the row's switch (or write the default, on, when there was none), so the
// entry runs again once hub.json drops disabled; only switching it off is the owner's choice and kept.
func TestVoiceDisabledInHubJSON(t *testing.T) {
	onVoiceStores(t, []string{"talk", "talk-new", "talk-off"}, func(t *testing.T, store voiceTestStore) {
		ctx := context.Background()
		if err := store.PutExtension(ctx, extension.Record{ID: "talk", Source: extension.SourceBuiltin, Version: "test",
			Manifest: json.RawMessage(`{}`), Enabled: true, InstalledAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		held := func(id string) ExtensionConfig {
			entry := voiceEntry(id)
			entry.Disabled = true
			return entry
		}
		h := voiceHub(t, store, held("talk"))
		if p := h.extensions.reg.Get("talk"); p == nil || p.Record.Source != extension.SourceBuiltin || p.Record.Enabled {
			t.Fatalf("%+v", p)
		}
		if v := viewOf(h, "talk"); v.Status != extensionDisabled {
			t.Fatalf("%+v", v)
		}
		mux := h.routes()
		code, out := call(t, mux, "PUT", "/api/v1/extensions/talk/-/enabled", h.cfg.Token, `{"enabled": true}`)
		if code != 409 || out["error"] != i18n.T(i18n.Default, "hub-voice_extension.disabled_in_hub_json") {
			t.Fatalf("%d %v", code, out)
		}
		// Its settings and key stay editable for when hub.json lets it run, and leave the row's switch as it was.
		if code, out = call(t, mux, "PUT", "/api/v1/extensions/talk/-/config", h.cfg.Token, `{"config": {"owner": "me", "url": "wss://live.example.com/v1/live"}}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		if code, out = call(t, mux, "PUT", "/api/v1/extensions/talk/-/secrets/key", h.cfg.Token, `{"value": "sk-test-voice"}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		rowOn := func(id string) bool {
			t.Helper()
			rows, err := store.Extensions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if r.ID == id {
					return r.Enabled
				}
			}
			t.Fatalf("no row for %s", id)
			return false
		}
		if !rowOn("talk") {
			t.Fatal("the hold reached the row")
		}

		// No row yet: editing writes the default (on), not the hold.
		h2 := voiceHub(t, store, held("talk-new"))
		if code, out = call(t, h2.routes(), "PUT", "/api/v1/extensions/talk-new/-/config", h2.cfg.Token, `{"config": {"owner": "me"}}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		if !rowOn("talk-new") {
			t.Fatal("the hold reached the new row")
		}
		// Switched off while held: that is the owner's choice, kept.
		h3 := voiceHub(t, store, held("talk-off"))
		if code, out = call(t, h3.routes(), "PUT", "/api/v1/extensions/talk-off/-/enabled", h3.cfg.Token, `{"enabled": false}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		if code, out = call(t, h3.routes(), "PUT", "/api/v1/extensions/talk-off/-/config", h3.cfg.Token, `{"config": {"owner": "me"}}`); code != 200 || rowOn("talk-off") {
			t.Fatalf("%d %v", code, out)
		}

		// hub.json drops disabled: on as the rows say, with what was set meanwhile.
		for id, on := range map[string]bool{"talk": true, "talk-new": true, "talk-off": false} {
			again := voiceHub(t, store, voiceEntry(id))
			p := again.extensions.reg.Get(id)
			if p == nil || p.Record.Enabled != on {
				t.Fatalf("%s: %+v", id, p)
			}
			if id == "talk" && (voiceURL(p.Record.Config) == "" || !again.extensions.reg.SecretsSet(ctx, p)[voiceSecretKey]) {
				t.Fatalf("the settings and key set while held: %+v", p.Record)
			}
		}
	})
}

// Without a hub.json entry the built-in is there as "voice", off; with another program's voice entry, it is not.
func TestVoiceWithoutAHubJSONEntry(t *testing.T) {
	onVoiceStores(t, []string{voiceExtensionID}, func(t *testing.T, store voiceTestStore) {
		h := voiceHub(t, store)
		p := h.extensions.reg.Get(voiceExtensionID)
		if p == nil || p.Record.Source != extension.SourceBuiltin || p.Record.Enabled || p.Full.Name != "实时语音" {
			t.Fatalf("%+v", p)
		}
		if v := viewOf(h, voiceExtensionID); v.Status != extensionDisabled || v.Kind != "voice" {
			t.Fatalf("%+v", v)
		}
		if ch, ok := h.voiceChannel(context.Background()); !ok || ch.State != "disabled" {
			t.Fatalf("%+v", ch)
		}
		raw, _, err := h.modelsAppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppPlugins, Method: assistantwire.PluginsVoice})
		if err != nil || string(raw) != `{"plugins":[]}` {
			t.Fatalf("a switched-off voice offers no models to pick: %s %v", raw, err)
		}
	})
	h := voiceHub(t, newExtensionStore(), ExtensionConfig{ID: "voice", Kind: "voice", Command: "/usr/bin/node", Args: []string{"main.mjs"}, Disabled: true})
	if p := h.extensions.reg.Get(voiceExtensionID); p == nil || p.Record.Source != extension.SourceHubJSON {
		t.Fatalf("hub.json's own voice program keeps its id: %+v", p)
	}
}

// flakyStore fails reading the extensions' secrets or deleting one, on demand (a connection reset, a statement
// timeout).
type flakyStore struct {
	*extensionStore
	failRead, failDelete atomic.Bool
}

func (s *flakyStore) ExtensionSecrets(ctx context.Context, id string) (map[string]string, error) {
	if s.failRead.Load() {
		return nil, errors.New("statement timeout")
	}
	return s.extensionStore.ExtensionSecrets(ctx, id)
}

func (s *flakyStore) DeleteExtensionSecret(ctx context.Context, id, name string) error {
	if s.failDelete.Load() {
		return errors.New("connection reset")
	}
	return s.extensionStore.DeleteExtensionSecret(ctx, id, name)
}

// The key follows the address fail closed: a failed read of the secrets still deletes it (and says so, unsure);
// a delete that fails saves nothing; a configuration that cannot be saved after the delete says the key went. Any
// caller of SetConfig is held to it, not only the route.
func TestVoiceKeyFollowsTheAddressFailClosed(t *testing.T) {
	store := &flakyStore{extensionStore: newExtensionStore()}
	h := voiceHub(t, store)
	mux, tok, ctx := h.routes(), h.cfg.Token, context.Background()
	base := "/api/v1/extensions/voice/-/"
	reg := h.extensions.reg
	keyStored := func() bool {
		sealed, _ := store.extensionStore.ExtensionSecrets(ctx, voiceExtensionID)
		return sealed[voiceSecretKey] != ""
	}
	setKey := func(url string) {
		t.Helper()
		if code, out := call(t, mux, "PUT", base+"config", tok, `{"config": {"url": "`+url+`"}}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		if code, out := call(t, mux, "PUT", base+"secrets/key", tok, `{"value": "sk-test-voice"}`); code != 200 || !keyStored() {
			t.Fatalf("%d %v", code, out)
		}
	}

	setKey("wss://a.example.com/v1/live")
	store.failRead.Store(true)
	code, out := call(t, mux, "PUT", base+"config", tok, `{"config": {"url": "wss://b.example.com/v1/live"}}`)
	store.failRead.Store(false)
	if code != 200 || keyStored() || !slices.Equal(toStrings(out["secrets_cleared"]), []string{voiceSecretKey}) {
		t.Fatalf("a failed read keeps no key: %d %v (stored %v)", code, out, keyStored())
	}

	setKey("wss://a.example.com/v1/live")
	store.failDelete.Store(true)
	code, out = call(t, mux, "PUT", base+"config", tok, `{"config": {"url": "wss://b.example.com/v1/live"}}`)
	store.failDelete.Store(false)
	if code != 500 || out["code"] != "secret_not_cleared" || out["secrets_cleared"] != nil || !keyStored() ||
		voiceURL(reg.Config(reg.Get(voiceExtensionID))) != "wss://a.example.com/v1/live" {
		t.Fatalf("a key that cannot be deleted saves nothing: %d %v", code, out)
	}

	store.failPut = true
	code, out = call(t, mux, "PUT", base+"config", tok, `{"config": {"url": "wss://b.example.com/v1/live"}}`)
	store.failPut = false
	if code != 409 || !slices.Equal(toStrings(out["secrets_cleared"]), []string{voiceSecretKey}) || keyStored() ||
		voiceURL(reg.Config(reg.Get(voiceExtensionID))) != "wss://a.example.com/v1/live" {
		t.Fatalf("the key went before the save failed, and the answer says so: %d %v", code, out)
	}

	// Not only the route: any SetConfig, with the read failing too.
	setKey("wss://a.example.com/v1/live")
	store.failRead.Store(true)
	err := reg.SetConfig(ctx, voiceExtensionID, json.RawMessage(`{"url": "wss://b.example.com/v1/live"}`), "assistant")
	store.failRead.Store(false)
	if err != nil || keyStored() {
		t.Fatalf("%v, stored %v", err, keyStored())
	}
	// And a key needs an address whoever sets it.
	if err := reg.SetConfig(ctx, voiceExtensionID, json.RawMessage(`{}`), "assistant"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSecret(ctx, voiceExtensionID, voiceSecretKey, "sk-test-voice", "assistant"); !errors.As(err, new(keyNeedsURLError)) {
		t.Fatalf("%v", err)
	}
}

// The built-in voice extension keeps its id: a package of the same id installed later is refused (so no other
// program is handed the key set on it); one installed before the built-in existed keeps the id instead.
func TestVoiceIDIsNotYielded(t *testing.T) {
	onVoiceStores(t, []string{voiceExtensionID}, func(t *testing.T, store voiceTestStore) {
		ctx := context.Background()
		h := voiceHub(t, store)
		mux, tok := h.routes(), h.cfg.Token
		base := "/api/v1/extensions/voice/-/"
		call(t, mux, "PUT", base+"config", tok, `{"config": {"url": "wss://live.example.com/v1/live"}}`)
		if code, out := call(t, mux, "PUT", base+"secrets/key", tok, `{"value": "sk-test-voice"}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		_, err := h.extensions.reg.Preview(ctx, bundleGraphPackage(t, voiceExtensionID, "1.0.0"), "")
		if err == nil || !strings.Contains(err.Error(), "已经有一个叫 voice 的扩展") {
			t.Fatalf("a package taking the built-in's id: %v", err)
		}
		if p := h.extensions.reg.Get(voiceExtensionID); p == nil || p.Record.Source != extension.SourceBuiltin ||
			!h.extensions.reg.SecretsSet(ctx, p)[voiceSecretKey] {
			t.Fatalf("%+v", p)
		}
	})

	store := newExtensionStore()
	manifest := `{"manifest_version":1,"id":"voice","name":"Other voice","version":"1.0.0","contributes":{"providers":[{"id":"voice-provider","name":"v","base_url":"https://provider.example.com"}]}}`
	if err := store.PutExtension(context.Background(), extension.Record{ID: voiceExtensionID, Source: extension.SourceInstalled, Version: "1.0.0",
		Manifest: json.RawMessage(manifest), InstalledAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	h := voiceHub(t, store)
	if p := h.extensions.reg.Get(voiceExtensionID); p == nil || p.Record.Source != extension.SourceInstalled || h.extensions.voiceID != "" {
		t.Fatalf("installed before: %+v", p)
	}
}

// Rolling back to a personad before the built-in: it reads hub.json's entry as it always did and leaves the built-in's
// row alone (Load takes a built-in row only for a built-in); upgrading again finds the settings and the key there.
func TestVoiceUpgradeRollbackUpgrade(t *testing.T) {
	onVoiceStores(t, []string{"talk"}, func(t *testing.T, store voiceTestStore) {
		h := voiceHub(t, store, voiceEntry("talk"))
		mux := h.routes()
		base := "/api/v1/extensions/talk/-/"
		if code, out := call(t, mux, "PUT", base+"config", h.cfg.Token, `{"config": {"url": "wss://live.example.com/v1/live", "owner": "form owner"}}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		if code, out := call(t, mux, "PUT", base+"secrets/key", h.cfg.Token, `{"value": "sk-test-voice"}`); code != 200 {
			t.Fatalf("%d %v", code, out)
		}

		// The personad before: hub.json's entry as a hub.json extension (newExtensions and Load as they were).
		old := extension.NewRegistry(t.TempDir(), store, hubSealer{h}, 0)
		entry := voiceEntry("talk")
		entry.Name = "语音"
		if err := old.AddHubJSON(legacyManifest(entry), entry, true); err != nil {
			t.Fatal(err)
		}
		if err := old.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
		if p := old.Get("talk"); p == nil || p.Record.Source != extension.SourceHubJSON || !p.Record.Enabled || len(p.Record.Config) != 0 {
			t.Fatalf("rolled back: %+v", p)
		}

		// And the new one again: the same settings, the same key.
		again := voiceHub(t, store, voiceEntry("talk"))
		p := again.extensions.reg.Get("talk")
		if p == nil || p.Record.Source != extension.SourceBuiltin || !p.Record.Enabled || voiceURL(p.Record.Config) != "wss://live.example.com/v1/live" ||
			!again.extensions.reg.SecretsSet(context.Background(), p)[voiceSecretKey] {
			t.Fatalf("upgraded again: %+v", p)
		}
		cfg, err := voice.Merge(voice.Sources{Form: again.extensions.reg.Config(p), Key: "sk", HubJSON: again.extensions.voiceHubJSON()})
		if err != nil || len(cfg.Providers) != 2 || cfg.Providers[0].ID != "rt" || cfg.Providers[1].ID != voice.FormProvider || cfg.Owner != "form owner" {
			t.Fatalf("%+v %v", cfg, err)
		}
	})
}

// Every code the extension may report has words in each language.
func TestVoiceSetupWords(t *testing.T) {
	for _, l := range i18n.Langs {
		for _, key := range []string{"setup_" + voice.SetupURLAndKey, "setup_" + voice.SetupKey, "setup_" + voice.SetupURL,
			"warn_" + voice.WarnFormIDTaken, "warn_" + voice.WarnFormNoKey, "warn_" + voice.WarnDefaultUnknown} {
			if k := "hub-voice_extension." + key; i18n.T(l, k) == k {
				t.Errorf("%s: no %s", l, k)
			}
		}
	}
}

func TestSameOrigin(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{"wss://a.example.com/v1/live", "wss://a.example.com/v2", true},
		{"wss://a.example.com/v1/live", "wss://A.EXAMPLE.com:443/v1/live", true},
		{"ws://gateway.example.com/v1/live", "ws://gateway.example.com:80/x", true},
		{"wss://a.example.com/v1/live", "ws://a.example.com/v1/live", false},
		{"wss://a.example.com/v1/live", "wss://a.example.com:8443/v1/live", false},
		{"wss://a.example.com/v1/live", "wss://b.example.com/v1/live", false},
		{"wss://a.example.com/v1/live", "wss://a.example.com.evil.example/v1/live", false},
		{"wss://a.example.com/v1/live", "", false},
		{"", "", true},
		{"wss://[::1]:9000/v1", "wss://[::1]:9000/v2", true},
	} {
		if got := sameOrigin(tc.a, tc.b); got != tc.same {
			t.Errorf("%q %q: %v", tc.a, tc.b, got)
		}
	}
}

func toStrings(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// When a quota gateway extension is configured with gateway_url (https or loopback http) and an API key, the built-in
// voice extension automatically receives PERSONA_VOICE_GATEWAY_CONFIG and runs without needing separate voice setup.
func TestVoiceLinksQuotaGatewayConfig(t *testing.T) {
	store := newExtensionStore()
	h := voiceHub(t, store)
	ctx := context.Background()
	reg := h.extensions.reg

	gwManifest := `{"manifest_version":1,"id":"quota-relay","name":"Relay","version":"2.3.0",` +
		`"contributes":{"quota":[{"id":"relay","name":"Relay"}]},` +
		`"configuration":{"type":"object","properties":{"gateway_url":{"type":"string","format":"uri"},"key_kind":{"type":"string","default":"management"}}},` +
		`"permissions":{"network":["config:gateway_url"],"launcher_keys":true},"secrets":[{"name":"gateway_key","title":"Key"}]}`
	if err := store.PutExtension(ctx, extension.Record{
		ID:          "quota-relay",
		Source:      extension.SourceInstalled,
		Version:     "2.3.0",
		Manifest:    json.RawMessage(gwManifest),
		Config:      json.RawMessage(`{"gateway_url":"http://127.0.0.1:8080","key_kind":"api"}`),
		Enabled:     true,
		InstalledAt: time.Now(),
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSecret(ctx, "quota-relay", gatewaySecretKey, "gw-api-key-123456", "test"); err != nil {
		t.Fatal(err)
	}

	vp := reg.Get(voiceExtensionID)
	if setup, warns := h.extensions.voiceState(ctx, vp); setup != "" || len(warns) != 0 {
		t.Fatalf("voiceState with quota gateway api key: setup=%q warns=%v", setup, warns)
	}
	env := envOf(t, h, voiceExtensionID)
	var gwProv voice.Provider
	if err := json.Unmarshal([]byte(env["PERSONA_VOICE_GATEWAY_CONFIG"]), &gwProv); err != nil {
		t.Fatalf("PERSONA_VOICE_GATEWAY_CONFIG: %q (%v)", env["PERSONA_VOICE_GATEWAY_CONFIG"], err)
	}
	if gwProv.ID != voice.GatewayProvider || gwProv.Name != "Relay" || gwProv.URL != "ws://127.0.0.1:8080/v1/live" || gwProv.Dialect != voice.DialectCodex || gwProv.Key != "gw-api-key-123456" {
		t.Fatalf("gwProv: %+v", gwProv)
	}
}
