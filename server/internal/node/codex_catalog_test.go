package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
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

	"github.com/nwverse/persona/server/internal/proto"
)

// catalogEntry is a Codex model entry with every field Codex requires (codexCatalogFields), and a few it reads.
func catalogEntry(slug, visibility string, levels ...string) map[string]any {
	ls := []map[string]any{}
	for _, l := range levels {
		ls = append(ls, map[string]any{"effort": l, "description": l})
	}
	return map[string]any{"slug": slug, "display_name": slug, "description": "From example-gateway", "visibility": visibility,
		"supported_reasoning_levels": ls, "shell_type": "shell_command", "supported_in_api": true, "priority": 1,
		"support_verbosity": false, "truncation_policy": map[string]any{"mode": "tokens", "limit": 10000},
		"experimental_supported_tools": []string{}, "context_window": 1000000}
}

func catalogJSON(entries ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"models": entries})
	return string(b)
}

// codexCatalogBody is a Codex model catalog as a gateway serves it: a listed model with levels, one that takes none,
// and a hidden one.
var codexCatalogBody = catalogJSON(catalogEntry("example-big", "list", "low", "high"), catalogEntry("example-plain", "list"),
	catalogEntry("example-hidden", "hide", "low"))

// openAIModelsBody is OpenAI's model list, which Codex cannot read as a catalog.
const openAIModelsBody = `{"object":"list","data":[{"id":"example-big","object":"model"}]}`

// fakeCodex puts a codex on PATH that says it is version (codex --version), and forgets what earlier tests probed.
func fakeCodex(t *testing.T, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script as codex")
	}
	bin := t.TempDir()
	if version != "" {
		writeFile(t, filepath.Join(bin, "codex"), "#!/bin/sh\necho 'codex-cli "+version+"'\n")
		if err := os.Chmod(filepath.Join(bin, "codex"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir()) // agentExecutable's fallbacks look under the home and in the system's places
	t.Setenv("PERSONA_NODEJS", "")
	hideMachineAgents(t) // and under NVM_DIR: a codex installed with nvm would answer for the fake
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	forgetCodexCatalogs(t)
}

func forgetCodexCatalogs(t *testing.T) {
	t.Helper()
	reset := func() {
		codexCatalogCache.Lock()
		codexCatalogCache.got = map[string]codexCatalogAnswer{}
		codexCatalogCache.Unlock()
		codexVersionCache.Lock()
		codexVersionCache.stamp, codexVersionCache.version, codexVersionCache.read, codexVersionCache.until = "", "", false, time.Time{}
		codexVersionCache.Unlock()
		codexMissingUntil.Lock()
		codexMissingUntil.until = time.Time{}
		codexMissingUntil.Unlock()
		agentExecutableMu.Lock()
		delete(agentExecutableCache, "codex")
		agentExecutableMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// catalogGateway serves handler and records each request's path, query and Authorization.
type catalogGateway struct {
	*httptest.Server
	mu   sync.Mutex
	seen []*http.Request
}

func newCatalogGateway(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *catalogGateway {
	t.Helper()
	g := &catalogGateway{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" && slices.Contains(portProbeAgents, r.Header.Get("User-Agent")) {
			return // a Persona node on the machine probing this server among an agent's ports (ports.go), not the probe tested
		}
		g.mu.Lock()
		g.seen = append(g.seen, r.Clone(context.Background()))
		g.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(g.Close)
	return g
}

func codexProvider(base string) proto.ManagedLauncher {
	return proto.ManagedLauncher{Name: "gw", Agent: "codex", BaseURL: base, APIKey: "sk-test", Model: "example-big"}
}

func quietNode() *Node { return &Node{logger: log.New(io.Discard, "", 0)} }

// A codex whose --version fails is not run again on every start, and is again once codexCatalogRetry has passed.
func TestCodexVersionFailureKeptBriefly(t *testing.T) {
	fakeCodex(t, "0.167.0")
	exe, err := agentExecutable("codex")
	if err != nil {
		t.Fatal(err)
	}
	runs := filepath.Join(t.TempDir(), "runs")
	writeFile(t, exe, "#!/bin/sh\necho run >> '"+runs+"'\nexit 1\n")
	for range 3 {
		if _, v := codexVersion(context.Background()); v != "" {
			t.Fatalf("version %q", v)
		}
	}
	if b, _ := os.ReadFile(runs); strings.Count(string(b), "run") != 1 {
		t.Fatalf("ran codex --version %d times", strings.Count(string(b), "run"))
	}
	codexVersionCache.Lock()
	codexVersionCache.until = time.Now().Add(-time.Second)
	codexVersionCache.Unlock()
	codexVersion(context.Background())
	if b, _ := os.ReadFile(runs); strings.Count(string(b), "run") != 2 {
		t.Fatal("not asked again after codexCatalogRetry")
	}
}

// launcher.env carries the catalog URL to personad launch, and nothing when there is none.
func TestLauncherEnvCodexCatalog(t *testing.T) {
	fakeCodex(t, "0.167.0")
	with := newCatalogGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("client_version") != "" && r.URL.Path == "/v1/models" {
			io.WriteString(w, codexCatalogBody)
			return
		}
		io.WriteString(w, openAIModelsBody)
	})
	without := newCatalogGateway(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, openAIModelsBody) })
	n := quietNode()
	a, b := codexProvider(with.URL+"/v1"), codexProvider(without.URL+"/v1")
	a.Name, b.Name = "with", "without"
	n.setManaged([]proto.ManagedLauncher{a, b})
	exe, _ := agentExecutable("codex")
	if env := n.launcherEnv("with"); env["codex_catalog_url"] != with.URL+"/v1/models" || env["codex_catalog_codex"] != exe {
		t.Fatalf("with a catalog: %v, checked codex %v", env["codex_catalog_url"], env["codex_catalog_codex"])
	}
	if env := n.launcherEnv("without"); env["codex_catalog_url"] != nil || env["codex_catalog_codex"] != nil {
		t.Fatalf("without a catalog: %v", env)
	}
}

