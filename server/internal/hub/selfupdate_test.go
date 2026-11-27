package hub

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/update"
)

// fakePublisher serves a release whose binary is a shell script answering `version -v` as the real one does.
type fakePublisher struct {
	srv     *httptest.Server
	corrupt bool // serve another file than the one in the manifest
	script  []byte
	extra   map[string][]byte // more downloads by path (a program's)
	asked   func(path string) // every request's path, if set (tests that count downloads)
}

func newPublisher(t *testing.T, version string, build int64) *fakePublisher {
	return newPublisherWith(t, version, build, func(*update.Manifest) {})
}

// newPublisherWith is newPublisher with the manifest changed first (its commit and ancestors).
func newPublisherWith(t *testing.T, version string, build int64, edit func(*update.Manifest)) *fakePublisher {
	t.Helper()
	p := &fakePublisher{script: []byte(fmt.Sprintf("#!/bin/sh\necho %s\necho 'channel release, build %d'\n", version, build))}
	gz := gz(p.script)
	outer, inner := sha256.Sum256(gz), sha256.Sum256(p.script)
	name := "personad-" + runtime.GOOS + "-" + runtime.GOARCH + ".gz"
	m := update.Manifest{Version: version, Build: build, Protocol: proto.Protocol, Released: time.Now().UTC().Truncate(time.Second),
		Files: map[string]update.File{runtime.GOOS + "-" + runtime.GOARCH: {Name: name, Size: int64(len(gz)),
			SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}}}
	edit(&m)
	data, _ := json.Marshal(m)
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.asked != nil {
			p.asked(r.URL.Path)
		}
		switch r.URL.Path {
		case "/dl/latest.json":
			w.Write(data)
		case "/dl/" + name:
			if p.corrupt {
				w.Write(bytes.Repeat([]byte{0}, len(gz)))
				return
			}
			w.Write(gz)
		default:
			if b, ok := p.extra[r.URL.Path]; ok {
				w.Write(b)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func gz(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

// updatingHub is a hub of build 202611021500 that updates from p, running target (its "binary"), with one machine.
// execs returns a copy of the paths the hub exec'd so far: the update runs exec on its own goroutine.
func updatingHub(t *testing.T, p *fakePublisher, channel string) (*Hub, *Machine, string, func() []string) {
	t.Helper()
	h := newTestHub(t)
	h.build = update.Make("v1-202611021500", channel, "202611021500", p.srv.URL+"/dl")
	h.version = h.build.Version
	u := h.upd
	u.client = p.srv.Client()
	u.idleEvery, u.confirmIn, u.manualIdle = 20*time.Millisecond, 300*time.Millisecond, 2*time.Second
	target := filepath.Join(t.TempDir(), "personad")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	u.target = func() (string, error) { return target, nil }
	u.services = serviceFinder{} // not this computer's
	var mu sync.Mutex
	var execd []string
	u.exec = func(path string) error { mu.Lock(); execd = append(execd, path); mu.Unlock(); return nil }
	execs := func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(execd) }
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "ssh"})
	m.online = true
	h.machines, h.byID["box"] = append(h.machines, m), m
	return h, m, target, execs
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func setSessions(m *Machine, ss ...proto.Session) {
	m.mu.Lock()
	m.sessions = ss
	m.mu.Unlock()
}

// A download that does not match the manifest leaves the binary as it is, and the history says why.
func TestUpdateBadDownloadChangesNothing(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	p.corrupt = true
	h, _, target, execs := updatingHub(t, p, "release")
	h.upd.check(context.Background())
	if err := h.upd.begin(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure", func() bool { return h.upd.view().State == "failed" })
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" || len(execs()) != 0 {
		t.Fatal("a bad download replaced the binary")
	}
	if hs := h.upd.settings.History; len(hs) != 1 || hs[0].Result != "failed" {
		t.Fatalf("history %+v", hs)
	}
}

// An update that fails once its files are downloaded (here the new binary says another version than the release's)
// leaves none of them in update/<build>/: not the hub's binary, its .gz or the node binaries staged beside it.
func TestUpdateFailureLeavesNoDownload(t *testing.T) {
	p := newPublisherWith(t, "v2-202611021700", 202611021700, func(m *update.Manifest) {
		m.Version = "v2-202611021700-other" // the binary says v2-202611021700: not the release's
	})
	h, _, target, execs := updatingHub(t, p, "release")
	h.cfg.AutoUpdateNodes = true // the node binaries are staged before the check
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure", func() bool { return h.upd.view().State == "failed" })
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" || len(execs()) != 0 {
		t.Fatal("a binary that is not the release's was installed")
	}
	if !strings.Contains(h.upd.view().Error, "v2-202611021700-other") {
		t.Fatalf("failed for another reason: %q", h.upd.view().Error)
	}
	if left, err := os.ReadDir(filepath.Join(h.upd.dir, "202611021700")); !os.IsNotExist(err) {
		t.Fatalf("the failed update's downloads stayed: %v (%v)", left, err)
	}
}

// A start removes the downloads of every update but the one this process is confirming: an older update's (one
// that went back through the watchdog or the edge, which run the old build) and anything an older build left.
// The markers, the logs and what is not an update's download stay.
func TestUpdateStartPrunesOldDownloads(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, m, target, _ := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	h.version = h.build.Version
	m.mu.Lock()
	m.online = false // the update stays being confirmed while the test looks
	m.mu.Unlock()
	h.upd.confirmIn = time.Hour
	dir := h.upd.dir
	older, current, other := filepath.Join(dir, "202611011200"), filepath.Join(dir, "202611021700"), filepath.Join(dir, "notes")
	for _, d := range []string{filepath.Join(older, "nodes"), filepath.Join(current, "nodes"), other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "personad-darwin-arm64"), []byte("a binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "watch.log"), []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}
	pending := update.Pending{FromBuild: 202611021500, ToBuild: 202611021700, Target: target, Prev: target + ".prev",
		Online: []string{"box"}, Nodes: filepath.Join(current, "nodes")}
	if err := update.Save(update.PendingPath(dir), pending); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.upd.start(ctx)
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Fatalf("an older update's downloads stayed: %v", err)
	}
	for _, keep := range []string{filepath.Join(current, "nodes", "personad-darwin-arm64"), filepath.Join(other, "personad-darwin-arm64"),
		filepath.Join(dir, "watch.log"), update.PendingPath(dir)} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("%s went: %v", keep, err)
		}
	}
}

