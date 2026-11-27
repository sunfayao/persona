package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/connector/serve"
	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/proto"
)

// notesConnector is the connector an installed test plugin contributes: signed in with the API key the user filled
// in (a declared secret), its folders from the plugin's configuration.
type notesConnector struct {
	key     string
	folders []string
}

func (notesConnector) ID() string   { return "examplenotes" }
func (notesConnector) Name() string { return "Example Notes" }
func (notesConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools}, Hosts: []string{"notes.example.com"}}
}
func (n notesConnector) Status(context.Context) connector.Status {
	if n.key == "" {
		return connector.Status{State: connector.StateNotConfigured}
	}
	return connector.Status{State: connector.StateOK, Account: "key " + n.key[:2] + "…"}
}
func (n notesConnector) Tools() []proto.Tool { return []proto.Tool{{Name: "examplenotes_search"}} }
func (n notesConnector) Call(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return fmt.Sprintf("%s in %s", name, strings.Join(n.folders, ",")), nil
}

func serveConnectorExtensionForTest() {
	_ = serve.MainWith(context.Background(), os.Stdin, os.Stdout, func(i serve.Init, _ connector.Secrets) (connector.Connector, error) {
		var cfg struct {
			Folders []string `json:"folders"`
		}
		_ = json.Unmarshal(i.Config, &cfg)
		return notesConnector{key: i.Declared["api_key"], folders: cfg.Folders}, nil
	})
}

// extensionStore is a hub store that keeps plugins too (the database's tables, in memory).
type extensionStore struct {
	*keepStore
	mu         sync.Mutex
	rows       map[string]extension.Record
	secrets    map[string]map[string]string
	audit      []string
	auditRows  []storeAudit // the same rows, with their details
	markets    map[string]extension.MarketRecord
	failDelete string // only this extension's delete fails
	failPut    bool   // every PutExtension fails (a database that cannot be written)
	failPutID  string // a selected member or main record fails before its write
}

func (s *extensionStore) Extensions(context.Context) ([]extension.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []extension.Record
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}
func (s *extensionStore) PutExtension(ctx context.Context, r extension.Record) error {
	return s.PutExtensionPlan(ctx, []extension.Record{r})
}
func (s *extensionStore) PutExtensionPlan(_ context.Context, records []extension.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range records {
		if s.failPut || s.failPutID == record.ID {
			return errors.New("the database cannot be written")
		}
	}
	for _, record := range records {
		if record.ExpectedConfig != nil {
			current := s.rows[record.ID].Config
			if len(current) == 0 {
				current = json.RawMessage(`{}`)
			}
			if !sameValue(current, record.ExpectedConfig) {
				return extension.ErrSettingsMoved
			}
		}
	}
	for _, record := range records {
		deleted := slices.Clone(record.DeleteSecrets)
		record.ExpectedConfig, record.DeleteSecrets = nil, nil
		s.rows[record.ID] = record
		for _, name := range deleted {
			delete(s.secrets[record.ID], name)
		}
	}
	return nil
}

func (s *extensionStore) DeleteExtension(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDelete == id {
		return errors.New("the database cannot delete this extension")
	}
	delete(s.rows, id)
	delete(s.secrets, id)
	return nil
}
func (s *extensionStore) ExtensionSecrets(_ context.Context, id string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.secrets[id] {
		out[k] = v
	}
	return out, nil
}
func (s *extensionStore) PutExtensionSecret(_ context.Context, id, name, sealed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.secrets[id] == nil {
		s.secrets[id] = map[string]string{}
	}
	s.secrets[id][name] = sealed
	return nil
}
func (s *extensionStore) DeleteExtensionSecret(_ context.Context, id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets[id], name)
	return nil
}
func (s *extensionStore) AddExtensionAudit(_ context.Context, id, action, actor string, detail json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, id+" "+action+" "+actor)
	s.auditRows = append(s.auditRows, storeAudit{plugin: id, action: action, actor: actor, detail: detail})
	return nil
}

type storeAudit struct {
	plugin, action, actor string
	detail                json.RawMessage
}

func (s *extensionStore) audits() []storeAudit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]storeAudit(nil), s.auditRows...)
}