// launcher.env waits for the lookup at most codexCatalogWait (nodecmd's TestLaunchWaitsLongerThanTheCatalogLookup
// keeps it well under what personad launch waits for the node): a gateway or a codex --version that does not answer
// starts the provider's codex without a catalog and never fails the start, and the lookup goes on, so a later start
// has the catalog.
func TestLauncherEnvWaitsBrieflyForTheCatalog(t *testing.T) {
	old := codexCatalogWait
	codexCatalogWait = 100 * time.Millisecond
	t.Cleanup(func() { codexCatalogWait = old })
	for _, slow := range []string{"gateway", "codex --version"} {
		t.Run(slow, func(t *testing.T) {
			fakeCodex(t, "0.167.0")
			release := make(chan struct{})
			released := sync.OnceFunc(func() { close(release) })
			t.Cleanup(released)
			if slow == "codex --version" {
				exe, _ := agentExecutable("codex")
				gate := filepath.Join(t.TempDir(), "gate")
				writeFile(t, exe, "#!/bin/sh\nwhile [ ! -e '"+gate+"' ]; do /bin/sleep 0.05; done\necho 'codex-cli 0.167.0'\n")
				go func() { <-release; os.WriteFile(gate, nil, 0o600) }()
			}
			g := newCatalogGateway(t, func(w http.ResponseWriter, r *http.Request) {
				if slow == "gateway" {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				if r.URL.Path == "/v1/models" {
					io.WriteString(w, codexCatalogBody)
				}
			})
			n := quietNode()
			n.setManaged([]proto.ManagedLauncher{codexProvider(g.URL + "/v1")})
			start := time.Now()
			if env := n.launcherEnv("gw"); env["codex_catalog_url"] != nil {
				t.Fatalf("waited for the catalog: %v", env["codex_catalog_url"])
			}
			if took := time.Since(start); took > 2*time.Second {
				t.Fatalf("launcher.env took %s", took)
			}
			released()
			waitWithin(t, 10*time.Second, "the lookup kept what it found", func() bool {
				return n.launcherEnv("gw")["codex_catalog_url"] == g.URL+"/v1/models"
			})
		})
	}
}

// A codex behind a shim is the same file before and after an upgrade: its version is read again after
// codexCatalogTTL, in the background, the version read before given meanwhile, so a slow shim holds no start.
func TestCodexVersionReadAgainBehindAShim(t *testing.T) {
	fakeCodex(t, "0.167.0")
	exe, _ := agentExecutable("codex")
	dir := t.TempDir()
	said, slow := filepath.Join(dir, "version"), filepath.Join(dir, "slow")
	writeFile(t, said, "codex-cli 0.160.0\n")
	writeFile(t, exe, "#!/bin/sh\nif [ -e '"+slow+"' ]; then /bin/sleep 1; fi\n/bin/cat '"+said+"'\n")
	if _, v := codexVersion(context.Background()); v != "0.160.0" {
		t.Fatalf("version %q", v)
	}
	writeFile(t, said, "codex-cli 0.167.0\n") // upgraded behind the shim; the shim is unchanged
	writeFile(t, slow, "")
	if _, v := codexVersion(context.Background()); v != "0.160.0" {
		t.Fatalf("read again at once: %q", v)
	}
	codexVersionCache.Lock()
	kept := time.Until(codexVersionCache.until)
	codexVersionCache.until = time.Now().Add(-time.Second) // as if codexCatalogTTL had passed
	codexVersionCache.Unlock()
	if kept <= 0 || kept > codexCatalogTTL {
		t.Fatalf("the version is kept %s, want at most %s", kept, codexCatalogTTL)
	}
	start := time.Now()
	if _, v := codexVersion(context.Background()); v != "0.160.0" || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("past codexCatalogTTL: %q after %s, want the one read before at once", v, time.Since(start))
	}
	waitWithin(t, 10*time.Second, "the version read again", func() bool {
		_, v := codexVersion(context.Background())
		return v == "0.167.0"
	})
}