// A confirm that finds the marker claimed by the watchdog (update.Watch reached its deadline first) leaves the update
// to it: nothing is recorded as updated and nobody is told, so the rollback the old version records stands alone.
func TestUpdateConfirmLeavesAClaimedUpdateToTheWatchdog(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, _ := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	told := 0
	h.upd.notify = func(string, string) { told++ }
	h.upd.edgeConfirmed = func() { told++ }
	pending := update.Pending{FromBuild: 202611021500, ToBuild: 202611021700, Target: target, Prev: target + ".prev", Online: []string{"box"}, Auto: true}
	update.Save(update.PendingPath(h.upd.dir), pending)
	bootUpdater(t, h.upd)
	os.Rename(update.PendingPath(h.upd.dir), update.PendingPath(h.upd.dir)+".watch") // the watchdog's claim
	h.upd.confirm(context.Background(), pending)
	if h.upd.view().Confirmed || len(h.upd.settings.History) != 0 || told != 0 {
		t.Fatalf("confirmed an update the watchdog is undoing: history %+v, told %d", h.upd.settings.History, told)
	}
}

// A watchdog that died while it held the claim (power loss, or a stop of the hub's unit, whose cgroup it shares)
// leaves pending.json.watch: the next start takes it as the marker. The new build counts its start and confirms; the
// old build records the rollback, so the build is marked bad.
func TestUpdateBootTakesBackAWatchdogsClaim(t *testing.T) {
	pending := update.Pending{FromVersion: "v1-202611021500", FromBuild: 202611021500, ToVersion: "v2-202611021700", ToBuild: 202611021700, Online: []string{"box"}}
	t.Run("new build", func(t *testing.T) {
		p := newPublisher(t, "v2-202611021700", 202611021700)
		h, _, target, execs := updatingHub(t, p, "release")
		h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
		pending.Target, pending.Prev = target, target+".prev"
		update.Save(update.PendingPath(h.upd.dir)+".watch", pending)
		if !bootUpdater(t, h.upd) || len(execs()) != 0 {
			t.Fatalf("the new build has no update to confirm (execd %v)", execs())
		}
		var back update.Pending
		if ok, _ := update.Load(update.PendingPath(h.upd.dir), &back); !ok || back.Starts != 1 {
			t.Fatalf("the start is not counted: %v %+v", ok, back)
		}
		if _, err := os.Stat(update.PendingPath(h.upd.dir) + ".watch"); !os.IsNotExist(err) {
			t.Fatal("the claim stayed")
		}
	})
	t.Run("old build", func(t *testing.T) {
		p := newPublisher(t, "v2-202611021700", 202611021700)
		h, _, target, _ := updatingHub(t, p, "release")
		pending.Target, pending.Prev = target, target+".prev"
		update.Save(update.PendingPath(h.upd.dir)+".watch", pending)
		if bootUpdater(t, h.upd) {
			t.Fatal("the old build thinks it has an update to confirm")
		}
		var rb update.RolledBack
		if ok, _ := update.Load(update.RolledBackPath(h.upd.dir), &rb); !ok || rb.ToBuild != 202611021700 {
			t.Fatal("the rollback is not written down")
		}
		if left, _ := filepath.Glob(update.PendingPath(h.upd.dir) + "*"); len(left) != 0 {
			t.Fatalf("left %v behind", left)
		}
	})
}

