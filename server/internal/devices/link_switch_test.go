package devices

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// The pairing link the fake hub hands out: it must reach the computer's installer on stdin and nowhere else.
const (
	lsTestLink = "persona://link?hub=hub.example.com&token=SECRET-PAIRING-TOKEN-7f3a9c"
	lsTestHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// lsTestTimes are the steps' times, short.
var lsTestTimes = linkSwitchTimes{Probe: 20 * time.Second, Transfer: 20 * time.Second, Begin: 5 * time.Second,
	Install: 20 * time.Second, Connect: 3 * time.Second, EndBackoff: 20 * time.Millisecond, EndBackoffMax: 200 * time.Millisecond,
	EndGiveUp: time.Minute,
	Uninstall: 20 * time.Second, SSHBack: 3 * time.Second, Total: time.Minute, Resume: 300 * time.Millisecond,
	Shown: time.Hour, Poll: 20 * time.Millisecond}

// lsHub is the hub of a switch: it answers begin and end as the hub does (switching the entry's kind and handing the
// manager its new list), and anything else as fakeHost.
type lsHub struct {
	*fakeHost
	h *lsHarness

	mu     sync.Mutex
	cfg    devicewire.Config
	begins []devicewire.LinkSwitchBegin
	ends   []devicewire.LinkSwitchEnd
	refuse string // begin answers this code
	drop   bool   // begin switches the entry, its answer lost
	// endFail: how many ends fail first (-1: every one); endCode: every end answers this code; endHeld: every end
	// fails while set (the hub away), as many tries as there are.
	endFail int
	endHeld atomic.Bool
	endCode string
	onBegin func()
	onEnd   func(p devicewire.LinkSwitchEnd)
	// beginErr: begin fails with it after switching the entry (an error the hub did not code as a refusal).
	beginErr error
}

func remarshal(in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (hub *lsHub) Call(ctx context.Context, method string, params, out any) error {
	switch method {
	case devicewire.MethodLinkSwitchBegin:
		var p devicewire.LinkSwitchBegin
		_ = remarshal(params, &p)
		hub.mu.Lock()
		hub.begins = append(hub.begins, p)
		refuse, drop, hook, berr := hub.refuse, hub.drop, hub.onBegin, hub.beginErr
		hub.mu.Unlock()
		hub.h.note("hub begin")
		if refuse != "" {
			return &rpc.Error{Code: refuse, Message: "refused"}
		}
		if hook != nil {
			hook()
		}
		hub.setKind(kindLink)
		if drop {
			return rpc.ErrDropped
		}
		if berr != nil {
			return berr
		}
		return remarshal(devicewire.LinkSwitchBegun{Link: lsTestLink, PairHash: lsTestHash}, out)
	case devicewire.MethodLinkSwitchEnd:
		var p devicewire.LinkSwitchEnd
		_ = remarshal(params, &p)
		hub.mu.Lock()
		hub.ends = append(hub.ends, p)
		n, fail, code, hook := len(hub.ends), hub.endFail, hub.endCode, hub.onEnd
		hub.mu.Unlock()
		hub.h.note("hub end " + p.Outcome)
		if hook != nil {
			hook(p)
		}
		if fail < 0 || n <= fail || hub.endHeld.Load() {
			return errors.New("the hub is away")
		}
		if code != "" {
			return &rpc.Error{Code: code, Message: "refused"}
		}
		if p.Outcome == devicewire.LinkSwitchRolledBack {
			hub.setKind("ssh")
		}
		return nil
	}
	return hub.fakeHost.Call(ctx, method, params, out)
}

// setKind makes the entry of kind and hands the manager the list, as devices.config does.
func (hub *lsHub) setKind(kind string) {
	hub.mu.Lock()
	cfg := hub.cfg
	var mc map[string]any
	_ = json.Unmarshal(cfg.Machines[0], &mc)
	mc["kind"] = kind
	b, _ := json.Marshal(mc)
	cfg.Machines = []json.RawMessage{b}
	hub.cfg = cfg
	hub.mu.Unlock()
	hub.h.d.Configure(cfg)
}

func (hub *lsHub) kind() string {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var mc MachineConfig
	_ = json.Unmarshal(hub.cfg.Machines[0], &mc)
	return mc.Kind
}

func (hub *lsHub) calls() ([]devicewire.LinkSwitchBegin, []devicewire.LinkSwitchEnd) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return slices.Clone(hub.begins), slices.Clone(hub.ends)
}

type lsCall struct {
	what string
	argv []string
}

// lsHarness is a manager with one ssh computer, box: its ssh runs the switch's real scripts with sh in a temporary
// home (stubs for systemctl and, as the build sent, for persona-link), its node is an in-process one reached by the
// machine's kind (ssh while the entry says ssh, Persona Link once the stub installer said it connected).
type lsHarness struct {
	t                     *testing.T
	d                     *Devices
	hub                   *lsHub
	store                 *memStore
	node                  *testNode
	root, home, stubs     string
	runDir, pkg           string
	sshDown, nodeDown     atomic.Bool
	mu                    sync.Mutex
	calls                 []lsCall
	order                 []string
	gates                 map[string]chan struct{}
	force                 map[string]int
	forceOut              map[string]string // what a forced script prints
	onRun                 func(what string) // before each script runs
	pkgErr                error
	logs                  bytes.Buffer
	cancel                context.CancelFunc
	extraEnv              []string
	persona, units, binOf string
}

// lsStubLink is the build the tests send: persona-link's commands as a switch uses them, against $HOME. The
// installer writes what came on its stdin to .pair-got and puts a node service back from node-switch.backup, as
// persona-link's does; files in $HOME make it hang (.hang), fail before it paired (.fail-pair), after it paired
// (.fail-install), never connect (.no-connect) or fail to uninstall (.fail-uninstall).
const lsStubLink = `#!/bin/sh
D="$HOME/.persona"; U="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
case "$1 $2" in
"version "*) echo "persona-link test"; exit 0 ;;
"install --pair")
	[ "$3" = "-" ] || exit 2
	cat > "$D/.pair-got"
	if [ -e "$HOME/.hang" ]; then exec sleep 30; fi
	if [ -e "$HOME/.fail-pair" ]; then exit 1; fi
	mkdir -p "$U"
	echo '{}' > "$D/link.json"
	if [ -e "$D/node-switch.backup" ]; then mv "$D/node-switch.backup" "$U/personad-node.service"; fi
	[ -e "$U/personad-node.service" ] || echo written-by-the-installer > "$U/personad-node.service"
	echo unit > "$U/persona-link.service"
	if [ -e "$HOME/.fail-install" ]; then exit 1; fi
	if [ ! -e "$HOME/.no-connect" ]; then : > "$D/.connected"; fi
	exit 0 ;;
"uninstall --purge")
	if [ -e "$HOME/.fail-uninstall" ]; then exit 1; fi
	rm -f "$U/persona-link.service" "$D/link.json" "$D/bin/persona-link" "$D/.connected"
	exit 0 ;;
"node uninstall")
	rm -f "$U/personad-node.service"; exit 0 ;;
esac
exit 2
`

type lsOptions struct {
	kind  string // the entry's kind at the start ("ssh")
	stub  string // the build's content (lsStubLink)
	setup func(h *lsHarness)
}

func newLSHarness(t *testing.T, o lsOptions) *lsHarness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the switch's scripts run on Linux computers")
	}
	for _, tool := range []string{"sh", "gzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		if _, err := exec.LookPath("shasum"); err != nil {
			t.Skip("no sha256sum or shasum")
		}
	}
	o.kind = orDefault(o.kind, "ssh")
	o.stub = orDefault(o.stub, lsStubLink)
	root := t.TempDir()
	h := &lsHarness{t: t, root: root, home: filepath.Join(root, "home"), stubs: filepath.Join(root, "stubs"),
		runDir: filepath.Join(root, "run"), pkg: filepath.Join(root, "pkg", "persona-link-linux-amd64"),
		gates: map[string]chan struct{}{}, force: map[string]int{}, forceOut: map[string]string{}}
	h.persona = filepath.Join(h.home, ".persona")
	h.binOf = filepath.Join(h.persona, "bin")
	h.units = filepath.Join(h.home, ".config", "systemd", "user")
	me, err := user.Current()
	if err != nil {
		t.Skip("no user name")
	}
	for _, dir := range []string{h.binOf, h.stubs, h.runDir, filepath.Dir(h.pkg), filepath.Join(root, "systemd"), filepath.Join(root, "linger")} {
		mustMkdir(t, dir)
	}
	for _, dir := range []string{h.home, h.persona, h.binOf} { // writable by the user alone, whatever the umask here
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(root, "linger", me.Username), "", 0o644)
	mustWrite(t, filepath.Join(h.binOf, "personad"), "#!/bin/sh\nexit 0\n", 0o755)
	mustWrite(t, filepath.Join(h.stubs, "systemctl"), "#!/bin/sh\nif [ -e \"$HOME/.no-systemd\" ]; then exit 1; fi\nexit 0\n", 0o755)
	mustWrite(t, h.pkg, o.stub, 0o755)
	h.node = &testNode{info: &proto.NodeInfo{Hostname: "box", OS: "linux", Arch: "amd64", Home: "/home/me",
		DataDir: "/home/me/.persona", Version: "test"}}

	h.store = newMemStore()
	h.hub = &lsHub{fakeHost: &fakeHost{}, h: h}
	logf := func(format string, args ...any) {
		h.mu.Lock()
		fmt.Fprintf(&h.logs, format+"\n", args...)
		h.mu.Unlock()
	}
	d := New(Options{Hub: h.hub, Store: h.store, Logf: logf})
	h.d = d
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	entry, _ := json.Marshal(MachineConfig{ID: "box", Name: "Box", Kind: o.kind, SSHHost: "box.example"})
	cfg := devicewire.Config{HubID: "hub-test", HubName: "test", HubVersion: "test", LinkSwitch: true,
		Endpoints: []devicewire.LinkEndpoint{{URL: "https://hub.example.com", Kind: "public"}}, Machines: []json.RawMessage{entry}}
	h.hub.cfg = cfg
	d.mu.Lock()
	d.ctx, d.got, d.cfg = ctx, true, cfg
	d.mu.Unlock()
	close(d.gotCh)
	d.switches.times = lsTestTimes
	d.switches.run = h.run
	d.switches.pkg = func(ctx context.Context, goos, arch string) (string, string, error) {
		if goos != "linux" || arch != "amd64" {
			return "", "", fmt.Errorf("asked for %s/%s", goos, arch)
		}
		if h.pkgErr != nil {
			return "", "", h.pkgErr
		}
		return h.pkg, "test", nil
	}
	d.switches.paths = lsPaths{systemd: filepath.Join(root, "systemd"), linger: filepath.Join(root, "linger")}
	d.opts.DialNode = h.dial
	if o.setup != nil {
		o.setup(h)
	}
	t.Cleanup(h.close)
	d.Configure(cfg)
	if o.kind == "ssh" {
		eventually(t, 5*time.Second, "box online over ssh", func() bool { return d.sshOnline("box") })
	}
	return h
}

