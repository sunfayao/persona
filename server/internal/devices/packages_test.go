package devices

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/identity"
	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/update"
	"github.com/sunfayao/persona/server/internal/update/updatetest"
)

// nodeBinary is the build a hub of version sends a node of program on goos/arch from this machine's own builds, with
// no release to fetch from (what the deploy tests check): its path, or why none.
func nodeBinary(goos, arch, program, version string) (string, error) {
	d := New(Options{})
	d.cfg = devicewire.Config{HubVersion: version}
	pkg, err := d.lookupPackage(context.Background(), d.packageKey(goos, arch, program))
	return pkg.path, err
}

// The platform the fetched builds in these tests are for: nobody runs the tests there, so no build is run (the
// version -v check is for this machine's own platform only, TestFetchedBuildOfThisPlatformRuns).
const otherOS, otherArch = "linux", "riscv64"

// fakeRelease is a publisher's site: dl/<version>/manifest.json, dl/<version>/<name>.gz and dl/latest.json, with the
// paths asked for.
type fakeRelease struct {
	mu    sync.Mutex
	files map[string][]byte
	asked []string
	down  bool          // answers 503 to everything: the site is away
	delay time.Duration // before each answer
	srv   *httptest.Server
}

func newFakeRelease(t *testing.T) *fakeRelease {
	r := &fakeRelease{files: map[string][]byte{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, req)
			return
		}
		r.mu.Lock()
		r.asked = append(r.asked, req.URL.Path)
		b, ok := r.files[req.URL.Path]
		down, delay := r.down, r.delay
		r.mu.Unlock()
		time.Sleep(delay)
		if down {
			http.Error(w, "away", http.StatusServiceUnavailable)
			return
		}
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *fakeRelease) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// publish puts release m up at dl/<dir>/ (and as latest.json when latest) with builds: download name (no .gz) ->
// the binary, each listed in Files, Nodes or Links by its name.
func (r *fakeRelease) publish(t *testing.T, dir string, m update.Manifest, latest bool, builds map[string]string) *update.Manifest {
	t.Helper()
	m.Files = map[string]update.File{}
	for name, bin := range builds {
		raw, err := os.ReadFile(bin)
		if err != nil {
			t.Fatal(err)
		}
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		outer, inner := sha256.Sum256(gz.Bytes()), sha256.Sum256(raw)
		f := update.File{Name: name + ".gz", Size: int64(gz.Len()), SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}
		if plat, ok := strings.CutPrefix(name, "persona-node-"); ok {
			plat = strings.TrimSuffix(plat, ".exe")
			if m.Nodes == nil {
				m.Nodes = map[string]map[string]update.File{"persona-node": {}}
			}
			m.Nodes["persona-node"][plat] = f
		} else if plat, ok := strings.CutPrefix(name, programConnector+"-"); ok {
			if m.Links == nil {
				m.Links = map[string]update.File{}
			}
			m.Links[plat] = f
		} else {
			m.Files[strings.TrimSuffix(strings.TrimPrefix(name, "personad-"), ".exe")] = f
		}
		r.mu.Lock()
		r.files["/"+dir+"/"+f.Name] = gz.Bytes()
		r.mu.Unlock()
	}
	b, _ := json.Marshal(m)
	r.mu.Lock()
	r.files["/"+dir+"/manifest.json"] = b
	if latest {
		r.files["/latest.json"] = b
	}
	r.mu.Unlock()
	return &m
}

// packageDevices is a manager whose hub is at version hub, fetching from site ("" none), with its own node binary
// dir, bundled dir and cache in this test's directories.
func packageDevices(t *testing.T, hub, site string) (*Devices, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	isolateNodeBinaries(t)
	bundled := t.TempDir()
	t.Setenv("PERSONA_NODE_BUNDLED", bundled)
	d := newTestDevices(t)
	d.mu.Lock()
	d.cfg.HubVersion, d.cfg.NodePackagesURL = hub, site
	d.mu.Unlock()
	return d, nodeBinaryDir(), bundled
}