// A build deployed straight from master during an update's checks clears the marker without calling it a failure.
func TestUpdateDirectDeployIsNotARollback(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execs := updatingHub(t, p, "release")
	h.build = update.Make("deployed-202611021800", "release", "202611021800", p.srv.URL+"/dl")
	update.Save(update.PendingPath(h.upd.dir), update.Pending{FromBuild: 202611021500, ToBuild: 202611021700, Target: target, Prev: target + ".prev"})
	bootUpdater(t, h.upd)
	if _, err := os.Stat(update.RolledBackPath(h.upd.dir)); !os.IsNotExist(err) {
		t.Fatal("a direct deploy was recorded as a rollback (its release would be marked bad)")
	}
	if _, err := os.Stat(update.PendingPath(h.upd.dir)); !os.IsNotExist(err) || len(execs()) != 0 {
		t.Fatal("the marker stayed, or it restarted")
	}
}

// Each start is counted once: the third failed start of a new version still runs it, the fourth goes back.
func TestUpdateStartsCountedOnce(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execs := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	os.WriteFile(target+".prev", []byte("old"), 0o755)
	update.Save(update.PendingPath(h.upd.dir), update.Pending{ToBuild: 202611021700, Target: target, Prev: target + ".prev"})
	for i := 1; i <= updateMaxStarts; i++ {
		if !bootUpdater(t, h.upd) || len(execs()) != 0 {
			t.Fatalf("start %d went back already", i)
		}
	}
	bootUpdater(t, h.upd)
	if len(execs()) != 1 {
		t.Fatal("start 4 did not go back")
	}
}

// A new version that keeps dying in a service manager's restart loop goes back on its next start.
func TestUpdateCrashLoopGoesBack(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execs := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	os.WriteFile(target+".prev", []byte("old"), 0o755)
	update.Save(update.PendingPath(h.upd.dir), update.Pending{ToBuild: 202611021700, Target: target, Prev: target + ".prev", Starts: updateMaxStarts})
	bootUpdater(t, h.upd)
	if b, _ := os.ReadFile(target); string(b) != "old" || len(execs()) != 1 {
		t.Fatal("a crash loop did not go back")
	}
}

// A start count that cannot be saved (full or read-only disk, wrong owner) would leave a crash loop uncounted
// forever: the new version goes back on that start instead, and says why in the log.
func TestUpdateUnsavedStartCountGoesBack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execs := updatingHub(t, p, "release")
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	os.WriteFile(target+".prev", []byte("old"), 0o755)
	if err := update.Save(update.PendingPath(h.upd.dir), update.Pending{ToBuild: 202611021700, Target: target, Prev: target + ".prev", Starts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(h.upd.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(h.upd.dir, 0o700) })
	bootUpdater(t, h.upd)
	if b, _ := os.ReadFile(target); string(b) != "old" || len(execs()) != 1 {
		t.Fatalf("an unsaved start count kept the new version (execd %v)", execs())
	}
	for _, want := range []string{"save start count", "save rollback marker", "remove pending marker"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log misses %q:\n%s", want, logs.String())
		}
	}
}

// Only a release build updates itself: a dev build and the Docker image never replace their binary, whatever is out.
func TestUpdateNeverOnDevOrDocker(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	for _, ch := range []string{"dev", "docker"} {
		h, _, _, _ := updatingHub(t, p, ch)
		srv := httptest.NewServer(h.routes())
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/hub/update", strings.NewReader(`{"now":true}`))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		srv.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s: %d", ch, resp.StatusCode)
		}
		if ch == "docker" && !strings.Contains(body.Error, "docker compose") {
			t.Fatalf("docker: %q", body.Error)
		}
		if v := h.upd.view(); v.CanUpdate || v.Auto || v.AutoNotice {
			t.Fatalf("%s: %+v", ch, v)
		}
	}
}

