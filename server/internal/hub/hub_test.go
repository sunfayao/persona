package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Listen != ":18660" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if len(cfg.Token) != 48 {
		t.Fatalf("token should be 24 random bytes of hex, got %q", cfg.Token)
	}
	if other := defaultConfig(); other.Token == cfg.Token {
		t.Fatal("each default config needs its own token")
	}
	if !strings.Contains(cfg.PublicURL, ":18660") {
		t.Fatalf("public url = %q", cfg.PublicURL)
	}
	ids := map[string]bool{}
	for _, m := range cfg.Machines {
		if m.ID == "" || m.Name == "" || (m.Kind != "local" && m.Kind != "ssh") {
			t.Fatalf("bad machine %+v", m)
		}
		if m.Kind == "ssh" && m.SSHHost == "" {
			t.Fatalf("ssh machine %s has no host", m.ID)
		}
		ids[m.ID] = true
	}
	// A fresh hub knows only itself; other machines are added to hub.json (docs/operations/config.md).
	if len(cfg.Machines) != 1 {
		t.Fatalf("default machines should be just this one, got %+v", cfg.Machines)
	}
	if local := cfg.Machines[0]; local.Kind != "local" {
		t.Fatalf("the first machine should be the local hub, got %+v", local)
	}
}

// The Docker image's hub (PERSONA_LOCAL_NODE=0) starts with no machine: its own has no herdr or claude.
func TestDefaultConfigWithoutLocalNode(t *testing.T) {
	t.Setenv("PERSONA_LOCAL_NODE", "0")
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".persona", "hub.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["machines"]) != "[]" {
		t.Fatalf("machines written as %s, want [] (not omitted: a missing list would mean the default)", raw["machines"])
	}
}

// A compose file gives the database's address and the hub's own: they win over hub.json and are not written into it.
func TestEnvOverridesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".persona")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := `{"token":"t","database_url":"postgres://file","public_url":"http://file:18660","listen":":1"}`
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PERSONA_DATABASE_URL", "postgres://env")
	t.Setenv("PERSONA_PUBLIC_URL", "https://hub.example.com")
	t.Setenv("PERSONA_LISTEN", ":18660")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://env" || cfg.PublicURL != "https://hub.example.com" || cfg.Listen != ":18660" {
		t.Fatalf("env not applied: %+v", cfg)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hub.json")); string(b) != file {
		t.Fatalf("hub.json was rewritten: %s", b)
	}
	t.Setenv("PERSONA_DATABASE_URL", "")
	if cfg, _ := LoadConfig(); cfg.DatabaseURL != "postgres://file" {
		t.Fatalf("an empty variable must not override: %q", cfg.DatabaseURL)
	}
}

func TestLoadConfigCreatesAndReloads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".persona", "hub.json")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %v", st.Mode().Perm())
	}
	again, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if again.Token != cfg.Token {
		t.Fatal("the token must survive a reload")
	}
}

func TestLoadConfigKeepsExplicitValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".persona")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"listen":":1","token":"abc","public_url":"http://x","hook_timeout_minutes":0,"machines":[]}`
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":1" || cfg.Token != "abc" {
		t.Fatalf("explicit values were not kept: %+v", cfg)
	}
}

func TestLoadConfigRejectsEmptyToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".persona")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(`{"listen":":1","token":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("want a token error, got %v", err)
	}
}

func TestLoadConfigRejectsBadJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".persona")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("want a parse error, got %v", err)
	}
}

func TestPairURLRoundTrips(t *testing.T) {
	cfg := &Config{PublicURL: "http://hub.example.com:18660", Token: "a b&c=d/é"}
	link := cfg.PairURL()
	if !strings.HasPrefix(link, "persona://pair?") {
		t.Fatalf("pair url = %q", link)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "persona" || u.Host != "pair" {
		t.Fatalf("unexpected scheme/host %q %q", u.Scheme, u.Host)
	}
	q := u.Query()
	if q.Get("url") != cfg.PublicURL {
		t.Fatalf("url = %q", q.Get("url"))
	}
	if q.Get("token") != cfg.Token {
		t.Fatalf("token = %q", q.Get("token"))
	}
}

// The Mac has no camera to scan with: `personad hub pair` prints the link too, to paste into "Connect hub".
func TestPairPrintsTheLinkForTheMac(t *testing.T) {
	setPairLanguage(t, "en")
	cfg := &Config{PublicURL: "http://hub.example.com:18660", Token: "tok"}
	var b strings.Builder
	writePair(&b, cfg)
	out := b.String()
	if !strings.Contains(out, cfg.PairURL()) {
		t.Fatalf("pair output has no link:\n%s", out)
	}
	if !strings.Contains(out, "Server: "+cfg.PublicURL) {
		t.Fatalf("pair output lost the server line:\n%s", out)
	}
}

// A hello starts fleetClientsChanged in the background, so test clients use the same lock as connections.
func addTestClient(h *Hub, clients ...*client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range clients {
		h.clients[c] = struct{}{}
	}
}

func removeTestClient(h *Hub, c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// addCfgMachines adds hub.json entries to a test hub whose managers are already connected. Until startDevices, which
// most test hubs never call, entryOwner and cfgEntries read h.cfg.Machines under h.devices.mu from the hub's goroutines
// (a manager's bus event, an HTTP handler), so the write takes the same lock (#8639).
func addCfgMachines(h *Hub, machines ...MachineConfig) {
	h.devices.mu.Lock()
	defer h.devices.mu.Unlock()
	h.cfg.Machines = append(h.cfg.Machines, machines...)
}

// newTestHub builds a Hub with no machines, no clients and push disabled, so interaction logic can be exercised offline.
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	return newTestHubWithDeployCtx(t, context.Background())
}

func newTestHubWithDeployCtx(t *testing.T, deployCtx context.Context) *Hub {
	t.Helper()
	cfg := defaultConfig()
	cfg.Machines = nil
	cfg.dir = shortTempDir(t)
	h := &Hub{
		deployCtx: deployCtx, cfg: cfg, version: "test", logger: log.New(io.Discard, "", 0),
		byID: map[string]*Machine{}, clients: map[*client]struct{}{},
		push: newPusher(nil, func(string, ...any) {}),
		// fake nodes start sessions they never list: do not wait the full listedWaitMax on each start
		listedWait: 50 * time.Millisecond,
	}
	playManagerAsks(h) // its own cards wait for h.resolve (fleet_helpers_test.go)
	h.push.quiesce = &h.quiesce
	h.push.web.quiesce = &h.quiesce
	h.upd = newUpdater(h)
	h.upd.dir = filepath.Join(cfg.dir, "update")
	h.upd.exec = func(string) error { return errors.New("exec is not run in tests") }
	h.upd.startWatch = func(string, string) error { return nil }
	h.startHelloWriters()
	t.Cleanup(h.stopHelloWriters)
	return h
}

// shortTempDir is a temporary directory short enough for the Unix sockets the hub makes under it
// (<dir>/extensions/<id>.sock, <dir>/extensions/run/<id>/http.sock). sun_path holds 104 bytes on macOS, and t.TempDir()
// under its /var/folders/…/T/ plus the test's name already takes most of them.
func shortTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "ph")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func textOf(r proto.ToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}