// A run its caller gave up on says nothing of codex: it is not kept as a failure.
func TestCodexVersionCallerGivingUpKeepsNothing(t *testing.T) {
	fakeCodex(t, "0.167.0")
	exe, _ := agentExecutable("codex")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v := codexVersionAt(ctx, exe); v != "" {
		t.Fatalf("version %q", v)
	}
	if v := codexVersionAt(context.Background(), exe); v != "0.167.0" {
		t.Fatalf("after a caller gave up: %q", v)
	}
}

// A provider whose gateway and key come only from its own env is looked up like any other.
func TestCodexCatalogForAGatewayInTheProvidersEnv(t *testing.T) {
	fakeCodex(t, "0.167.0")
	g := newCatalogGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Header.Get("Authorization") == "Bearer sk-env" {
			io.WriteString(w, codexCatalogBody)
		}
	})
	n := quietNode()
	n.setManaged([]proto.ManagedLauncher{{Name: "gw", Agent: "codex", Env: "OPENAI_BASE_URL=" + g.URL + "/v1\nOPENAI_API_KEY=sk-env"}})
	if got := n.launcherEnv("gw")["codex_catalog_url"]; got != g.URL+"/v1/models" {
		t.Fatalf("got %v", got)
	}
}

// A panic in a lookup's own goroutine is logged, not the end of the node.
func TestCodexCatalogLookupSurvivesAPanic(t *testing.T) {
	var logged strings.Builder
	n := &Node{logger: log.New(&logged, "", 0)}
	func() {
		defer n.survive("gw", "looking")
		panic("boom")
	}()
	if !strings.Contains(logged.String(), "launcher gw: looking failed: boom") {
		t.Fatalf("logged %q", logged.String())
	}
}

// A codex --version whose child keeps stdout open does not hold the answer past the timeout.
func TestCodexVersionDoesNotWaitOnAChildsOutput(t *testing.T) {
	fakeCodex(t, "0.167.0")
	exe, _ := agentExecutable("codex")
	writeFile(t, exe, "#!/bin/sh\n/bin/sleep 8 &\n/bin/sleep 8\n")
	old := codexVersionTimeout
	codexVersionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { codexVersionTimeout = old })
	start := time.Now()
	if v := codexVersionAt(context.Background(), exe); v != "" {
		t.Fatalf("version %q", v)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("waited %s", took)
	}
}

// No codex found is kept for codexCatalogRetry: agentExecutable may run a login shell for it.
func TestCodexMissingKeptBriefly(t *testing.T) {
	fakeCodex(t, "")
	if exe, v := codexVersion(context.Background()); exe != "" || v != "" {
		t.Fatalf("no codex: %q %q", exe, v)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "codex"), "#!/bin/sh\necho 'codex-cli 0.167.0'\n")
	if err := os.Chmod(filepath.Join(dir, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if _, v := codexVersion(context.Background()); v != "" {
		t.Fatalf("looked again at once: %q", v)
	}
	codexMissingUntil.Lock()
	codexMissingUntil.until = time.Now().Add(-time.Second)
	codexMissingUntil.Unlock()
	if _, v := codexVersion(context.Background()); v != "0.167.0" {
		t.Fatalf("after codexCatalogRetry: %q", v)
	}
}