// overlapping is a release whose node this manager drives.
func overlapping(version string, build int64) update.Manifest {
	return update.Manifest{Version: version, Build: build, NodeProtocol: nodeproto.Version, NodeProtocolMin: nodeproto.MinManagerVersion,
		NodeCapabilities: nodeproto.Announced}
}

// flatten moves release dir's builds beside latest.json and drops dl/<dir>/: a source with no per-version directories,
// as a GitHub repository's releases/latest/download serves a release.
func (r *fakeRelease) flatten(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for path, b := range r.files {
		if name, ok := strings.CutPrefix(path, "/"+dir+"/"); ok {
			delete(r.files, path)
			if name != "manifest.json" {
				r.files["/"+name] = b
			}
		}
	}
}

// Fetches go through node_packages.proxy (internal/netproxy, never the environment's), with a timeout; a proxy
// that does not read stops them.
func TestNodePackagesProxyAndTimeout(t *testing.T) {
	site := newFakeRelease(t)
	d, _, _ := packageDevices(t, "v2", site.srv.URL)
	type use struct {
		proxy   string
		timeout time.Duration
	}
	var uses []use
	d.packages.httpClient = func(proxy *url.URL, timeout time.Duration) *http.Client {
		p := ""
		if proxy != nil {
			p = proxy.Host
		}
		uses = append(uses, use{p, timeout})
		return http.DefaultClient
	}
	d.mu.Lock()
	d.cfg.NodePackagesProxy = "http://[fd00::20]:3128"
	d.mu.Unlock()
	_, _ = d.nodePackageFor(context.Background(), otherOS, otherArch, "")
	if len(uses) == 0 || uses[0].proxy != "[fd00::20]:3128" || uses[0].timeout != packageLookup {
		t.Fatalf("uses %+v", uses)
	}
	d.packages.httpClient = nil
	c, err := d.packageClient(packageDownload)
	if err != nil || c.Timeout != packageDownload {
		t.Fatalf("netproxy client: %+v %v", c, err)
	}
	d.mu.Lock()
	d.cfg.NodePackagesProxy = "socks5://[fd00::20]:1080"
	d.mu.Unlock()
	if _, err := d.packageClient(packageLookup); err == nil || !strings.Contains(err.Error(), "node_packages.proxy") {
		t.Fatalf("a bad proxy: %v", err)
	}
}

// A node on the fallback build is up to date: no mismatch, no redeploy on connect, the card shows the build it runs.
// One on another build is offered the fallback; with no build at all, Redeploy is not offered and the card says why.
func TestNodeOnTheFallbackBuildIsUpToDate(t *testing.T) {
	plat := otherOS + "-" + otherArch
	site := newFakeRelease(t)
	site.publish(t, "v9-1", overlapping("v9-1", 9), true, map[string]string{"personad-" + plat: updatetest.Program(t, "personad", otherOS, otherArch)})
	d, _, _ := packageDevices(t, "1.2.3", site.srv.URL)
	m := newMachine(d, MachineConfig{ID: "box", Name: "box", Kind: "ssh"})
	d.addEntry(m)
	m.mu.Lock()
	m.online = true
	m.info = proto.NodeInfo{OS: otherOS, Arch: otherArch, Version: "v9-1", Protocol: nodeproto.Version,
		ProtocolMin: nodeproto.MinManagerVersion, Capabilities: nodeproto.Announced}
	m.mu.Unlock()
	if got, _ := d.resolvedTargetKnown(context.Background(), m.info); got != "v9-1" {
		t.Fatalf("target %s", got)
	}
	if s := m.snapshot(); s.NodeMismatch || s.NodeVersion != "v9-1" || s.CanDeploy == nil || !*s.CanDeploy {
		t.Fatalf("on the fallback: %+v", s)
	}
	m.updateOnConnect(context.Background())
	if s := m.snapshot(); s.Deploying {
		t.Fatal("redeployed a node on the fallback build")
	}
	m.mu.Lock()
	m.info.Version = "v8-1"
	m.mu.Unlock()
	if s := m.snapshot(); !s.NodeMismatch {
		t.Fatalf("another build: %+v", s)
	}

	// No build for its platform anywhere: not offered, and the card says why.
	m.mu.Lock()
	m.info.Arch = "mips"
	m.mu.Unlock()
	_, _ = d.resolvedTargetKnown(context.Background(), m.info)
	s := m.snapshot()
	want := i18n.T(i18n.Default, "devices-deploy.no_package", i18n.Args{"os": otherOS, "arch": "mips", "version": "v9-1",
		"name": "personad-" + otherOS + "-mips"})
	if s.CanDeploy == nil || *s.CanDeploy || s.DeployReason != want || !s.DeployNoPackage {
		t.Fatalf("no build: %v %q no_package=%v", s.CanDeploy, s.DeployReason, s.DeployNoPackage)
	}
	if err := m.startDeploy(); err == nil || err.Error() != want {
		t.Fatalf("Redeploy anyway: %v", err)
	}
}