// notesPackage writes a plugin package whose program is this test binary serving notesConnector.
func notesPackage(t *testing.T, version string) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "notes"), []byte("#!/bin/sh\nPERSONA_TEST_PLUGIN=connector exec "+self+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-notes", "name": "Example Notes", "version": %q,
		"engines": {"plugin_protocol": 1},
		"runtime": {"command": {%q: "bin/notes"}},
		"contributes": {"connectors": [{"id": "examplenotes", "name": "Example Notes", "capabilities": ["status", "tools", "future_example"],
			"hosts": ["notes.example.com"]}]},
		"configuration": {"type": "object", "properties": {"folders": {"type": "array", "title": "文件夹", "items": {"type": "string"}}}},
		"secrets": [{"name": "api_key", "title": "API Key", "required": true}],
		"permissions": {"network": ["notes.example.com"]}}`, version, runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func extensionTestHub(t *testing.T) (*Manager, *extensionStore) {
	h := newTestManager(t)
	store := newExtensionStore()
	h.store = store
	startTestExtensions(t, h)
	return h, store
}

func newExtensionStore() *extensionStore {
	return &extensionStore{keepStore: newKeepStore(), rows: map[string]extension.Record{}, secrets: map[string]map[string]string{}}
}

// startTestExtensions starts h's connectors and plugins (hub.json's cfgs) on h.store, stopping them when the test ends.
func startTestExtensions(t *testing.T, h *Manager, cfgs ...ExtensionConfig) {
	// t.TempDir removes all of a test's temporary directories in one cleanup, added at its first call. Calling it
	// before adding the cleanup below makes the programs stop before those directories go (cleanups run last added,
	// first called): a program a test's last change restarted would otherwise still be writing into one while it is
	// removed ("TempDir RemoveAll cleanup: … directory not empty", #4104).
	_ = t.TempDir()
	h.connectors = connector.NewRegistry(nil)
	h.extensions = newExtensions(h.cfg.dir, cfgs, h.logf)
	h.extensions.sandboxes.noDefault = true // test programs live outside their packages (extension_sandbox_test.go)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		for _, p := range h.extensions.reg.List() {
			if hp := h.extensions.get(p.Record.ID); hp != nil && hp.proc != nil {
				hp.proc.Stop()
			}
		}
	})
	h.startTokenRecord(ctx)
	h.startConnectors(ctx)
	h.startExtensions(ctx)
}

func waitLong(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A phone (a device token) sets plugins up but installs nothing; an answer-only device does neither.
func TestExtensionAPIRights(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	for _, path := range []string{"/api/v1/plugins/-/preview", "/api/v1/plugins/-/install", "/api/v1/plugins/x/-/upgrade", "/api/v1/plugins/x/-/rollback"} {
		req := `{}`
		code, out := callFrom(t, mux, "POST", path, h.cfg.Token, req, "[fd00::10]:1234")
		if code != 403 || out["code"] != "this_machine_only" {
			t.Errorf("%s from another machine: %d %v", path, code, out)
		}
	}
}

// callFrom is call from the address from.
func callFrom(t *testing.T, h http.Handler, method, path, token, body, from string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = from
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (s *extensionStore) ExtensionMarkets(context.Context) ([]extension.MarketRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []extension.MarketRecord
	for _, r := range s.markets {
		out = append(out, r)
	}
	return out, nil
}
func (s *extensionStore) PutExtensionMarket(_ context.Context, r extension.MarketRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.markets == nil {
		s.markets = map[string]extension.MarketRecord{}
	}
	s.markets[r.ID] = r
	return nil
}
func (s *extensionStore) DeleteExtensionMarket(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.markets, id)
	return nil
}

// A plugin's confirm_always travels from its manifest to the gate: an allow-everything rule no longer lets the AI run
// that action without the user, and the connectors that say nothing keep their allow.
func TestExtensionConfirmAlwaysReachesTheGate(t *testing.T) {
	item := extension.ConnectorItem{ID: "examplechat", Name: "Example Chat", Capabilities: []string{"status", "actions"}, ConfirmAlways: []string{"examplechat_send"}}
	m := connectorManifest(item)
	allow := []connector.Rule{{Connector: "*", Action: "*", Decision: connector.Allow}}
	if got := connector.Decide(allow, m, "examplechat", "examplechat_send", nil); got != connector.Confirm {
		t.Fatalf("send under an allow rule: %s", got)
	}
	if got := connector.Decide(allow, m, "examplechat", "examplechat_mark_read", nil); got != connector.Allow {
		t.Fatalf("an action not in confirm_always: %s", got)
	}
	item.ConfirmAlways = nil
	if got := connector.Decide(allow, connectorManifest(item), "examplechat", "examplechat_send", nil); got != connector.Allow {
		t.Fatalf("not declared: %s", got)
	}
}

// An installed plugin takes the place of a built-in that yields (Telegram from the market, #1704), shown with the
// example connector: the built-in's program has stopped once the installed one is in, the connector keeps its id and
// its sign-in (the hub's sealed secret, never forgotten), and uninstalling the plugin brings the built-in back with
// the sign-in still there.
func TestInstalledExtensionTakesTheBuiltinsPlace(t *testing.T) {
	h, store := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	ctx := context.Background()
	builtinDir := notesPackage(t, "0.9.0")
	raw, _ := os.ReadFile(filepath.Join(builtinDir, extension.ManifestFile))
	m, err := extension.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.extensions.reg.AddBuiltin(m, builtinDir, true); err != nil {
		t.Fatal(err)
	}
	h.extensions.reg.Yields("example-notes")
	_ = store.PutConnectorSecret(ctx, "examplenotes", "sealed session")
	if code, out := call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/secrets/api_key", master, `{"value": "k3y"}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	ok := func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(ctx, c).State == connector.StateOK
	}
	waitLong(t, "the built-in's connector registered", func() bool { return h.connectors.Get("examplenotes") != nil })
	if err := h.setConnectorEnabled(ctx, "examplenotes", true); err != nil { // a built-in's starts off; the user turned it on
		t.Fatal(err)
	}
	waitLong(t, "the built-in's connector", ok)
	builtin := h.extensions.get("example-notes").proc

	pkg := notesPackage(t, "1.0.0")
	code, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+pkg+`"}`)
	if code != 200 {
		t.Fatalf("the built-in's id is free to install over: %d %v", code, pv)
	}
	code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	if code != 200 || out["source"] != "installed" || out["version"] != "1.0.0" {
		t.Fatalf("installed: %d %v", code, out)
	}
	if st, _ := builtin.Status(); st != "stopped" {
		t.Fatalf("the built-in's program still runs (%s): two would hold one sign-in", st)
	}
	waitLong(t, "the installed plugin's connector", ok)
	if hp := h.extensions.get("example-notes"); hp == nil || hp.proc == builtin {
		t.Fatal("the installed plugin does not run")
	}
	if sealed, kept, _ := store.ConnectorSecret(ctx, "examplenotes"); !kept || sealed != "sealed session" {
		t.Fatal("the connector's sign-in did not carry over")
	}

	if code, out = call(t, mux, "DELETE", "/api/v1/plugins/example-notes/-/", master, ""); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if p := h.extensions.reg.Get("example-notes"); p == nil || p.Record.Source != extension.SourceBuiltin {
		t.Fatalf("the built-in is not back: %+v", p)
	}
	if sealed, kept, _ := store.ConnectorSecret(ctx, "examplenotes"); !kept || sealed != "sealed session" {
		t.Fatal("uninstalling the plugin forgot the sign-in the built-in uses")
	}
}

