package shellexec

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The executor with a fake clock and a fake process: no child process, no real waiting.

const home = "/home/u"

// fakeClock moves only when a test advances it; waitTimers blocks until the executor waits on that many timers.
type fakeClock struct {
	mu     sync.Mutex
	cond   *sync.Cond
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	c := &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, fakeTimer{c.now.Add(d), ch})
	c.cond.Broadcast()
	return ch
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			kept = append(kept, t)
			continue
		}
		t.ch <- c.now
	}
	c.timers = kept
}

func (c *fakeClock) waitTimers(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.timers) < n {
		c.cond.Wait()
	}
}

// fakeProc is a shell and what else is in its group. Wait returns once the shell exits.
type fakeProc struct {
	mu         sync.Mutex
	shell      bool // the shell runs
	others     bool // something else runs in its group
	endsOnTerm bool // SIGTERM ends the whole group
	code       int
	exited     chan struct{}
	signals    []string
}

func newFakeProc(running bool) *fakeProc {
	p := &fakeProc{shell: true, exited: make(chan struct{})}
	if !running {
		p.exitLocked(0)
	}
	return p
}

func (p *fakeProc) exitLocked(code int) {
	if p.shell {
		p.shell, p.code = false, code
		close(p.exited)
	}
}

func (p *fakeProc) Wait() (int, error) {
	<-p.exited
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, nil
}

func (p *fakeProc) Terminate() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signals = append(p.signals, "TERM")
	if p.endsOnTerm {
		p.others = false
		p.exitLocked(-1)
	}
	return nil
}

func (p *fakeProc) Kill(within time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signals = append(p.signals, "KILL "+within.String())
	p.others = false
	p.exitLocked(-1)
}

func (p *fakeProc) Alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shell || p.others
}

func (p *fakeProc) sent() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.signals)
}

// fakeStarter starts proc, after out has written the command's output.
type fakeStarter struct {
	mu     sync.Mutex
	proc   *fakeProc
	out    func(s Spec)
	starts []Spec
}

func (f *fakeStarter) Start(s Spec) (Process, error) {
	f.mu.Lock()
	f.starts = append(f.starts, s)
	f.mu.Unlock()
	if f.out != nil {
		f.out(s)
	}
	return f.proc, nil
}

func (f *fakeStarter) started() []Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.starts)
}

func newExecutor(st *fakeStarter, c *fakeClock, d Dependencies) *Executor {
	d.Home, d.Start, d.Clock = home, st, c
	if d.Env == nil {
		d.Env = []string{"PATH=/usr/bin:/bin", "SHELL=/bin/zsh", "PAGER=less", "LANG=en_US.UTF-8"}
	}
	if d.GOOS == "" {
		d.GOOS = "darwin"
	}
	return New(d)
}

func params(command string, timeout time.Duration) proto.MachineExecParams {
	return proto.MachineExecParams{Command: command, TimeoutMS: timeout.Milliseconds()}
}