func (r *fakeRelease) setDown(down bool) {
	r.mu.Lock()
	r.down = down
	r.mu.Unlock()
}

// While the release site is away, what was found before stands: a node on the fallback build stays up to date and can
// be sent the cached build; after a restart (nothing remembered) the cache's newest fallback this manager drives is
// used, and one it does not drive is not.
func TestTheSiteAwayKeepsWhatWasFound(t *testing.T) {
	plat := otherOS + "-" + otherArch
	site := newFakeRelease(t)
	site.publish(t, "v9-1", overlapping("v9-1", 9), true, map[string]string{"personad-" + plat: updatetest.Program(t, "personad", otherOS, otherArch)})
	d, _, _ := packageDevices(t, "1.2.3", site.srv.URL)
	now := time.Now()
	d.packages.now = func() time.Time { return now }
	ctx := context.Background()
	info := proto.NodeInfo{OS: otherOS, Arch: otherArch, Version: "v9-1"}
	pkg, err := d.nodePackageFor(ctx, otherOS, otherArch, "")
	if err != nil || !pkg.Fallback || pkg.Version != "v9-1" {
		t.Fatalf("%+v %v", pkg, err)
	}
	if _, err := d.fetchPackage(ctx, pkg); err != nil {
		t.Fatal(err)
	}

	site.setDown(true)
	now = now.Add(packageFoundTTL + time.Second) // what was found is old now: looked up again, and the site is away
	if v, known := d.resolvedTargetKnown(ctx, info); v != "v9-1" || !known {
		t.Fatalf("target while the site is away: %s %v", v, known)
	}
	if err := d.packageBlocked(info); err != nil {
		t.Fatalf("blocked while the site is away: %v", err)
	}
	pkg, err = d.nodePackageFor(ctx, otherOS, otherArch, "")
	if err != nil || pkg.Version != "v9-1" {
		t.Fatalf("Redeploy while the site is away: %+v %v", pkg, err)
	}
	if bin, err := d.fetchPackage(ctx, pkg); err != nil || update.ProgramOf(bin) != nodeproto.ProgramPersonad {
		t.Fatalf("the cached build: %s %v", bin, err)
	}
	// The stand-in is looked up again soon, not in ten minutes.
	asked := len(site.requests())
	now = now.Add(packageMissTTL + time.Second)
	_, _ = d.resolvedTargetKnown(ctx, info)
	if len(site.requests()) == asked {
		t.Fatal("the site was not asked again once it may be back")
	}

	// A manager that started since (nothing remembered): the cache's fallback, judged again.
	e := newTestDevices(t)
	e.mu.Lock()
	e.cfg.HubVersion, e.cfg.NodePackagesURL = "1.2.3", site.srv.URL
	e.mu.Unlock()
	pkg, err = e.nodePackageFor(ctx, otherOS, otherArch, "")
	if err != nil || pkg.Version != "v9-1" || !pkg.Fallback || pkg.path == "" {
		t.Fatalf("after a restart: %+v %v", pkg, err)
	}
	_, entry := cachePaths("v9-1", update.File{Name: "personad-" + plat + ".gz"})
	var ce packageCacheEntry
	b, _ := os.ReadFile(entry)
	_ = json.Unmarshal(b, &ce)
	ce.NodeProtocol, ce.NodeProtocolMin = nodeproto.MinVersion-1, nodeproto.MinVersion-1 // a node this manager does not drive
	b, _ = json.Marshal(ce)
	_ = os.WriteFile(entry, b, 0o644)
	f := newTestDevices(t)
	f.mu.Lock()
	f.cfg.HubVersion, f.cfg.NodePackagesURL = "1.2.3", site.srv.URL
	f.mu.Unlock()
	if _, err = f.nodePackageFor(ctx, otherOS, otherArch, ""); !siteAway(err) {
		t.Fatalf("a cached build this manager does not drive: %v", err)
	}
}

