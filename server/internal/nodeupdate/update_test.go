package nodeupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/update/updatetest"
)

var fixtureDir string
var fixtureOnce sync.Once
var fixtureErr error

func TestMain(m *testing.M) {
	var err error
	fixtureDir, err = os.MkdirTemp("", "node-update-fixtures-")
	if err != nil {
		panic(err)
	}
	// Keep all fixture processes away from the developer's runtime roots. Reuse
	// only Go's build/module caches so isolated HOME does not redownload tools.
	for _, key := range []string{"GOCACHE", "GOMODCACHE"} {
		if os.Getenv(key) == "" {
			if out, err := exec.Command("go", "env", key).Output(); err == nil {
				_ = os.Setenv(key, strings.TrimSpace(string(out)))
			}
		}
	}
	_ = os.Setenv("HOME", filepath.Join(fixtureDir, "home"))
	_ = os.Setenv("PERSONA_HOME", filepath.Join(fixtureDir, "runtime"))
	_ = os.Setenv("PERSONA_HOME", filepath.Join(fixtureDir, "runtime"))
	_ = os.MkdirAll(os.Getenv("HOME"), 0700)
	_ = os.Setenv("INVOCATION_ID", "")
	_ = os.Setenv("XPC_SERVICE_NAME", "")
	// Never contact the host's service manager, even on Linux CI. This fake
	// launches the real watchdog process but records the transient-unit command.
	fakeBin := filepath.Join(fixtureDir, "fake-bin")
	_ = os.MkdirAll(fakeBin, 0700)
	fake := "#!/bin/sh\nif [ -n \"$NODE_UPDATE_FAKE_SYSTEMD_LOG\" ]; then printf '%s\\n' \"$@\" >\"$NODE_UPDATE_FAKE_SYSTEMD_LOG\"; fi\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != \"--\" ]; do shift; done\n[ \"$#\" -gt 0 ] || exit 2\nshift\n\"$@\" </dev/null >/dev/null 2>&1 &\n"
	_ = os.WriteFile(filepath.Join(fakeBin, "systemd-run"), []byte(fake), 0700)
	_ = os.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	code := m.Run()
	_ = os.RemoveAll(fixtureDir)
	os.Exit(code)
}
func fixtures(t *testing.T) (string, string) {
	t.Helper()
	fixtureOnce.Do(func() {
		for _, version := range []string{"old", "next"} {
			cmd := exec.Command("go", "build", "-o", filepath.Join(fixtureDir, version), "-ldflags=-X main.version="+version, "./testdata")
			if out, err := cmd.CombinedOutput(); err != nil {
				fixtureErr = fmt.Errorf("build fixture: %w: %s", err, out)
				return
			}
		}
	})
	if fixtureErr != nil {
		t.Fatal(fixtureErr)
	}
	return filepath.Join(fixtureDir, "old"), filepath.Join(fixtureDir, "next")
}
func testManager(t *testing.T) (*Manager, []byte, *atomic.Int32) {
	t.Helper()
	old, next := fixtures(t)
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "resident")
	if err := copyFile(old, exe); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(next)
	if err != nil {
		t.Fatal(err)
	}
	count := new(atomic.Int32)
	m := &Manager{exe: exe, version: "old", timeout: time.Hour, restart: func(string) { count.Add(1) }, startWatch: func(string, string, string) error { return nil }}
	t.Cleanup(m.Close)
	return m, data, count
}
func spec(data []byte, version string) proto.NodeUpdateBeginParams {
	sum := sha256.Sum256(data)
	return proto.NodeUpdateBeginParams{Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Version: version}
}
func receive(t *testing.T, m *Manager, data []byte, p proto.NodeUpdateBeginParams) string {
	t.Helper()
	b, err := m.Begin(p)
	if err != nil {
		t.Fatal(err)
	}
	for offset := b.Offset; offset < int64(len(data)); {
		end := min(offset+MaxChunk, int64(len(data)))
		c, err := m.Chunk(proto.NodeUpdateChunkParams{ID: b.ID, Offset: offset, Data: data[offset:end]})
		if err != nil {
			t.Fatal(err)
		}
		offset = c.Offset
	}
	return b.ID
}
func hash(t *testing.T, path string) string {
	t.Helper()
	s, err := fileHash(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A personad (it may run a hub or a manager too) never becomes persona-node through an update, whatever the hub sends.
func TestPersonaNodeNeverReplacesPersonad(t *testing.T) {
	out := updatetest.Program(t, "persona-node", "", "")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	m, _, _ := testManager(t) // the resident is the fixture, a build of another package: personad
	before := hash(t, m.exe)
	id := receive(t, m, data, spec(data, "next"))
	if _, err := m.Commit(context.Background(), id, nil); err == nil || !strings.Contains(err.Error(), "not replacing it with persona-node") {
		t.Fatalf("persona-node over personad: %v", err)
	}
	if hash(t, m.exe) != before {
		t.Fatal("persona-node replaced personad")
	}
}

func TestResumeRetriesLimitsConcurrencyAndCancellation(t *testing.T) {
	m, data, _ := testManager(t)
	p := spec(data, "next")
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := m.Begin(p)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- b.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("concurrent begin created two uploads")
		}
		id = got
	}
	other := p
	other.Version = "another"
	if _, err := m.Begin(other); err == nil {
		t.Fatal("accepted concurrent different update")
	}
	chunk := data[:MaxChunk]
	if _, err := m.Chunk(proto.NodeUpdateChunkParams{ID: id, Data: chunk}); err != nil {
		t.Fatal(err)
	}
	b, err := m.Begin(p)
	if err != nil || b.ID != id || b.Offset != MaxChunk {
		t.Fatalf("resume %+v %v", b, err)
	}
	if c, err := m.Chunk(proto.NodeUpdateChunkParams{ID: id, Data: chunk}); err != nil || c.Offset != MaxChunk {
		t.Fatal("duplicate chunk", c, err)
	}
	changed := bytes.Clone(chunk)
	changed[0] ^= 1
	if _, err := m.Chunk(proto.NodeUpdateChunkParams{ID: id, Data: changed}); err == nil {
		t.Fatal("conflicting retry accepted")
	}
	if _, err := m.Chunk(proto.NodeUpdateChunkParams{ID: id, Offset: MaxChunk + 1, Data: chunk}); err == nil {
		t.Fatal("offset gap accepted")
	}
	if _, err := m.Chunk(proto.NodeUpdateChunkParams{ID: id, Offset: MaxChunk, Data: make([]byte, MaxChunk+1)}); err == nil {
		t.Fatal("oversized chunk accepted")
	}
	path := m.upload.Path
	if err := m.Abort(id); err != nil {
		t.Fatal(err)
	}
	if err := m.Abort(id); err != nil {
		t.Fatal("abort retry", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("abort left staged executable")
	}
	if _, err := m.Chunk(proto.NodeUpdateChunkParams{ID: id, Data: chunk}); err == nil {
		t.Fatal("aborted upload accepted data")
	}
	p.Size = MaxSize + 1
	if _, err := m.Begin(p); err == nil {
		t.Fatal("unbounded upload accepted")
	}
}

func TestSameVersionCannotHideDifferentLoadedCodeFromRollbackBarrier(t *testing.T) {
	m, data, _ := testManager(t)
	if _, err := m.Begin(spec(data, "old")); err == nil {
		t.Fatal("same-version reinstall would make loaded-code rollback identity ambiguous")
	}
}

func TestExpiryAndCloseCleanStages(t *testing.T) {
	m, data, _ := testManager(t)
	b, err := m.Begin(spec(data, "next"))
	if err != nil {
		t.Fatal(err)
	}
	path := m.upload.Path
	m.upload.At = time.Now().Add(-UploadTTL - time.Second)
	if _, err := m.Chunk(proto.NodeUpdateChunkParams{ID: b.ID, Data: data[:1]}); err == nil {
		t.Fatal("expired upload accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired stage remains")
	}
	if _, err := m.Begin(spec(data, "next")); err != nil {
		t.Fatal(err)
	}
	path = m.upload.Path
	m.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("closed resident left stage")
	}
}