func orDefault(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func (h *lsHarness) close() {
	h.cancel()
	done := make(chan struct{})
	go func() {
		h.d.switches.wg.Wait()
		h.d.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		h.t.Error("the switch or the machines did not stop")
	}
}

func (h *lsHarness) note(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.order = append(h.order, s)
}

func (h *lsHarness) orderSeen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.order)
}

func (h *lsHarness) whats() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, c := range h.calls {
		out = append(out, c.what)
	}
	return out
}

// hold keeps the script what from running until the returned func is called.
func (h *lsHarness) hold(what string) func() {
	gate := make(chan struct{})
	h.mu.Lock()
	h.gates[what] = gate
	h.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

// dial is DialNode: made for each new machine with its entry's kind as the hub has it then.
func (h *lsHarness) dial(id string) func(context.Context) (net.Conn, error) {
	kind := h.hub.kind()
	return func(ctx context.Context) (net.Conn, error) {
		for {
			up := !h.nodeDown.Load()
			switch kind {
			case kindLink:
				_, err := os.Stat(filepath.Join(h.persona, ".connected"))
				up = up && err == nil
			default:
				up = up && !h.sshDown.Load()
			}
			if up {
				return h.node.dial(ctx)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

// run is the computer's ssh: the remote command run by sh in the temporary home.
func (h *lsHarness) run(ctx context.Context, what string, argv []string, stdin io.Reader, stdout io.Writer) (int, error) {
	h.mu.Lock()
	h.calls = append(h.calls, lsCall{what: what, argv: slices.Clone(argv)})
	h.order = append(h.order, "ssh "+what)
	gate := h.gates[what]
	code, forced := h.force[what]
	forcedOut := h.forceOut[what]
	env := slices.Clone(h.extraEnv)
	onRun := h.onRun
	h.mu.Unlock()
	if onRun != nil {
		onRun(what)
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return -1, ctx.Err()
		}
	}
	if forced {
		_, _ = io.Copy(io.Discard, stdin)
		_, _ = io.WriteString(stdout, forcedOut)
		return code, nil
	}
	if h.sshDown.Load() {
		return 255, nil
	}
	if argv[0] != "ssh" || argv[len(argv)-2] != "box.example" {
		h.t.Errorf("ssh to the wrong place: %q", argv)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", argv[len(argv)-1])
	cmd.Env = append(append(shellEnv(h.home, h.stubs), "XDG_RUNTIME_DIR="+h.runDir), env...)
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit) && exit.Exited():
		if s := strings.TrimSpace(stderr.String()); s != "" {
			h.t.Logf("%s exited %d: %s", what, exit.ExitCode(), s)
		}
		return exit.ExitCode(), nil
	}
	return -1, err
}

// post starts a switch over HTTP as a device of role (or the admin page: role "admin").
func (h *lsHarness) post(role string) (int, map[string]json.RawMessage, string) {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/machines/box/link-switch", nil)
	if role == "admin" {
		req.Header.Set(assistantwire.AdminHeader, "1")
	} else if role != "" {
		req.Header.Set(assistantwire.RoleHeader, role)
	}
	rec := httptest.NewRecorder()
	h.d.Handler(http.NotFoundHandler()).ServeHTTP(rec, req)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body, rec.Body.String()
}

// start posts as a full device and wants it accepted.
func (h *lsHarness) start() proto.MachineLinkSwitch {
	h.t.Helper()
	code, body, raw := h.post("full")
	if code != http.StatusAccepted {
		h.t.Fatalf("POST link-switch: %d %s", code, raw)
	}
	var v proto.MachineLinkSwitch
	if err := json.Unmarshal(body["link_switch"], &v); err != nil {
		h.t.Fatalf("answer %s: %v", raw, err)
	}
	if strings.Contains(raw, lsTestLink) || strings.Contains(raw, "SECRET") {
		h.t.Fatal("the answer carries the pairing link")
	}
	return v
}

// finished waits for the switch to end and is its record.
func (h *lsHarness) finished() linkSwitchRecord {
	h.t.Helper()
	var rec *linkSwitchRecord
	eventually(h.t, 30*time.Second, "the switch ends", func() bool {
		rec = h.d.linkSwitchRecordOf("box")
		return rec.finished()
	})
	eventually(h.t, 10*time.Second, "its goroutine ends", func() bool {
		h.d.switches.mu.Lock()
		defer h.d.switches.mu.Unlock()
		return !h.d.switches.active["box"]
	})
	return *h.d.linkSwitchRecordOf("box") // as its goroutine left it
}

// stored is the record the settings have.
func (h *lsHarness) stored() linkSwitchRecord {
	h.t.Helper()
	var rec linkSwitchRecord
	if ok, err := h.store.Setting(context.Background(), linkSwitchKey("box"), &rec); !ok || err != nil {
		h.t.Fatalf("no stored record: %v", err)
	}
	return rec
}

// noSecret checks the pairing link is nowhere but the installer's stdin: not in the log, the settings, an ssh
// command line.
func (h *lsHarness) noSecret() {
	h.t.Helper()
	h.mu.Lock()
	logs := h.logs.String()
	calls := slices.Clone(h.calls)
	h.mu.Unlock()
	if strings.Contains(logs, lsTestLink) || strings.Contains(logs, "SECRET") {
		h.t.Errorf("the log has the pairing link:\n%s", logs)
	}
	for _, c := range calls {
		for _, a := range c.argv {
			if strings.Contains(a, lsTestLink) || strings.Contains(a, "SECRET") {
				h.t.Errorf("the pairing link is on ssh's command line (%s): %q", c.what, a)
			}
		}
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	for k, v := range h.store.kv {
		if strings.Contains(string(v), lsTestLink) || strings.Contains(string(v), "SECRET") {
			h.t.Errorf("the settings keep the pairing link in %s", k)
		}
	}
}

func (h *lsHarness) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(h.home, rel))
	return err == nil
}

// tree is every file under the home with its content.
func (h *lsHarness) tree() map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(h.home, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(h.home, p)
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "-> " + target
		case !e.IsDir():
			b, _ := os.ReadFile(p)
			out[rel] = string(b)
		default:
			out[rel+"/"] = ""
		}
		return nil
	})
	return out
}

