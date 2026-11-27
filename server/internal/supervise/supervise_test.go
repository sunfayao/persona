//go:build unix

package supervise

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/managerconf"
)

// The test binary stands in for the hub and the manager (`hub run` and persona-hub's `run`, `assistant`) when
// SUPERVISE_TEST_DIR is set: it notes each start (its pid and PERSONA_ASSISTANT_DEPLOY) in <dir>/<role>.starts and
// the program and arguments it ran as in <dir>/<role>.programs, exits with the code in <dir>/<role>.exit when that
// file shows up (taking it), stops on SIGTERM unless <dir>/<role>.stubborn is there.
func TestMain(m *testing.M) {
	dir := os.Getenv("SUPERVISE_TEST_DIR")
	if dir != "" && len(os.Args) > 1 {
		switch os.Args[1] {
		case "hub", "run":
			child(dir, "hub")
		case "assistant":
			child(dir, "assistant")
		}
	}
	os.Exit(m.Run())
}

// child is the fake hub or manager. It catches SIGTERM before it writes <role>.starts: the tests stop it as soon as
// they see that file.
func child(dir, role string) {
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	f, err := os.OpenFile(filepath.Join(dir, role+".starts"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(9)
	}
	fmt.Fprintf(f, "%d %s\n", os.Getpid(), os.Getenv("PERSONA_ASSISTANT_DEPLOY"))
	f.Close()
	if f, err := os.OpenFile(filepath.Join(dir, role+".programs"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(f, "%s %s\n", filepath.Base(os.Args[0]), strings.Join(os.Args[1:], " "))
		f.Close()
	}
	if _, err := os.Stat(filepath.Join(dir, role+".spawn")); err == nil { // leave something behind: a process of its own
		gc := exec.Command("sleep", "30")
		if _, err := os.Stat(filepath.Join(dir, role+".spawn-group")); err == nil { // in a group of its own (an extension)
			gc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		if gc.Start() == nil {
			_ = os.WriteFile(filepath.Join(dir, role+".grandchild"), []byte(strconv.Itoa(gc.Process.Pid)), 0o600)
		}
	}
	for {
		select {
		case <-term:
			if _, err := os.Stat(filepath.Join(dir, role+".stubborn")); err != nil {
				os.Exit(0)
			}
		case <-time.After(5 * time.Millisecond):
			exitFile := filepath.Join(dir, role+".exit")
			if b, err := os.ReadFile(exitFile); err == nil {
				_ = os.Remove(exitFile)
				code, _ := strconv.Atoi(strings.TrimSpace(string(b)))
				os.Exit(code)
			}
		}
	}
}

// syncBuffer is the supervisor's log, written from several goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type fixture struct {
	e        *env
	dir      string // the children's notes
	out      *syncBuffer
	mu       sync.Mutex
	execs    int
	execd    string // the last exec's program and arguments
	binds    []string
	bindErrs []error // what the next binds answer (nil after)
}

func newFixture(t *testing.T, hubJSON string) *fixture {
	t.Helper()
	old := []time.Duration{backoffFirst, backoffMost, steadyAfter, stopWait, bindRetry, modeRecheck, bindRetryMost}
	t.Cleanup(func() {
		backoffFirst, backoffMost, steadyAfter, stopWait, bindRetry, modeRecheck, bindRetryMost = old[0], old[1], old[2], old[3], old[4], old[5], old[6]
	})
	bindRetryMost = time.Second
	backoffFirst, backoffMost, steadyAfter, stopWait, bindRetry, modeRecheck =
		10*time.Millisecond, 40*time.Millisecond, time.Hour, 2*time.Second, 20*time.Millisecond, 20*time.Millisecond
	t.Setenv("PERSONA_LISTEN", "")
	t.Setenv(SeparateEnv, "")
	t.Setenv("PERSONA_SPLIT_PROGRAMS", "")
	home, dir := t.TempDir(), t.TempDir()
	if hubJSON != "" {
		if err := os.WriteFile(filepath.Join(home, "hub.json"), []byte(hubJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{dir: dir, out: &syncBuffer{}}
	f.e = &env{self: os.Args[0], home: home, out: f.out,
		environ: append(os.Environ(), "SUPERVISE_TEST_DIR="+dir),
		execHub: func(bin string, args, _ []string) error {
			f.mu.Lock()
			f.execs++
			f.execd = filepath.Base(bin) + " " + strings.Join(args, " ")
			f.mu.Unlock()
			return nil
		},
		bind: func(_ context.Context, hubURL string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.binds = append(f.binds, hubURL)
			if len(f.bindErrs) > 0 {
				err := f.bindErrs[0]
				f.bindErrs = f.bindErrs[1:]
				return err
			}
			return nil
		}}
	return f
}

// starts are the starts of role: pid and PERSONA_ASSISTANT_DEPLOY each.
func (f *fixture) starts(role string) [][2]string {
	b, _ := os.ReadFile(filepath.Join(f.dir, role+".starts"))
	var out [][2]string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		pid, deploy, _ := strings.Cut(l, " ")
		out = append(out, [2]string{pid, deploy})
	}
	return out
}

// stop makes role exit with code.
func (f *fixture) stop(t *testing.T, role string, code int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, role+".exit"), []byte(strconv.Itoa(code)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// running runs fn (the supervisor) until stop is called, which cancels it and waits for it to return.
func running(t *testing.T, fn func(ctx context.Context) error) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()
	return func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(15 * time.Second):
			t.Fatal("the supervisor did not return")
			return nil
		}
	}
}

// Not a service: the supervisor becomes the hub (exec), as the container was before; it starts nothing itself.
func TestChildModeIsTheHubAsBefore(t *testing.T) {
	for _, hubJSON := range []string{"", `{"token":"t"}`, `{"assistant":{"mode":"child"}}`, `{"assistant":{"mode":"service","external":true}}`} {
		f := newFixture(t, hubJSON)
		if err := f.e.hubContainer(context.Background()); err != nil || f.execs != 1 || len(f.starts("hub")) != 0 || len(f.binds) != 0 ||
			f.execd != filepath.Base(os.Args[0])+" hub run" {
			t.Fatalf("%s: err %v, execs %d %q, starts %v, binds %v", hubJSON, err, f.execs, f.execd, f.starts("hub"), f.binds)
		}
	}
}

// programs are the programs and arguments role ran as, one a start.
func (f *fixture) programs(role string) []string {
	b, _ := os.ReadFile(filepath.Join(f.dir, role+".programs"))
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// split puts the test binary as personad, persona-hub and persona-manager in a directory of its own, personad the
// supervisor's self, each answering `version -v` with builds[name] ("persona 1.0 abc" when not there).
func (f *fixture) split(t *testing.T, builds map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"personad", "persona-hub", "persona-manager"} {
		if err := os.Symlink(os.Args[0], filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	f.e.self = filepath.Join(dir, "personad")
	f.e.version = func(_ context.Context, p string) (string, error) {
		if b, ok := builds[filepath.Base(p)]; ok {
			return b, nil
		}
		return "persona 1.0 abc", nil
	}
}

// The image's programs of the same build: the hub runs as `persona-hub run` and the built-in manager as
// `persona-manager assistant`, side by side, or the hub alone (exec) when it starts the manager itself.
func TestSplitPrograms(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	f.split(t, nil)
	stop := running(t, f.e.hubContainer)
	waitFor(t, "both started", func() bool { return len(f.starts("hub")) == 1 && len(f.starts("assistant")) == 1 })
	_ = stop()
	if h, m := f.programs("hub"), f.programs("assistant"); len(h) != 1 || h[0] != "persona-hub run" || len(m) != 1 || m[0] != "persona-manager assistant" {
		t.Fatalf("hub %v, manager %v", h, m)
	}

	f = newFixture(t, `{"assistant":{"mode":"child"}}`)
	f.split(t, nil)
	if err := f.e.hubContainer(context.Background()); err != nil || f.execd != "persona-hub run" {
		t.Fatalf("%v %q", err, f.execd)
	}

	f = newFixture(t, `{"assistant":{"mode":"service"}}`)
	f.split(t, nil)
	stop = running(t, func(ctx context.Context) error { return f.e.managerOnly(ctx, "http://hub:18660") })
	waitFor(t, "the manager", func() bool { return len(f.starts("assistant")) == 1 })
	_ = stop()
	if m := f.programs("assistant"); len(m) != 1 || m[0] != "persona-manager assistant" {
		t.Fatalf("manager %v", m)
	}
}

// PERSONA_SPLIT_PROGRAMS=0 keeps both on personad, and asks nothing of the others; one of another build is passed
// over, saying why.
func TestSplitProgramsOff(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	f.split(t, nil)
	asked := false
	f.e.version = func(context.Context, string) (string, error) { asked = true; return "persona 1.0 abc", nil }
	t.Setenv("PERSONA_SPLIT_PROGRAMS", "0")
	stop := running(t, f.e.hubContainer)
	waitFor(t, "both started", func() bool { return len(f.starts("hub")) == 1 && len(f.starts("assistant")) == 1 })
	_ = stop()
	if h, m := f.programs("hub"), f.programs("assistant"); len(h) != 1 || h[0] != "personad hub run" || len(m) != 1 || m[0] != "personad assistant" || asked {
		t.Fatalf("hub %v, manager %v, asked %v", h, m, asked)
	}

	t.Setenv("PERSONA_SPLIT_PROGRAMS", "")
	f = newFixture(t, `{"assistant":{"mode":"service"}}`)
	f.split(t, map[string]string{"persona-manager": "persona 0.9 old"})
	stop = running(t, f.e.hubContainer)
	waitFor(t, "both started", func() bool { return len(f.starts("hub")) == 1 && len(f.starts("assistant")) == 1 })
	_ = stop()
	if h, m := f.programs("hub"), f.programs("assistant"); len(h) != 1 || h[0] != "persona-hub run" || len(m) != 1 || m[0] != "personad assistant" ||
		!strings.Contains(f.out.String(), "persona-manager is another build") || !strings.Contains(f.out.String(), "running personad assistant instead") {
		t.Fatalf("hub %v, manager %v\n%s", h, m, f.out.String())
	}
}

// A binding that fails is said and tried again; the manager starts once it works, the hub runs all along.
func TestBindingRetried(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	f.bindErrs = []error{errors.New("the hub did not answer"), errors.New("still not")}
	stop := running(t, f.e.hubContainer)
	waitFor(t, "the manager", func() bool { return len(f.starts("assistant")) == 1 })
	_ = stop()
	if h := f.starts("hub"); len(h) != 1 {
		t.Fatalf("hub starts %v", h)
	}
	if log := f.out.String(); strings.Count(log, "manager: the hub did not answer; trying again") != 1 || !strings.Contains(log, "still not") {
		t.Fatalf("log:\n%s", log)
	}
}

// The built-in manager in a container of its own: the hub's container runs the hub alone.
func TestSeparateManager(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	t.Setenv(SeparateEnv, "separate")
	stop := running(t, f.e.hubContainer)
	waitFor(t, "the hub", func() bool { return len(f.starts("hub")) == 1 })
	time.Sleep(100 * time.Millisecond)
	_ = stop()
	if len(f.starts("assistant")) != 0 || len(f.binds) != 0 {
		t.Fatalf("a manager here: %v %v", f.starts("assistant"), f.binds)
	}
}

// --manager-only: waits while the hub starts its own child, then runs the built-in manager bound to --hub.
func TestManagerOnly(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"child"}}`)
	stop := running(t, func(ctx context.Context) error { return f.e.managerOnly(ctx, "http://hub:18660") })
	time.Sleep(100 * time.Millisecond)
	if len(f.starts("assistant")) != 0 {
		t.Fatal("started in child mode")
	}
	if err := os.WriteFile(filepath.Join(f.e.home, "hub.json"), []byte(`{"assistant":{"mode":"service"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the manager", func() bool { return len(f.starts("assistant")) == 1 })
	_ = stop()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.binds) != 1 || f.binds[0] != "http://hub:18660" || len(f.starts("hub")) != 0 ||
		!strings.Contains(f.out.String(), "assistant.mode is not service") {
		t.Fatalf("binds %v, hub %v\n%s", f.binds, f.starts("hub"), f.out.String())
	}
}

// The hub's address on this machine is managerconf.LocalHubURL of its listen address (the hub and the manager use it
// too); the supervisor takes the default one for a listen address without a port as well.
func TestLoopback(t *testing.T) {
	for listen, want := range map[string]string{"": "http://127.0.0.1:18660", ":18765": "http://127.0.0.1:18765",
		"0.0.0.0:19000": "http://127.0.0.1:19000", "[::]:19001": "http://127.0.0.1:19001", "[fd00::20]:19002": "http://[fd00::20]:19002"} {
		if got := managerconf.LocalHubURL(listen); got != want {
			t.Errorf("LocalHubURL %q: %s, want %s", listen, got, want)
		}
		if got := loopback(listen); got != want {
			t.Errorf("%q: %s, want %s", listen, got, want)
		}
	}
	for _, listen := range []string{":", "192.0.2.1:", "[::]:"} {
		if got := loopback(listen); got != "http://127.0.0.1:18660" {
			t.Errorf("%q: %s, want the default address", listen, got)
		}
	}
}

// The hub starts again with another mode in hub.json (manager-mode, then an admin's restart of the hub): the
// supervisor stops everything and says why, so that the container starts again and decides anew.
func TestModeChangeStopsTheSupervisor(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	done := make(chan error, 1)
	go func() { done <- f.e.hubContainer(context.Background()) }()
	waitFor(t, "both", func() bool { return len(f.starts("hub")) == 1 && len(f.starts("assistant")) == 1 })
	if err := os.WriteFile(filepath.Join(f.e.home, "hub.json"), []byte(`{"assistant":{"mode":"child"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.stop(t, "hub", 0)
	select {
	case err := <-done:
		if !errors.Is(err, errChanged) || !errors.Is(err, ErrStopped) || len(f.starts("hub")) != 1 ||
			!strings.Contains(f.out.String(), "assistant.mode or listen address changed") {
			t.Fatalf("err %v, hub starts %d\n%s", err, len(f.starts("hub")), f.out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor did not stop")
	}
	pid, _ := strconv.Atoi(f.starts("assistant")[0][0])
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("the manager still runs")
	}
}

// A binding that keeps failing waits longer each time.
func TestBindRetryGrows(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	f.bindErrs = []error{errors.New("e1"), errors.New("e2"), errors.New("e3")}
	stop := running(t, f.e.hubContainer)
	waitFor(t, "the manager", func() bool { return len(f.starts("assistant")) == 1 })
	_ = stop()
	log := f.out.String()
	for _, want := range []string{"e1; trying again in 20ms", "e2; trying again in 40ms", "e3; trying again in 80ms"} {
		if !strings.Contains(log, want) {
			t.Fatalf("no %q in:\n%s", want, log)
		}
	}
}

// What a program leaves behind when it stops (a process of its own) is killed with its session: in its process group,
// or (on Linux) in a group of its own, as an extension's program is.
func TestLeftoversKilled(t *testing.T) {
	t.Run("group", func(t *testing.T) { testLeftoversKilled(t, false) })
	t.Run("own group", func(t *testing.T) {
		if self, _ := os.Readlink("/proc/self"); runtime.GOOS != "linux" || self != strconv.Itoa(os.Getpid()) {
			t.Skip("sessionMembers lists a session's processes on Linux only, with this pid namespace's /proc")
		}
		testLeftoversKilled(t, true)
	})
}

func testLeftoversKilled(t *testing.T, ownGroup bool) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	if err := os.WriteFile(filepath.Join(f.dir, "assistant.spawn"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if ownGroup {
		if err := os.WriteFile(filepath.Join(f.dir, "assistant.spawn-group"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stop := running(t, f.e.hubContainer)
	defer func() { _ = stop() }()
	var gc int
	waitFor(t, "the manager's own process", func() bool {
		b, err := os.ReadFile(filepath.Join(f.dir, "assistant.grandchild"))
		gc, _ = strconv.Atoi(string(b))
		return err == nil && gc > 0
	})
	f.stop(t, "assistant", 1)
	waitFor(t, "what it left behind gone", func() bool { return gone(gc) })
}

// gone: pid has ended. A zombie counts: whatever adopted it may never reap it (a container whose first process
// doesn't), and its state in /proc says so where there is one; this test process reaps it when it adopted it (as the
// first process of a pid namespace, or a subreaper).
func gone(pid int) bool {
	var ws syscall.WaitStatus
	_, _ = syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
	if syscall.Kill(pid, 0) != nil {
		return true
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return len(f) > 0 && f[0] == "Z"
}

// hub.json that can't be read once the hub stopped: the supervisor stops and says that, not that the mode changed.
func TestUnreadableHubFileStopsTheSupervisor(t *testing.T) {
	f := newFixture(t, `{"assistant":{"mode":"service"}}`)
	done := make(chan error, 1)
	go func() { done <- f.e.hubContainer(context.Background()) }()
	waitFor(t, "both", func() bool { return len(f.starts("hub")) == 1 && len(f.starts("assistant")) == 1 })
	if err := os.WriteFile(filepath.Join(f.e.home, "hub.json"), []byte(`{"assistant":`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.stop(t, "hub", 1)
	select {
	case err := <-done:
		log := f.out.String()
		if !errors.Is(err, errUnreadable) || !errors.Is(err, ErrStopped) || errors.Is(err, errChanged) ||
			!strings.Contains(log, "hub.json can't be read after the hub stopped") || strings.Contains(log, "changed") {
			t.Fatalf("err %v\n%s", err, log)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor did not stop")
	}
}