// Every plugin learns the user's time zone at its start (initialize's hub.timezone), as the built-in Telegram did
// through its settings: an installed Telegram plugin shows times as the user reads them (#1704).
func TestExtensionsGetTheUserTimezone(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	pkg := notesPackage(t, "1.0.0")
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", h.cfg.Token, `{"source": "dir:`+pkg+`"}`)
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", h.cfg.Token, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	for _, tz := range []string{"", "Asia/Shanghai"} {
		h.opts.Timezone = func() string { return tz } // the assistant's settings, in this process
		s, err := h.extensions.spec(context.Background(), h.extensions.reg.Get("example-notes"), &hubExtension{id: "example-notes"})
		if err != nil {
			t.Fatal(err)
		}
		params, err := s.Init(context.Background())
		if err != nil || params.Hub.Timezone != tz {
			t.Fatalf("%q: %+v %v", tz, params.Hub, err)
		}
	}
}

// blockingInitSecretStore pauses one extension secret read after the fixture is set up. Later reads, including the
// configuration update's bound-secret cleanup and a retry, proceed normally.
type blockingInitSecretStore struct {
	*extensionStore
	mu             sync.Mutex
	armed, blocked bool
	started        chan struct{}
	release        chan struct{}
	audits         map[string]*initAuditBlock
}