func stagedName(op string) string { return filepath.Join(".persona", "bin", lsStagePrefix+op) }

// Sending the build fails (ssh drops, the build does not run there): nothing asked of the hub, the staged build
// removed, the computer on ssh as before.
func TestLinkSwitchTransferFails(t *testing.T) {
	for _, c := range []struct {
		name  string
		stub  string
		setup func(h *lsHarness)
	}{
		{"ssh drops", "", func(h *lsHarness) { h.force["stage"] = 255 }},
		{"the build does not run", "#!/bin/sh\nexit 1\n", nil},
		{"no build", "", func(h *lsHarness) { h.pkgErr = errNoConnector }},
		{"the release site is away", "", func(h *lsHarness) { h.pkgErr = errors.New("release site: connection refused") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newLSHarness(t, lsOptions{stub: c.stub, setup: c.setup})
			h.start()
			rec := h.finished()
			want := proto.LinkSwitchReasonTransferFailed
			if h.pkgErr != nil {
				want = proto.LinkSwitchReasonNoPackage
			}
			if rec.Phase != proto.LinkSwitchPhaseFailed || rec.Error != want || rec.RollbackUnconfirmed || rec.BeginIntended {
				t.Fatalf("record %+v", rec)
			}
			if begins, ends := h.hub.calls(); len(begins)+len(ends) > 0 {
				t.Errorf("the hub was asked: %+v %+v", begins, ends)
			}
			wantRuns := []string{"probe", "stage", "remove staged"}
			if h.pkgErr != nil {
				wantRuns = []string{"probe"}
			}
			if got := h.whats(); !slices.Equal(got, wantRuns) {
				t.Errorf("ssh runs %q", got)
			}
			if h.exists(stagedName(rec.Operation)) || h.exists(filepath.Join(".persona", "bin", "persona-link")) {
				t.Error("the build is left on the computer")
			}
		})
	}
}