// A command that exits on its own: its exit code and its output, in the user's login shell, non-interactive.
func TestARunReturnsTheExitCodeAndTheOutput(t *testing.T) {
	t.Parallel()
	for _, code := range []int{0, 3} {
		st := &fakeStarter{proc: newFakeProc(false), out: func(s Spec) {
			_, _ = s.Stdout.Write([]byte("out\n"))
			_, _ = s.Stderr.Write([]byte("err\n"))
		}}
		st.proc.code = code
		res, err := newExecutor(st, newFakeClock(), Dependencies{}).Run(t.Context(), params("echo hi", 20*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		want := proto.MachineExecResult{ExitCode: code, Stdout: "out\n", Stderr: "err\n"}
		if res != want {
			t.Fatalf("result %+v, want %+v", res, want)
		}
		if sent := st.proc.sent(); len(sent) != 0 {
			t.Fatalf("a command that ended by itself got %v", sent)
		}
		s := st.started()[0]
		if s.Path != "/bin/zsh" || !slices.Equal(s.Args, []string{"-lc", "echo hi"}) || s.Dir != home {
			t.Fatalf("started %s %v in %s", s.Path, s.Args, s.Dir)
		}
	}
}

// At the timeout the whole group gets SIGTERM, then after the grace SIGKILL until it is empty: timed_out, exit -1.
func TestATimeoutStopsTheWholeGroup(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	st := &fakeStarter{proc: newFakeProc(true)}
	st.proc.others = true // a child that ignores SIGTERM
	e := newExecutor(st, c, Dependencies{})
	done := make(chan proto.MachineExecResult, 1)
	go func() {
		res, err := e.Run(context.Background(), params("sleep 100 & sleep 100", 5*time.Second))
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	c.waitTimers(1)
	c.advance(5 * time.Second)
	c.waitTimers(1) // looking at the group during the grace
	if sent := st.proc.sent(); !slices.Equal(sent, []string{"TERM"}) {
		t.Fatalf("at the timeout the group got %v, want TERM only", sent)
	}
	c.advance(grace)
	res := <-done
	if !res.TimedOut || res.ExitCode != -1 || res.DurationMS != (5*time.Second+grace).Milliseconds() {
		t.Fatalf("result %+v", res)
	}
	if sent := st.proc.sent(); !slices.Equal(sent, []string{"TERM", "KILL " + sweep.String()}) || st.proc.Alive() {
		t.Fatalf("the group got %v (alive %v), want TERM then KILL until empty", sent, st.proc.Alive())
	}
}

// A group that ends on SIGTERM is not waited for until the grace ends; SIGKILL still sweeps it.
func TestAGroupThatEndsOnTermIsNotWaitedFor(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	st := &fakeStarter{proc: newFakeProc(true)}
	st.proc.endsOnTerm = true
	e := newExecutor(st, c, Dependencies{})
	done := make(chan proto.MachineExecResult, 1)
	go func() {
		res, _ := e.Run(context.Background(), params("sleep 100", time.Second))
		done <- res
	}()
	c.waitTimers(1)
	c.advance(time.Second)
	res := <-done
	if !res.TimedOut || res.ExitCode != -1 || res.DurationMS != 1000 {
		t.Fatalf("result %+v", res)
	}
	if sent := st.proc.sent(); !slices.Equal(sent, []string{"TERM", "KILL " + sweep.String()}) {
		t.Fatalf("the group got %v", sent)
	}
}

// What the shell left running in its group when it exited is stopped too; the command's own exit code stands.
func TestWhatTheShellLeftRunningIsStopped(t *testing.T) {
	t.Parallel()
	st := &fakeStarter{proc: newFakeProc(false)}
	st.proc.others, st.proc.endsOnTerm = true, true
	res, err := newExecutor(st, newFakeClock(), Dependencies{}).Run(t.Context(), params("sleep 100 &", 20*time.Second))
	if err != nil || res.ExitCode != 0 || res.TimedOut {
		t.Fatalf("result %+v, %v", res, err)
	}
	if sent := st.proc.sent(); !slices.Equal(sent, []string{"TERM", "KILL " + sweep.String()}) || st.proc.Alive() {
		t.Fatalf("the group got %v", sent)
	}
}

// The caller going away (the connection ended) stops the command; nothing is answered but that.
func TestTheCallerGoingAwayStopsTheCommand(t *testing.T) {
	t.Parallel()
	st := &fakeStarter{proc: newFakeProc(true)}
	st.proc.endsOnTerm = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newExecutor(st, newFakeClock(), Dependencies{}).Run(ctx, params("sleep 100", 20*time.Second))
	if err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("err %v", err)
	}
	if sent := st.proc.sent(); len(sent) == 0 || sent[0] != "TERM" || st.proc.Alive() {
		t.Fatalf("the group got %v", sent)
	}
}

// Each stream keeps its first 8 KiB and its last 24 KiB around a marker, cut on characters, and says what it left out.
func TestLongOutputKeepsItsHeadAndItsEnd(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("é", 50_000) // 100,000 bytes of two-byte characters
	st := &fakeStarter{proc: newFakeProc(false), out: func(s Spec) {
		for i := 0; i < len(body); i += 4097 { // writes of an odd size: characters cut between them
			_, _ = s.Stdout.Write([]byte(body[i:min(i+4097, len(body))]))
		}
		_, _ = s.Stderr.Write([]byte("short"))
	}}
	res, err := newExecutor(st, newFakeClock(), Dependencies{}).Run(t.Context(), params("yes", 20*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	head, tail, ok := strings.Cut(res.Stdout, "\n[… ")
	_, tail, _ = strings.Cut(tail, " bytes omitted …]\n")
	if !ok || head != body[:streamHead] || tail != body[len(body)-streamTail:] {
		t.Fatalf("head %d bytes, tail %d bytes", len(head), len(tail))
	}
	if res.StdoutOmitted != int64(len(body)-StreamCap) || !strings.Contains(res.Stdout, "[… 67232 bytes omitted …]") {
		t.Fatalf("omitted %d: %q", res.StdoutOmitted, res.Stdout[streamHead-4:streamHead+40])
	}
	if res.Stderr != "short" || res.StderrOmitted != 0 {
		t.Fatalf("stderr %q, %d", res.Stderr, res.StderrOmitted)
	}

	c := capture{} // a character cut at both ends of what is kept is left out whole
	_, _ = c.Write([]byte(strings.Repeat("a", streamHead-1) + "é" + strings.Repeat("b", 100) + "é" + strings.Repeat("c", streamTail-1)))
	out, omitted := c.result()
	if omitted != 104 || !strings.HasPrefix(out, strings.Repeat("a", streamHead-1)+"\n[… 104 bytes omitted …]\n"+strings.Repeat("c", streamTail-1)) {
		t.Fatalf("omitted %d: %q", omitted, out[streamHead-4:streamHead+40])
	}
}

// Windows is refused before anything starts, as secret.run refuses it.
func TestWindowsIsRefused(t *testing.T) {
	t.Parallel()
	st := &fakeStarter{proc: newFakeProc(false)}
	_, err := newExecutor(st, newFakeClock(), Dependencies{GOOS: "windows"}).Run(t.Context(), params("dir", time.Second))
	if err == nil || !strings.Contains(err.Error(), "Windows") || len(st.started()) != 0 {
		t.Fatalf("err %v, started %d", err, len(st.started()))
	}
}

// A command or directory naming a refused path does not run; the paths are read from the text as a shell would expand
// its words: from cwd, with ~, $HOME and $PERSONA_HOME, and through quotes, escapes and braces.
func TestACommandNamingACredentialFileIsRefused(t *testing.T) {
	t.Parallel()
	refused := func(p string) bool {
		return filepath.Base(p) == "hub.json" || strings.HasPrefix(p, "/home/u/.persona/tls") || p == "/data/link.json"
	}
	for _, tc := range []struct {
		command, cwd string
		refused      bool
	}{
		{"cat ~/.persona/hub.json", "", true},
		{`cat "$HOME/.persona/hub.json"`, "", true},
		{"cat ${HOME}/.persona/h'u'b.j\\son", "", true},
		{"jq .token hub.json", "~/.persona", true},
		{"ls tls", "~/.persona", true},
		{"ls", "~/.persona/tls", true},
		{"cp $PERSONA_HOME/link.json /tmp/x", "", true},
		{"cat /etc/{x,hub.json}", "", true},
		{"docker run -v=/srv:/home/u/.persona/hub.json x", "", true},
		{"ls ~/.persona/logs && echo hub", "", false},
		{"echo $HOMEBREW_PREFIX", "/tmp", false},
	} {
		st := &fakeStarter{proc: newFakeProc(false)}
		p := params(tc.command, time.Second)
		p.Cwd = tc.cwd
		_, err := newExecutor(st, newFakeClock(), Dependencies{DataDir: "/data", Refused: refused}).Run(t.Context(), p)
		if got := err != nil && strings.Contains(err.Error(), "refused, nothing ran"); got != tc.refused || got == (len(st.started()) == 1) {
			t.Errorf("%q in %q: err %v, started %d", tc.command, tc.cwd, err, len(st.started()))
		}
	}
}

// The params are checked on the node too.
func TestTheNodeChecksTheParams(t *testing.T) {
	t.Parallel()
	for _, p := range []proto.MachineExecParams{
		{Command: " ", TimeoutMS: 1000},
		{Command: strings.Repeat("x", proto.MachineExecMaxCommand+1), TimeoutMS: 1000},
		{Command: "echo \x00", TimeoutMS: 1000},
		{Command: "echo \xff", TimeoutMS: 1000},
		{Command: "echo", TimeoutMS: 0},
		{Command: "echo", TimeoutMS: proto.MachineExecMaxTimeout.Milliseconds() + 1},
		{Command: "echo", TimeoutMS: 1000, Cwd: strings.Repeat("d", proto.MachineExecMaxCwd+1)},
	} {
		st := &fakeStarter{proc: newFakeProc(false)}
		if _, err := newExecutor(st, newFakeClock(), Dependencies{}).Run(t.Context(), p); err == nil || len(st.started()) != 0 {
			t.Errorf("%.40q: err %v, started %d", p.Command, err, len(st.started()))
		}
	}
	st := &fakeStarter{proc: newFakeProc(false)}
	e := New(Dependencies{Start: st, Clock: newFakeClock(), GOOS: "linux", Env: []string{}})
	if _, err := e.Run(t.Context(), params("ls", time.Second)); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Fatalf("without a home: %v", err)
	}
}

// The login shell: on macOS $SHELL when it is zsh or bash, else zsh; elsewhere bash. The environment is the node's,
// non-interactive, with the user's bin folders at the end of PATH; cwd is under the home.
func TestTheShellAndItsEnvironment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		goos, shell, want string
	}{
		{"darwin", "/opt/homebrew/bin/bash", "/opt/homebrew/bin/bash"},
		{"darwin", "/usr/local/bin/fish", "/bin/zsh"},
		{"darwin", "", "/bin/zsh"},
		{"linux", "/bin/zsh", "bash"},
	} {
		st := &fakeStarter{proc: newFakeProc(false)}
		env := []string{"PATH=/usr/bin:/bin:/opt/homebrew/bin", "PAGER=less", "LANG=C"}
		if tc.shell != "" {
			env = append(env, "SHELL="+tc.shell)
		}
		p := params("git log", time.Second)
		p.Cwd = "~/src"
		if _, err := newExecutor(st, newFakeClock(), Dependencies{GOOS: tc.goos, Env: env}).Run(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		s := st.started()[0]
		if s.Path != tc.want || s.Dir != "/home/u/src" {
			t.Errorf("%s with SHELL %q: %s in %s", tc.goos, tc.shell, s.Path, s.Dir)
		}
		path := "PATH=/usr/bin:/bin:/opt/homebrew/bin:/home/u/.local/bin"
		if tc.goos == "darwin" {
			path = "PATH=/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin:/home/u/.local/bin"
		}
		for _, kv := range []string{"LANG=C", "PAGER=cat", "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "TERM=dumb", "NO_COLOR=1", path} {
			if !slices.Contains(s.Env, kv) {
				t.Errorf("%s: env %v lacks %s", tc.goos, s.Env, kv)
			}
		}
		if slices.Contains(s.Env, "PAGER=less") {
			t.Errorf("env keeps the node's pager: %v", s.Env)
		}
	}
}