func replaceExternally(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.CreateTemp(filepath.Dir(path), ".external-deploy-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Chmod(0755); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		t.Fatal(err)
	}
}

func TestProcessIdentityIgnoresResidentAndWatchdogTimezone(t *testing.T) {
	t.Setenv("TZ", "Pacific/Honolulu")
	a, err := processStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TZ", "Asia/Tokyo")
	b, err := processStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || a != b {
		t.Fatalf("process identity changed with service timezone: %q != %q", a, b)
	}
}

func fixturePeer(t *testing.T, path string) (*rpc.Peer, net.Conn) {
	t.Helper()
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	peer := rpc.New("test", c)
	go func() { _ = peer.Serve(context.Background(), c) }()
	return peer, c
}
func call(t *testing.T, p *rpc.Peer, method string, params, result any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Call(ctx, method, params, result); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for fixture state")
}

func TestCapabilityRequiresResidentSupervisor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XPC_SERVICE_NAME", "")
	t.Setenv("INVOCATION_ID", "")
	if ok, reason := New("old", func(string) {}, Options{}).Capability(); ok || reason == "" {
		t.Fatal("unsupervised node advertised safe update")
	}
	var m *Manager
	if ok, reason := m.Capability(); ok || reason == "" {
		t.Fatal("stdio node advertised update")
	}
}