// A build deployed from master (its build number its build time) is never replaced by an older or equal release.
func TestUpdateNoDowngrade(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, _, _ := updatingHub(t, p, "release")
	h.build = update.Make("deployed-202611021800", "release", "202611021800", p.srv.URL+"/dl")
	h.upd.check(context.Background())
	if v := h.upd.view(); v.State != "idle" || v.Latest.Newer {
		t.Fatalf("an older release looks newer: %+v %+v", v.State, v.Latest)
	}
	if err := h.upd.begin(context.Background(), false); !errors.Is(err, errNoUpdate) {
		t.Fatalf("downgrade: %v", err)
	}
	if h.upd.wantsAuto() {
		t.Fatal("would downgrade automatically")
	}
}

// Commits for the anti-rollback tests: the release is published from b (a before it); a hub deployed straight
// from master runs d, which came after b.
const (
	gitA = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	gitB = "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	gitD = "d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4"
)

func publishedAtB(t *testing.T) *fakePublisher {
	return newPublisherWith(t, "v2-202611021700", 202611021700, func(m *update.Manifest) {
		m.Commit = gitB
		m.Ancestors, _ = update.EncodeAncestors([]string{gitB, gitA})
	})
}

// A release that contains the hub's commit installs by itself as before, and nothing is held.
func TestUpdateAutoWhenTheReleaseContainsIt(t *testing.T) {
	p := publishedAtB(t)
	h, _, _, _ := updatingHub(t, p, "release")
	h.build.Commit = gitA
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if v := h.upd.view(); v.Latest.Held != "" || !h.upd.wantsAuto() {
		t.Fatalf("held %q, auto %v", v.Latest.Held, h.upd.wantsAuto())
	}
	h.upd.noteHeld(ctx)
	if len(h.upd.settings.History) != 0 {
		t.Fatalf("history %+v", h.upd.settings.History)
	}
}

// With auto-update off nothing would install by itself anyway: no held record either.
func TestUpdateHeldNotNotedWithAutoOff(t *testing.T) {
	p := publishedAtB(t)
	h, _, _, _ := updatingHub(t, p, "release")
	h.build.Commit = gitD
	off := false
	h.upd.settings.Auto = &off
	h.upd.check(context.Background())
	h.upd.noteHeld(context.Background())
	if len(h.upd.settings.History) != 0 {
		t.Fatalf("history %+v", h.upd.settings.History)
	}
}

// Auto-update is on by default and the app is told once; turning it off sticks and counts as told.
func TestUpdateSettings(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, _, _ := updatingHub(t, p, "release")
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	call := func(method, path, body string) updateView {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var v updateView
		json.NewDecoder(resp.Body).Decode(&v)
		return v
	}
	if v := call("GET", "/api/v1/hub/update", ""); !v.Auto || !v.AutoNotice || !v.CanUpdate || v.Channel != "release" {
		t.Fatalf("defaults %+v", v)
	}
	if v := call("PUT", "/api/v1/hub/update/settings", `{"auto":false}`); v.Auto || v.AutoNotice {
		t.Fatalf("turned off %+v", v)
	}
	h.upd.check(context.Background())
	if h.upd.wantsAuto() {
		t.Fatal("updates automatically with auto off")
	}
}

// Once confirmed, the release's node binaries go where "Redeploy" takes them (deploy.go), and only machines whose
// node is another build, reached through the hub, are redeployed: never the hub's own machine.
func TestUpdateNodesBinariesInPlace(t *testing.T) {
	nodes := t.TempDir()
	t.Setenv("PERSONA_NODE_BINARIES", nodes)
	staged := t.TempDir()
	os.WriteFile(filepath.Join(staged, "personad-darwin-arm64"), []byte("node for macs"), 0o755)
	os.WriteFile(filepath.Join(staged, "personad-darwin-arm64.gz"), []byte("the download"), 0o644)
	os.WriteFile(filepath.Join(nodes, "personad-darwin-arm64"), []byte("old node"), 0o755)
	// What the old one was said to be (docs/operations/config.md "Node builds"): not the release's, so it goes with it.
	os.WriteFile(filepath.Join(nodes, "personad-darwin-arm64.version"), []byte("v1-202611010000\n"), 0o644)
	h := newTestHub(t)
	h.version = "v2"
	h.cfg.AutoUpdateNodes = true
	local := newMachine(h, MachineConfig{ID: "hub", Name: "Hub", Kind: "local"})
	local.online, local.info.Version = true, "v1" // the hub's own: runs the hub's binary, never deployed to
	h.machines, h.byID["hub"] = []*Machine{local}, local
	h.upd.updateNodes(context.Background(), staged, "")
	if b, _ := os.ReadFile(filepath.Join(nodes, "personad-darwin-arm64")); string(b) != "node for macs" {
		t.Fatalf("node binary %q", b)
	}
	if _, err := os.Stat(filepath.Join(nodes, "personad-darwin-arm64.gz")); !os.IsNotExist(err) {
		t.Fatal("the download itself went in with the binaries")
	}
	if _, err := os.Stat(filepath.Join(nodes, "personad-darwin-arm64.version")); !os.IsNotExist(err) {
		t.Fatal("the old build's version file stayed beside the release's")
	}
	_ = local // the manager decides what to redeploy (internal/devices TestUpdateNodesSkipsTheHubsOwn)
}