// Only a full device or the admin page may start one; who did is recorded.
func TestLinkSwitchNeedsAFullDevice(t *testing.T) {
	h := newLSHarness(t, lsOptions{setup: func(h *lsHarness) { _ = os.RemoveAll(filepath.Join(h.root, "linger")) }})
	for _, role := range []string{"", "read", "extension"} {
		if code, _, raw := h.post(role); code != http.StatusForbidden {
			t.Errorf("role %q: %d %s", role, code, raw)
		}
	}
	if rec := h.d.linkSwitchRecordOf("box"); rec != nil {
		t.Fatalf("started: %+v", rec)
	}
	if code, _, raw := h.post("admin"); code != http.StatusAccepted {
		t.Fatalf("admin: %d %s", code, raw)
	}
	if rec := h.finished(); rec.By != "admin" || rec.Error != proto.LinkSwitchReasonLingerOff {
		t.Errorf("record %+v", rec)
	}
}

// Not available: 409 with the reason, nothing started.
func TestLinkSwitchNotAvailable(t *testing.T) {
	h := newLSHarness(t, lsOptions{})
	h.d.mu.Lock()
	h.d.cfg.LinkSwitch = false
	h.d.mu.Unlock()
	code, body, raw := h.post("full")
	var c string
	_ = json.Unmarshal(body["code"], &c)
	if code != http.StatusConflict || c != proto.LinkSwitchReasonHubOld {
		t.Fatalf("%d %s", code, raw)
	}
	if rec := h.d.linkSwitchRecordOf("box"); rec != nil || len(h.whats()) > 0 {
		t.Errorf("started: %+v %q", rec, h.whats())
	}
}