// A remembered build stands while it is looked up again: a node on it does not show out of date in between, also
// when the site is slow to answer.
func TestATargetStandsWhileItIsLookedUpAgain(t *testing.T) {
	plat := otherOS + "-" + otherArch
	site := newFakeRelease(t)
	site.publish(t, "v9-1", overlapping("v9-1", 9), true, map[string]string{"personad-" + plat: updatetest.Program(t, "personad", otherOS, otherArch)})
	d, _, _ := packageDevices(t, "1.2.3", site.srv.URL)
	now := time.Now()
	d.packages.now = func() time.Time { return now }
	m := newMachine(d, MachineConfig{ID: "box", Name: "box", Kind: "ssh"})
	d.addEntry(m)
	m.mu.Lock()
	m.online = true
	m.info = proto.NodeInfo{OS: otherOS, Arch: otherArch, Version: "v9-1"}
	m.mu.Unlock()
	if got, _ := d.resolvedTargetKnown(context.Background(), m.info); got != "v9-1" {
		t.Fatalf("target %s", got)
	}
	site.mu.Lock()
	site.delay = 2 * time.Second
	site.mu.Unlock()
	now = now.Add(packageFoundTTL + time.Second)
	for i := 0; i < 3; i++ {
		if s := m.snapshot(); s.NodeMismatch || s.CanDeploy == nil || !*s.CanDeploy {
			t.Fatalf("while it is looked up again: %+v", s)
		}
	}
}

// A user and password in node_packages.url never reach the machine's card or the log.
func TestSiteErrorsHideTheCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	u.User = url.UserPassword("mirror-user", "mirror-secret")
	d, _, _ := packageDevices(t, "1.2.3", u.String())
	_, err := d.nodePackageFor(context.Background(), otherOS, otherArch, "")
	text := i18n.DeploymentText(i18n.En, err.Error())
	if err == nil || strings.Contains(text, "mirror-secret") || strings.Contains(text, "mirror-user") || !strings.Contains(text, "HTTP 500") {
		t.Fatalf("%v", text)
	}
}

// The cache keeps the hub's version, the build just fetched and what was used within a day (a node of another
// platform may still be sent it); staging directories of interrupted downloads go, a running one stays.
func TestCacheIsPruned(t *testing.T) {
	plat := otherOS + "-" + otherArch
	site := newFakeRelease(t)
	site.publish(t, "v9-1", overlapping("v9-1", 9), true, map[string]string{"personad-" + plat: updatetest.Program(t, "personad", otherOS, otherArch)})
	d, _, _ := packageDevices(t, "1.2.3", site.srv.URL)
	root := identity.NodePackageCacheDir()
	for _, dir := range []string{"v7-1", "v8-1", "1.2.3", ".fetch-old", ".fetch-running"} {
		_ = os.MkdirAll(filepath.Join(root, dir), 0o755)
		_ = os.WriteFile(filepath.Join(root, dir, "x"), []byte("x"), 0o644)
	}
	old := time.Now().Add(-3 * packageDownload)
	_ = os.Chtimes(filepath.Join(root, ".fetch-old"), old, old)
	unused := time.Now().Add(-cacheKeep - time.Hour)
	_ = os.Chtimes(filepath.Join(root, "v7-1"), unused, unused)
	_ = os.Chtimes(filepath.Join(root, "1.2.3"), unused, unused)
	pkg, err := d.nodePackageFor(context.Background(), otherOS, otherArch, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.fetchPackage(context.Background(), pkg); err != nil {
		t.Fatal(err)
	}
	var left []string
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, " ") != ".fetch-running 1.2.3 v8-1 v9-1" {
		t.Fatalf("cache: %v", left)
	}
}