// onlyPS makes PATH a folder with just ps: what a container without systemd has, and no systemd-run.
func onlyPS(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	if script == "" {
		ps, err := exec.LookPath("ps")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(ps, filepath.Join(bin, "ps")); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(filepath.Join(bin, "ps"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestDeclaredSupervisorEnablesUpdatesWithoutServiceManager(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("a declared supervisor counts on Linux and macOS only")
	}
	t.Setenv("XPC_SERVICE_NAME", "")
	t.Setenv("INVOCATION_ID", "")
	onlyPS(t, "")
	declared := New("old", func(string) {}, Options{Supervised: true})
	t.Cleanup(declared.Close)
	if ok, reason := declared.Capability(); !ok {
		t.Fatalf("a node whose supervisor said so cannot take updates: %s", reason)
	}
	if declared.Executable() == "" {
		t.Fatal("no executable resolved")
	}
	undeclared := New("old", func(string) {}, Options{})
	t.Cleanup(undeclared.Close)
	if ok, _ := undeclared.Capability(); ok {
		t.Fatal("a node nobody restarts advertised safe update")
	}
}

func TestDeclaredSupervisorNeedsPsThatReportsStartTimes(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("a declared supervisor counts on Linux and macOS only")
	}
	t.Setenv("XPC_SERVICE_NAME", "")
	t.Setenv("INVOCATION_ID", "")
	// BusyBox's ps fails on -p / -o lstart; another may answer with nothing.
	for name, script := range map[string]string{
		"busybox": "#!/bin/sh\necho \"ps: invalid option -- 'p'\" >&2\nexit 1\n",
		"empty":   "#!/bin/sh\nexit 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			onlyPS(t, script)
			m := New("old", func(string) {}, Options{Supervised: true})
			t.Cleanup(m.Close)
			if ok, reason := m.Capability(); ok || reason != psNoStart {
				t.Fatalf("got %v %q, want the ps reason", ok, reason)
			}
		})
	}
	// The hub shows the node's reason in the app's language by matching this original text.
	if got := i18n.T(i18n.ZhHans, "devices-deploy.node_ps_no_start"); got != psNoStart {
		t.Fatalf("hub catalog has %q for the node's %q", got, psNoStart)
	}
}

// superviseFixture runs the fixture the way a container's entry script runs the node: `node --attach --supervised`,
// started again shortly after every exit, with neither systemd's nor launchd's environment.
type fixtureSupervisor struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	starts  int
	stopped bool
	done    chan struct{}
}

func (s *fixtureSupervisor) current() (pid, starts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd.Process.Pid, s.starts
}

type fixtureProof struct {
	Version string
	PID     int
}

// proofOf reads what the running fixture wrote about itself; false while there is none (or it is being written).
func proofOf(path string) (fixtureProof, bool) {
	var p fixtureProof
	b, err := os.ReadFile(path)
	return p, err == nil && json.Unmarshal(b, &p) == nil
}