// A stored unfinished switch blocks a start even before the restarted manager looked at it.
func TestLinkSwitchStartReadsTheStoredRecord(t *testing.T) {
	h := newLSHarness(t, lsOptions{})
	_ = h.store.PutSetting(context.Background(), linkSwitchKey("box"), linkSwitchRecord{Operation: "ls_1", MachineID: "box",
		Phase: proto.LinkSwitchPhaseConnecting, BeginIntended: true})
	code, body, raw := h.post("full")
	var c string
	_ = json.Unmarshal(body["code"], &c)
	if code != http.StatusConflict || c != proto.LinkSwitchReasonInProgress {
		t.Fatalf("%d %s", code, raw)
	}
}

// What the machine list says of a switch, cheaply: for an ssh computer this manager connects, whether it can start
// and why not, in a fixed order; for others nothing, but the switch that ran on them within the hour.
func TestLinkSwitchAvailability(t *testing.T) {
	linux := proto.NodeInfo{OS: "linux", Arch: "arm64", Home: "/home/me", DataDir: "/home/me/.persona"}
	type mc = MachineConfig
	cases := []struct {
		name   string
		cfg    mc
		info   proto.NodeInfo
		online bool
		edit   func(d *Devices, m *Machine)
		want   *proto.MachineLinkSwitch // nil: none shown
	}{
		{"available", mc{Kind: "ssh"}, linux, true, nil, &proto.MachineLinkSwitch{Available: true}},
		{"kind unset is ssh", mc{}, linux, true, nil, &proto.MachineLinkSwitch{Available: true}},
		{"a switch runs", mc{Kind: "ssh"}, linux, true, func(d *Devices, m *Machine) {
			d.switches.records = map[string]*linkSwitchRecord{"box": {MachineID: "box", Phase: proto.LinkSwitchPhaseInstalling}}
			d.cfg.LinkSwitch = false // the order: in_progress first
		}, &proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonInProgress, Phase: proto.LinkSwitchPhaseInstalling}},
		{"no Persona Link at the hub", mc{Kind: "ssh"}, linux, false, func(d *Devices, m *Machine) {
			d.cfg.LinkSwitch, d.cfg.Endpoints = false, nil
		}, &proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonLinkUnavailable}},
		{"older hub", mc{Kind: "ssh"}, linux, false, func(d *Devices, m *Machine) { d.cfg.LinkSwitch = false },
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonHubOld}},
		{"pending", mc{Kind: "ssh"}, linux, false, func(d *Devices, m *Machine) { d.cfg.Pending = []string{"box"} },
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonPending}},
		{"pending unknown", mc{Kind: "ssh"}, linux, true, func(d *Devices, m *Machine) { d.cfg.PendingUnknown = true },
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonPending}},
		{"remote_bin", mc{Kind: "ssh", RemoteBin: "/opt/persona/personad"}, linux, false, nil,
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonCustomNode}},
		{"default remote_bin", mc{Kind: "ssh", RemoteBin: "~/.persona/bin/personad"}, linux, true, nil, &proto.MachineLinkSwitch{Available: true}},
		{"data directory", mc{Kind: "ssh"}, proto.NodeInfo{OS: "linux", Arch: "amd64", Home: "/home/me", DataDir: "/srv/persona"}, true, nil,
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonCustomNode}},
		{"data directory with a trailing slash", mc{Kind: "ssh"}, proto.NodeInfo{OS: "linux", Arch: "amd64", Home: "/home/me", DataDir: "/home/me/.persona/"},
			true, nil, &proto.MachineLinkSwitch{Available: true}},
		{"data directory not said (an older node: the probe looks)", mc{Kind: "ssh"}, proto.NodeInfo{OS: "linux", Arch: "amd64", Home: "/home/me"},
			true, nil, &proto.MachineLinkSwitch{Available: true}},
		{"offline", mc{Kind: "ssh"}, linux, false, nil, &proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonOffline}},
		{"never connected", mc{Kind: "ssh"}, proto.NodeInfo{}, false, nil, &proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonOffline}},
		{"mac", mc{Kind: "ssh"}, proto.NodeInfo{OS: "darwin", Arch: "arm64", Home: "/Users/me", DataDir: "/Users/me/.persona"}, true, nil,
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonUnsupportedOS}},
		{"windows", mc{Kind: "ssh"}, proto.NodeInfo{OS: "windows", Arch: "amd64", Home: `C:\Users\me`, DataDir: `C:\Users\me\.persona`}, true, nil,
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonUnsupportedOS}},
		{"32-bit", mc{Kind: "ssh"}, proto.NodeInfo{OS: "linux", Arch: "386", Home: "/home/me", DataDir: "/home/me/.persona"}, true, nil,
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonUnsupportedOS}},
		{"updating", mc{Kind: "ssh"}, linux, true, func(d *Devices, m *Machine) { m.deploying = true },
			&proto.MachineLinkSwitch{Reason: proto.LinkSwitchReasonBusy}},
		{"failed earlier", mc{Kind: "ssh"}, linux, true, func(d *Devices, m *Machine) {
			d.switches.records = map[string]*linkSwitchRecord{"box": {MachineID: "box", Phase: proto.LinkSwitchPhaseFailed,
				Error: proto.LinkSwitchReasonLingerOff, At: time.Unix(1e9, 0)}}
		}, &proto.MachineLinkSwitch{Available: true}}, // over an hour ago: not shown
		{"failed just now", mc{Kind: "ssh"}, linux, true, func(d *Devices, m *Machine) {
			d.switches.records = map[string]*linkSwitchRecord{"box": {MachineID: "box", Phase: proto.LinkSwitchPhaseFailed,
				Error: proto.LinkSwitchReasonLinkTimeout, RollbackUnconfirmed: true, At: time.Unix(2e9, 0)}}
		}, &proto.MachineLinkSwitch{Available: true, Phase: proto.LinkSwitchPhaseFailed, Error: proto.LinkSwitchReasonLinkTimeout,
			RollbackUnconfirmed: true, At: time.Unix(2e9, 0)}},
		{"the hub's own computer", mc{Kind: "local"}, linux, true, nil, nil},
		{"Persona Link", mc{Kind: kindLink}, linux, true, nil, nil},
		{"Persona Link just switched", mc{Kind: kindLink}, linux, true, func(d *Devices, m *Machine) {
			d.switches.records = map[string]*linkSwitchRecord{"box": {MachineID: "box", Phase: proto.LinkSwitchPhaseSucceeded, At: time.Unix(2e9, 0)}}
		}, &proto.MachineLinkSwitch{Phase: proto.LinkSwitchPhaseSucceeded, At: time.Unix(2e9, 0)}},
		{"a member hub's computer", mc{Kind: "ssh"}, linux, true, func(d *Devices, m *Machine) { m.derived = true }, nil},
		{"this hub is a member", mc{Kind: "ssh"}, linux, true, func(d *Devices, m *Machine) { d.cfg.Mesh = &devicewire.Mesh{Member: true} }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newTestDevices(t)
			d.switches.times = lsTestTimes
			d.switches.times.Shown = time.Duration(1<<63 - 1)
			if c.name == "failed earlier" {
				d.switches.times.Shown = time.Hour
			}
			d.mu.Lock()
			d.cfg.LinkSwitch = true
			d.cfg.Endpoints = []devicewire.LinkEndpoint{{URL: "https://hub.example.com", Kind: "public"}}
			d.mu.Unlock()
			c.cfg.ID, c.cfg.Name = "box", "Box"
			m := newMachine(d, c.cfg)
			m.info, m.online = c.info, c.online
			d.addEntry(m)
			if c.edit != nil {
				d.mu.Lock()
				c.edit(d, m)
				d.mu.Unlock()
			}
			got := m.snapshot().LinkSwitch
			if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// The scripts read the same in every login shell a computer may have (sshd hands the command to it): the probe looks
// and the uninstall finds nothing to remove.
func TestLinkSwitchScriptsRunInEveryShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the switch's scripts run on Linux computers")
	}
	for _, shell := range loginShells(t) {
		home := t.TempDir()
		stubs := t.TempDir()
		mustWrite(t, filepath.Join(stubs, "systemctl"), "#!/bin/sh\nexit 0\n", 0o755)
		mustWrite(t, filepath.Join(home, ".persona", "bin", "personad"), "#!/bin/sh\n", 0o755)
		for _, script := range []string{lsProbeScript(lsPaths{systemd: home, linger: home}), lsUninstallScript("ls_0011", "", true, true)} {
			cmd := exec.Command(shell, "-c", script)
			cmd.Env = shellEnv(home, stubs)
			out, err := cmd.Output()
			kv := outputOf(out)
			if err != nil || (kv["end"] != "1" && kv["clean"] != "1") {
				t.Errorf("%s: %v %q", filepath.Base(shell), err, out)
			}
			if kv["end"] == "1" && (kv["node_bin"] != "1" || kv["link"] != "0" || kv["systemd"] != "1") {
				t.Errorf("%s: the probe found %v", filepath.Base(shell), kv)
			}
		}
	}
}