// A Docker hub never replaces itself, but it looks up its image's tags and says when a newer release is out, and how
// to get it (a pinned tag is changed in .env); a private image without a token says what to set.
func TestUpdateDockerSaysNewImage(t *testing.T) {
	private := false
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if private {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v2/owner/persona/tags/list" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"name":"owner/persona","tags":["0.3.0","0.4.0","0.5.0-rc1","edge","latest"]}`)
	}))
	defer reg.Close()
	host := strings.TrimPrefix(reg.URL, "http://")
	h := newTestHub(t)
	h.build, h.version = update.Make("0.3.0", "docker", "", ""), "0.3.0"
	h.upd.client = reg.Client()
	ctx := context.Background()

	t.Setenv("PERSONA_IMAGE", host+"/owner/persona:latest")
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	v := h.upd.view()
	if v.State != "available" || v.Latest == nil || v.Latest.Version != "0.4.0" || !v.Latest.Newer || v.CanUpdate {
		t.Fatalf("a newer image is not offered: %+v %+v", v, v.Latest)
	}
	if v.Image == nil || v.Image.Name != host+"/owner/persona" || v.Image.Tag != "latest" || v.Image.Pinned {
		t.Fatalf("image %+v", v.Image)
	}
	t.Setenv("PERSONA_IMAGE", host+"/owner/persona:0.3.0")
	if v := h.upd.view(); v.Image == nil || !v.Image.Pinned {
		t.Fatalf("a pinned tag: %+v", v.Image)
	}

	h.version = "0.4.0"
	h.upd.check(ctx)
	if v := h.upd.view(); v.State != "idle" || v.Latest.Newer {
		t.Fatalf("the newest image offered again: %+v", v)
	}

	private = true
	h.upd.check(ctx)
	if v := h.upd.view(); !strings.Contains(v.Error, "PERSONA_REGISTRY_TOKEN") {
		t.Fatalf("a private image does not say what to set: %q", v.Error)
	}

	t.Setenv("PERSONA_IMAGE", "")
	if _, err := h.upd.check(ctx); err == nil || !strings.Contains(h.upd.view().Error, "PERSONA_IMAGE") {
		t.Fatalf("no image to look at: %v, %q", err, h.upd.view().Error)
	}
	h.build.Image = host + "/owner/persona"
	private = false
	if _, err := h.upd.check(ctx); err != nil || h.upd.view().Image.Tag != "latest" {
		t.Fatalf("the image it was built as: %v %+v", err, h.upd.view().Image)
	}
}

// A program's download that is not the release's build stops the update before anything is swapped.
func TestUpdateRefusesAWrongProgram(t *testing.T) {
	programs := update.ProgramNames
	update.ProgramNames = append(append([]string(nil), programs...), "persona-examplechat")
	t.Cleanup(func() { update.ProgramNames = programs })
	plat := runtime.GOOS + "-" + runtime.GOARCH
	prog := []byte("#!/bin/sh\necho v1-old\necho 'channel release, build 1'\n") // says another build
	pgz := gz(prog)
	outer, inner := sha256.Sum256(pgz), sha256.Sum256(prog)
	pf := update.File{Name: "persona-examplechat-" + plat + ".gz", Size: int64(len(pgz)), SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}
	p := newPublisherWith(t, "v2-202611021700", 202611021700, func(m *update.Manifest) {
		m.Programs = map[string]map[string]update.File{"persona-examplechat": {plat: pf}}
	})
	p.extra = map[string][]byte{"/dl/" + pf.Name: pgz}
	h, _, target, execs := updatingHub(t, p, "release")
	ctx := context.Background()
	h.upd.check(ctx)
	if err := h.upd.begin(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure", func() bool { return h.upd.view().State == "failed" })
	if len(execs()) != 0 {
		t.Fatal("restarted")
	}
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" {
		t.Fatal("the hub's binary was swapped")
	}
	if !strings.Contains(h.upd.view().Error, "persona-examplechat") {
		t.Fatalf("error %q", h.upd.view().Error)
	}
}
