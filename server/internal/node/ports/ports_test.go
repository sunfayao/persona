package ports

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/processtable"
	"github.com/sunfayao/persona/server/internal/proto"
)

// lsof -a -p … -nP -iTCP -sTCP:LISTEN -Fpcn on macOS: vite on IPv6 localhost, a Python server on every address,
// next on both stacks (two sockets, one port).
const lsofOut = `p4242
cnode
f23
n[::1]:5173
p4300
cPython
f3
n*:8000
p4401
cnext-server (v15.2.0)
f20
n*:3000
f21
n[::]:3000
`

func TestParseLsof(t *testing.T) {
	t.Parallel()
	got := parseLsof(lsofOut)
	want := []listener{
		{pid: 4242, port: 5173, addr: "::1", cmd: "node"},
		{pid: 4300, port: 8000, addr: "*", cmd: "Python"},
		{pid: 4401, port: 3000, addr: "*", cmd: "next-server (v15.2.0)"},
		{pid: 4401, port: 3000, addr: "::", cmd: "next-server (v15.2.0)"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parseLsof:\n got %+v\nwant %+v", got, want)
	}
}

// /proc/net/tcp and tcp6: only LISTEN (0A) sockets, addresses in little-endian words.
func TestParseProcNet(t *testing.T) {
	t.Parallel()
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0000000000000000 100 0 0 10 0
   1: 00000000:1F40 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 222 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1435 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1000        0 333 1 0000000000000000 20 4 30 10 -1
`
	tcp6 := `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:0BB8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 444 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000000000000:0BB8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 555 1 0000000000000000 100 0 0 10 0
   2: 0000000000000000FFFF00000100007F:22B8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 666 1 0000000000000000 100 0 0 10 0
`
	got := parseProcNet(tcp)
	if len(got) != 2 || got[111] != (listener{port: 5173, addr: "127.0.0.1"}) || got[222] != (listener{port: 8000, addr: "0.0.0.0"}) {
		t.Fatalf("tcp = %+v", got)
	}
	got6 := parseProcNet(tcp6)
	if got6[444] != (listener{port: 3000, addr: "::1"}) || got6[555] != (listener{port: 3000, addr: "::"}) ||
		got6[666] != (listener{port: 8888, addr: "127.0.0.1"}) {
		t.Fatalf("tcp6 = %+v", got6)
	}
}

// A fake /proc: the pids' descriptors point at socket inodes; only listening ones count.
func TestProcfsListeners(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("net/tcp", "header\n   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0 100 0 0 10 0\n")
	write("net/tcp6", "header\n")
	_ = os.MkdirAll(filepath.Join(root, "42", "fd"), 0o755)
	_ = os.Symlink("socket:[111]", filepath.Join(root, "42", "fd", "20"))
	_ = os.Symlink("socket:[999]", filepath.Join(root, "42", "fd", "21")) // a connected socket
	_ = os.Symlink("/dev/null", filepath.Join(root, "42", "fd", "0"))
	got := procfsListeners(root, []int{42, 43})
	if len(got) != 1 || got[0] != (listener{pid: 42, port: 5173, addr: "127.0.0.1"}) {
		t.Fatalf("procfsListeners = %+v", got)
	}
}

// A live scan on this machine: a server this test process runs is found as the pane's, with its title; pushed
// again only when that changes; gone once it stops.
func TestScanPortsFindsTheAgentsServer(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("ports are scanned on Linux and macOS")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<title>Scanned</title>")
	}))
	port := portOf(t, srv.URL)
	// The pane's agent (its first process) is not there; the test process is the pane's other process, as the
	// shell a server runs under would be.
	h := &countingProcHost{pids: []int{1 << 30, os.Getpid()}}
	panes := map[string]bool{"w1:p1": true}
	logger := log.New(os.Stderr, "", 0)
	n := New(Config{Panes: func() map[string]bool { return panes }, PaneProcesses: h.PaneProcesses,
		Logf: logger.Printf})
	ctx := context.Background()
	if !n.Scan(ctx) {
		t.Fatal("the first scan with a server should be a change")
	}
	ps := n.Of("w1:p1")
	i := slices.IndexFunc(ps, func(p proto.Port) bool { return p.Port == port })
	if i < 0 {
		t.Fatalf("port %d not found in %+v", port, ps)
	}
	if ps[i].Title != "Scanned" || ps[i].PID != os.Getpid() {
		t.Fatalf("port = %+v", ps[i])
	}
	if n.Scan(ctx) {
		t.Fatal("nothing changed: no push")
	}
	srv.Close()
	n.Scan(ctx)
	if slices.ContainsFunc(n.Of("w1:p1"), func(p proto.Port) bool { return p.Port == port }) {
		t.Fatal("a stopped server is still listed")
	}
}

// lsof exits 1 when a pid it was given is gone or a zombie (an agent's short command that exited between ps and
// lsof); the others' listeners are still found (#541).
func TestLsofListenersWithAGonePid(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("lsof lists listeners on macOS")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	reaped := exec.Command("true")
	if err := reaped.Run(); err != nil {
		t.Fatal(err)
	}
	zombie := exec.Command("true")
	if err := zombie.Start(); err != nil {
		t.Fatal(err)
	}
	defer zombie.Wait()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(zombie.Process.Pid)).Output()
		if strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
			break
		}
		select {
		case <-timer.C:
			t.Fatalf("%d is not a zombie: %q", zombie.Process.Pid, out)
		case <-ticker.C:
		}
	}
	ls, err := lsofListeners(context.Background(), []int{os.Getpid(), zombie.Process.Pid, reaped.Process.Pid})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(ls, func(l listener) bool { return l.pid == os.Getpid() && l.port == port }) {
		t.Fatalf("port %d not found in %+v", port, ls)
	}
}

// A scan that keeps failing logs once, and once more when scans work again (#541: a line every 4 seconds).
func TestScanPortsLogsAFailureOnce(t *testing.T) {
	t.Parallel()
	var logs strings.Builder
	logger := log.New(&logs, "", 0)
	failing := true
	n := New(Config{
		Panes:         func() map[string]bool { return map[string]bool{"w1:p1": true} },
		PaneProcesses: func(context.Context, string) ([]int, error) { return nil, nil },
		Processes: func(context.Context) (map[int]processtable.Process, error) {
			if failing {
				return nil, errors.New("process table failed")
			}
			return nil, nil
		},
		Logf: logger.Printf,
	})
	ctx := context.Background()
	for range 3 {
		n.Scan(ctx)
	}
	if got := strings.Count(logs.String(), "\n"); got != 1 {
		t.Fatalf("3 failed scans logged %d lines:\n%s", got, logs.String())
	}
	failing = false
	n.Scan(ctx)
	n.Scan(ctx)
	if lines := strings.Split(strings.TrimSpace(logs.String()), "\n"); len(lines) != 2 || lines[1] != "ports: scanning again" {
		t.Fatalf("after recovering:\n%s", logs.String())
	}
}

func portOf(t *testing.T, u string) int {
	t.Helper()
	i := strings.LastIndexByte(u, ':')
	p, err := strconv.Atoi(u[i+1:])
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Two panes, a server each: one scan asks each server once, and the next asks neither (results are
// kept by pid:port until the process or the port goes). A pane's own pass must not drop another pane's results.
func TestProbePanesKeepsEveryPanesProbes(t *testing.T) {
	t.Parallel()
	var hits [2]atomic.Int32
	var ports [2]int
	for i := range hits {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits[i].Add(1)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<title>pane %d</title>", i)
		}))
		t.Cleanup(srv.Close)
		ports[i] = portOf(t, srv.URL)
	}
	n := New(Config{})
	found := map[string][]listener{
		"w1:p1": {{pid: 101, port: ports[0], addr: "127.0.0.1"}},
		"w1:p2": {{pid: 102, port: ports[1], addr: "127.0.0.1"}},
	}
	ctx := context.Background()
	for range 2 {
		got := n.probePanes(ctx, found)
		if len(got["w1:p1"]) != 1 || len(got["w1:p2"]) != 1 {
			t.Fatalf("ports = %+v", got)
		}
	}
	if a, b := hits[0].Load(), hits[1].Load(); a != 1 || b != 1 {
		t.Fatalf("two scans asked the servers %d and %d times, want once each", a, b)
	}

	// The second pane's server gone: its result goes, the first pane's stays.
	delete(found, "w1:p2")
	n.probePanes(ctx, found)
	n.mu.Lock()
	_, kept := n.probes["101:"+strconv.Itoa(ports[0])]
	_, stale := n.probes["102:"+strconv.Itoa(ports[1])]
	n.mu.Unlock()
	if !kept || stale {
		t.Fatalf("after the second pane went: first kept %v, second kept %v", kept, stale)
	}
	// No listener found anywhere: nothing is kept.
	n.probePanes(ctx, nil)
	n.mu.Lock()
	left := len(n.probes)
	n.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d results kept with no listener left", left)
	}
	if hits[0].Load() != 1 {
		t.Fatalf("the first server was asked %d times", hits[0].Load())
	}
}

// countingProcHost reports pids as a pane's foreground processes and counts the asks.
type countingProcHost struct {
	mu    sync.Mutex
	pids  []int
	asked int
}

func (h *countingProcHost) PaneProcesses(context.Context, string) ([]int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked++
	return h.pids, nil
}

func (h *countingProcHost) set(pids ...int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pids = pids
}

func (h *countingProcHost) asks() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asked
}

// startProc runs name args until the test ends (or it is stopped); its pid.
func startProc(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

// A pane's other foreground processes are reused only while each is unchanged: one that exited, or whose pid now
// belongs to another process, is dropped rather than walked as a root (its children and its terminal could be another
// pane's), while the first process keeps the pane's roots without asking the host again.
func TestScanPortsDropsAChangedCachedProcess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("ports are scanned on Linux and macOS")
	}
	lead := startProc(t, "sleep", "300")
	other := startProc(t, "sleep", "300")
	h := &countingProcHost{}
	h.set(lead.Process.Pid, other.Process.Pid, os.Getpid())
	panes := map[string]bool{"w1:p1": true}
	logger := log.New(os.Stderr, "", 0)
	n := New(Config{Panes: func() map[string]bool { return panes }, PaneProcesses: h.PaneProcesses,
		Logf: logger.Printf})
	ctx := context.Background()
	n.Scan(ctx)
	if got := n.roots["w1:p1"].pids; len(got) != 3 {
		t.Fatalf("kept %v, want all 3 pids", got)
	}

	// The second exits: dropped.
	_ = other.Process.Kill()
	_ = other.Wait()
	n.Scan(ctx)
	if got, want := n.roots["w1:p1"].pids, []int{lead.Process.Pid, os.Getpid()}; !slices.Equal(got, want) {
		t.Fatalf("after the second exited: kept %v, want %v", got, want)
	}

	// The test process's pid stood for another process when it was cached (a reused pid): dropped.
	r := n.roots["w1:p1"]
	r.seen[1].Command = "not-" + r.seen[1].Command
	n.Scan(ctx)
	if got, want := n.roots["w1:p1"].pids, []int{lead.Process.Pid}; !slices.Equal(got, want) {
		t.Fatalf("after a reused pid: kept %v, want %v", got, want)
	}
	if got := h.asks(); got != 1 {
		t.Fatalf("the host was asked %d times, want 1 (the first process never changed)", got)
	}
}