// What each answer of the hub's end says: taken ({}; not_ssh to a rollback, the hub revoked before it looked at the
// kind), the switch succeeded (already_succeeded to a rollback), never to be taken (no_machine, invalid_params; not_ssh
// to a success), or asked again (no answer, any other code).
func TestLinkSwitchEndAnswers(t *testing.T) {
	for _, c := range []struct {
		code, outcome string
		want          endAnswer
	}{
		{"", devicewire.LinkSwitchRolledBack, endTaken},
		{devicewire.LinkSwitchCodeNotSSH, devicewire.LinkSwitchRolledBack, endTaken},
		{devicewire.LinkSwitchCodeSucceeded, devicewire.LinkSwitchRolledBack, endSucceeded},
		{devicewire.LinkSwitchCodeNoMachine, devicewire.LinkSwitchRolledBack, endRefused},
		{"invalid_params", devicewire.LinkSwitchRolledBack, endRefused},
		{devicewire.LinkSwitchCodeChanged, devicewire.LinkSwitchRolledBack, endNoAnswer},
		{"unknown_method", devicewire.LinkSwitchRolledBack, endUnknown},
		{devicewire.LinkSwitchCodeRolledBackGone, devicewire.LinkSwitchRolledBack, endGone},
		{"unknown_method", devicewire.LinkSwitchSucceeded, endUnknown},
		{"", devicewire.LinkSwitchSucceeded, endTaken},
		{devicewire.LinkSwitchCodeSucceeded, devicewire.LinkSwitchSucceeded, endTaken},
		{devicewire.LinkSwitchCodeNotSSH, devicewire.LinkSwitchSucceeded, endRefused},
		{"invalid_params", devicewire.LinkSwitchSucceeded, endRefused},
		{devicewire.LinkSwitchCodeChanged, devicewire.LinkSwitchSucceeded, endNoAnswer},
	} {
		t.Run(c.outcome+"/"+c.code, func(t *testing.T) {
			h := newLSHarness(t, lsOptions{})
			h.hub.endCode = c.code
			r := &linkSwitchRun{d: h.d, t: lsTestTimes, rec: linkSwitchRecord{MachineID: "box", Operation: "ls_1", PairHash: lsTestHash}}
			if got := r.endOnce(context.Background(), c.outcome); got != c.want {
				t.Errorf("answer %d, want %d", got, c.want)
			}
		})
	}
	t.Run("not answered", func(t *testing.T) {
		h := newLSHarness(t, lsOptions{})
		h.hub.endHeld.Store(true)
		r := &linkSwitchRun{d: h.d, t: lsTestTimes, rec: linkSwitchRecord{MachineID: "box", Operation: "ls_1"}}
		if got := r.endOnce(context.Background(), devicewire.LinkSwitchRolledBack); got != endNoAnswer {
			t.Errorf("answer %d", got)
		}
	})
}