func (s *blockingInitSecretStore) arm() {
	s.mu.Lock()
	s.armed = true
	s.mu.Unlock()
}

func (s *blockingInitSecretStore) ExtensionSecrets(ctx context.Context, id string) (map[string]string, error) {
	s.mu.Lock()
	block := s.armed && !s.blocked
	if block {
		s.blocked = true
	}
	s.mu.Unlock()
	if block {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.extensionStore.ExtensionSecrets(ctx, id)
}

type initAuditBlock struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *initAuditBlock) unblock() {
	if b != nil {
		b.once.Do(func() { close(b.release) })
	}
}

func (s *blockingInitSecretStore) blockAudit(action string) *initAuditBlock {
	if s.audits == nil {
		s.audits = map[string]*initAuditBlock{}
	}
	b := &initAuditBlock{started: make(chan struct{}), release: make(chan struct{})}
	s.audits[action] = b
	return b
}

func (s *blockingInitSecretStore) AddExtensionAudit(ctx context.Context, id, action, actor string, detail json.RawMessage) error {
	err := s.extensionStore.AddExtensionAudit(ctx, id, action, actor, detail)
	if b := s.audits[action]; b != nil {
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

type initSettingsSealer struct{}

func (initSettingsSealer) Seal(value string) (string, error) { return "sealed:" + value, nil }
func (initSettingsSealer) Open(value string) (string, error) {
	return strings.TrimPrefix(value, "sealed:"), nil
}

func initSettingsPackage(t *testing.T, rpc bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "gateway"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mode := `, "http": true`
	if !rpc {
		mode = `, "rpc": "none", "http": true`
	}
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-gateway", "name": "Example Gateway", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/gateway"}%s},
		"activation_events": ["on_startup"], "contributes": {"transcription": [{"id": "gateway", "name": "Gateway"}]},
		"configuration": {"type": "object", "properties": {"endpoint": {"type": "string", "format": "uri"}}},
		"secrets": [{"name": "api_key", "title": "API Key"}]}`, runtime.GOOS+"-"+runtime.GOARCH, mode)
	if err := os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A settings change while secrets are being obtained must not let an old endpoint escape with its replacement
// credential. This covers both initialize and the environment used by programs that do not speak RPC.
func TestExtensionInitSettingsMovedDuringSecretResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		rpc  bool
	}{{"rpc", true}, {"environment", false}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ps := newExtensions(t.TempDir(), nil, func(string, ...any) {})
			store := &blockingInitSecretStore{extensionStore: newExtensionStore(), started: make(chan struct{}), release: make(chan struct{})}
			ps.reg = extension.NewRegistry(ps.root, store, initSettingsSealer{}, 0)
			reg := ps.reg
			pv, err := reg.Preview(ctx, "dir:"+initSettingsPackage(t, tc.rpc), "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reg.Install(ctx, pv.ID, pv.Digest, "test"); err != nil {
				t.Fatal(err)
			}
			reg.BoundSecrets = func(_ *extension.Extension, old, next json.RawMessage) []string {
				if string(old) != string(next) {
					return []string{"api_key"}
				}
				return nil
			}
			configA := json.RawMessage(`{"endpoint":"https://a.example.com"}`)
			configB := json.RawMessage(`{"endpoint":"https://b.example.com"}`)
			if err := reg.SetConfig(ctx, "example-gateway", configA, "test"); err != nil {
				t.Fatal(err)
			}
			if err := reg.SetSecret(ctx, "example-gateway", "api_key", "credential-a", "test"); err != nil {
				t.Fatal(err)
			}

			type result struct {
				params extension.InitializeParams
				env    []string
				err    error
			}
			handoff := func() result {
				s, err := ps.specOf(ctx, reg.Get("example-gateway"), &hubExtension{id: "example-gateway"})
				if err != nil {
					return result{err: err}
				}
				if tc.rpc {
					params, err := s.Init(ctx)
					return result{params: params, err: err}
				}
				return result{env: s.Env}
			}
			store.arm()
			results := make(chan result, 1)
			go func() { results <- handoff() }()
			released := false
			defer func() {
				if !released {
					close(store.release)
				}
			}()
			select {
			case <-store.started:
			case <-time.After(5 * time.Second):
				t.Fatal("secret resolution did not start")
			}
			if err := reg.SetConfig(ctx, "example-gateway", configB, "test"); err != nil {
				t.Fatal(err)
			}
			if err := reg.SetSecret(ctx, "example-gateway", "api_key", "credential-b", "test"); err != nil {
				t.Fatal(err)
			}
			close(store.release)
			released = true

			var got result
			select {
			case got = <-results:
			case <-time.After(5 * time.Second):
				t.Fatal("secret resolution did not finish")
			}
			config, credential := string(got.params.Config), got.params.Secrets["api_key"]
			if !tc.rpc {
				for _, entry := range got.env {
					if value, ok := strings.CutPrefix(entry, "PERSONA_PLUGIN_CONFIG="); ok {
						config = value
					}
					if value, ok := strings.CutPrefix(entry, "PERSONA_PLUGIN_SECRETS="); ok {
						var secrets map[string]string
						_ = json.Unmarshal([]byte(value), &secrets)
						credential = secrets["api_key"]
					}
				}
			}
			if !errors.Is(got.err, extension.ErrSettingsMoved) {
				t.Fatalf("hand-off returned config %s with %q (err %v), want settings_moved", config, credential, got.err)
			}

			// A fresh specification after the aborted attempt uses the new coherent pair.
			got = handoff()
			config, credential = string(got.params.Config), got.params.Secrets["api_key"]
			if !tc.rpc {
				for _, entry := range got.env {
					if value, ok := strings.CutPrefix(entry, "PERSONA_PLUGIN_CONFIG="); ok {
						config = value
					}
					if value, ok := strings.CutPrefix(entry, "PERSONA_PLUGIN_SECRETS="); ok {
						var secrets map[string]string
						_ = json.Unmarshal([]byte(value), &secrets)
						credential = secrets["api_key"]
					}
				}
			}
			if got.err != nil || !sameValue(json.RawMessage(config), configB) || credential != "credential-b" {
				t.Fatalf("retry returned config %s with %q (err %v)", config, credential, got.err)
			}
		})
	}
}

type initHandoffResult struct {
	params extension.InitializeParams
	env    []string
	err    error
}

func runInitHandoff(ctx context.Context, ps *extensions, rpc bool) initHandoffResult {
	s, err := ps.specOf(ctx, ps.reg.Get("example-gateway"), &hubExtension{id: "example-gateway"})
	if err != nil {
		return initHandoffResult{err: err}
	}
	if rpc {
		params, err := s.Init(ctx)
		return initHandoffResult{params: params, err: err}
	}
	return initHandoffResult{env: s.Env}
}

func initHandoffValues(got initHandoffResult, rpc bool) (string, string) {
	config, credential := string(got.params.Config), got.params.Secrets["api_key"]
	if !rpc {
		for _, entry := range got.env {
			if value, ok := strings.CutPrefix(entry, "PERSONA_PLUGIN_CONFIG="); ok {
				config = value
			}
			if value, ok := strings.CutPrefix(entry, "PERSONA_PLUGIN_SECRETS="); ok {
				var secrets map[string]string
				_ = json.Unmarshal([]byte(value), &secrets)
				credential = secrets["api_key"]
			}
		}
	}
	return config, credential
}

// A committed save is already visible to secret reads before its audit and change notification finish. The hand-off
// must see the write in progress even when the post-commit notification has not advanced any generation yet.
func TestExtensionInitSettingsMovedBeforeChangeNotification(t *testing.T) {
	for _, mutation := range []string{"config_and_secret", "secret"} {
		for _, tc := range []struct {
			name string
			rpc  bool
		}{{"rpc", true}, {"environment", false}} {
			t.Run(mutation+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				ps := newExtensions(t.TempDir(), nil, func(string, ...any) {})
				store := &blockingInitSecretStore{extensionStore: newExtensionStore(), started: make(chan struct{}), release: make(chan struct{})}
				ps.reg = extension.NewRegistry(ps.root, store, initSettingsSealer{}, 0)
				reg := ps.reg
				pv, err := reg.Preview(ctx, "dir:"+initSettingsPackage(t, tc.rpc), "")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := reg.Install(ctx, pv.ID, pv.Digest, "test"); err != nil {
					t.Fatal(err)
				}
				reg.BoundSecrets = func(_ *extension.Extension, old, next json.RawMessage) []string {
					if string(old) != string(next) {
						return []string{"api_key"}
					}
					return nil
				}
				configA := json.RawMessage(`{"endpoint":"https://a.example.com"}`)
				configB := json.RawMessage(`{"endpoint":"https://b.example.com"}`)
				if err := reg.SetConfig(ctx, "example-gateway", configA, "test"); err != nil {
					t.Fatal(err)
				}
				if err := reg.SetSecret(ctx, "example-gateway", "api_key", "credential-a", "test"); err != nil {
					t.Fatal(err)
				}

				var configAudit *initAuditBlock
				if mutation == "config_and_secret" {
					configAudit = store.blockAudit("config")
				}
				secretAudit := store.blockAudit("secret_set")
				defer configAudit.unblock()
				defer secretAudit.unblock()
				store.arm()
				results := make(chan initHandoffResult, 1)
				go func() { results <- runInitHandoff(ctx, ps, tc.rpc) }()
				released := false
				defer func() {
					if !released {
						close(store.release)
					}
				}()
				select {
				case <-store.started:
				case <-time.After(5 * time.Second):
					t.Fatal("secret resolution did not start")
				}

				saves := make(chan error, 2)
				saveCount := 1
				if mutation == "config_and_secret" {
					saveCount = 2
					go func() { saves <- reg.SetConfig(ctx, "example-gateway", configB, "test") }()
					select {
					case <-configAudit.started:
					case <-time.After(5 * time.Second):
						t.Fatal("configuration did not commit before its audit")
					}
				}
				go func() { saves <- reg.SetSecret(ctx, "example-gateway", "api_key", "credential-b", "test") }()
				select {
				case <-secretAudit.started:
				case <-time.After(5 * time.Second):
					t.Fatal("secret did not commit before its audit")
				}

				close(store.release)
				released = true
				var got initHandoffResult
				select {
				case got = <-results:
				case <-time.After(5 * time.Second):
					t.Fatal("secret resolution did not finish")
				}
				config, credential := initHandoffValues(got, tc.rpc)
				configAudit.unblock()
				secretAudit.unblock()
				for range saveCount {
					select {
					case err := <-saves:
						if err != nil {
							t.Fatalf("save: %v", err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("save did not finish")
					}
				}
				if !errors.Is(got.err, extension.ErrSettingsMoved) {
					t.Fatalf("hand-off returned config %s with %q (err %v), want settings_moved", config, credential, got.err)
				}
			})
		}
	}
}

// A connector's command line (connector.Manifest.CLI, #1704) reaches the assistant's machine with the connectors
// signed in, as its name and the connector it runs (connectors.signed_in's clis, which the assistant passes on in
// assistant.prepare): only while the connector is signed in.
func TestAssistantGetsTheConnectorsCommandLines(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	pkg := notesPackage(t, "1.0.0")
	p := filepath.Join(pkg, extension.ManifestFile)
	raw, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(raw), `"hosts": ["notes.example.com"]}`, `"hosts": ["notes.example.com"], "cli": "notes"}`, 1)), 0o644)
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+pkg+`"}`)
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	clis := func() map[string]string {
		v, err := h.assistantAppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppConnectors,
			Method: assistantwire.ConnectorsSignedIn})
		raw, _ := json.Marshal(v)
		var res struct {
			CLIs map[string]string `json:"clis"`
		}
		if err != nil || json.Unmarshal(raw, &res) != nil {
			t.Fatal(err)
		}
		return res.CLIs
	}
	if len(clis()) != 0 {
		t.Fatal("not signed in yet, a command line:", clis())
	}
	call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/secrets/api_key", master, `{"value": "k3y"}`)
	waitLong(t, "the connector signed in", func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(context.Background(), c).State == connector.StateOK
	})
	if got := clis(); len(got) != 1 || got["notes"] != "examplenotes" {
		t.Fatal(got)
	}
}