// installed puts on the computer what a switch's installer leaves: Persona Link placed (the build sent), paired, its
// service and the node service it set up.
func (h *lsHarness) installed() {
	b, _ := os.ReadFile(h.pkg)
	mustWrite(h.t, filepath.Join(h.binOf, "persona-link"), string(b), 0o755)
	mustWrite(h.t, filepath.Join(h.persona, "link.json"), "{}", 0o600)
	mustWrite(h.t, filepath.Join(h.units, "persona-link.service"), "unit", 0o644)
	mustWrite(h.t, filepath.Join(h.units, "personad-node.service"), "unit", 0o644)
}

// waitEnds waits until the hub was asked at least n ends.
// switchesDone waits until no goroutine of the manager runs a switch (one a resume may have started has ended).
func (h *lsHarness) switchesDone() {
	h.t.Helper()
	done := make(chan struct{})
	go func() { h.d.switches.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		h.t.Fatal("a switch still runs")
	}
}

func (h *lsHarness) waitEnds(n int) {
	h.t.Helper()
	eventually(h.t, 20*time.Second, fmt.Sprintf("%d ends asked", n), func() bool {
		_, ends := h.hub.calls()
		return len(ends) >= n
	})
}

// asUser has the computer's `id -un` say name (a stub first on PATH), with a linger file for it.
func (h *lsHarness) asUser(name string) {
	real, err := exec.LookPath("id")
	if err != nil {
		h.t.Skip("no id")
	}
	mustWrite(h.t, filepath.Join(h.stubs, "id"), "#!/bin/sh\nif [ \"$1\" = -un ]; then echo "+shellQuote(name)+"; exit 0; fi\nexec "+
		shellQuote(real)+" \"$@\"\n", 0o755)
	mustWrite(h.t, filepath.Join(h.root, "linger", name), "", 0o644)
}

// lsAs has the computer's user be user (asUser), and its ls say every directory is owned by user and of group
// (a stub first on PATH), but foreign ("": none), owned by someone else: the owners and groups a test cannot make.
func (h *lsHarness) lsAs(user, group, foreign string) {
	real, err := exec.LookPath("ls")
	if err != nil {
		h.t.Skip("no ls")
	}
	h.asUser(user)
	mustWrite(h.t, filepath.Join(h.stubs, "ls"), "#!/bin/sh\nfor a; do last=$a; done\n"+shellQuote(real)+
		` "$@" | awk -v p="$last" -v f="${LS_FOREIGN:-}" -v o="${LS_OWNER:-}" -v g="${LS_GROUP:-}" `+
		`'{ if (o != "") $3 = o; if (g != "") $4 = g; if (f != "" && p == f) $3 = "someone-else"; print }'`+"\n", 0o755)
	h.extraEnv = append(h.extraEnv, "LS_OWNER="+user, "LS_GROUP="+group, "LS_FOREIGN="+foreign)
}

// A restart that finds a switch whose installer never ran rolls back at once: no link can come.
func TestLinkSwitchResumeWithoutAnInstallRollsBackAtOnce(t *testing.T) {
	for _, install := range []string{"", lsInstallNotPlaced} {
		t.Run(orDefault(install, "none"), func(t *testing.T) {
			h := newLSHarness(t, lsOptions{kind: kindLink})
			_ = h.store.PutSetting(context.Background(), linkSwitchKey("box"), linkSwitchRecord{Operation: "ls_1", MachineID: "box",
				Phase: proto.LinkSwitchPhaseInstalling, Deadline: time.Now().Add(time.Minute), SSHHost: "box.example",
				BeginIntended: true, PairHash: lsTestHash, Install: install})
			begun := time.Now()
			h.d.resumeLinkSwitches(context.Background())
			rec := h.finished()
			if rec.Phase != proto.LinkSwitchPhaseFailed || rec.Error != proto.LinkSwitchReasonInterrupted || rec.RollbackUnconfirmed {
				t.Fatalf("record %+v", rec)
			}
			if took := time.Since(begun); took > 15*time.Second {
				t.Errorf("waited %s for a link that cannot come", took)
			}
		})
	}
}
