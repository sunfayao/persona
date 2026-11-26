package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A launcher that runs claude under a wrapper makes the wrapper the pane's first foreground process, and herdr 0.9.5
// names Claude Code's process by its version and reports argv, not argv0. Claude Code's own listener (it answers HTTP)
// is still no server the Agent runs, so it cannot keep an idle Agent from Close (#14681). The same listener of
// another of the pane's processes is listed.
func TestScanPortsLeavesOutClaudeBehindAWrapper(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("ports are scanned on Linux and macOS")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // as Claude Code's own listener answers
	}))
	defer srv.Close()
	port := portOf(t, srv.URL)
	for _, tc := range []struct {
		name   string
		front  []any
		listed bool
	}{
		// this test process is the pane's Claude Code, behind a wrapper that lives in a folder named after claude
		{"claude behind a wrapper", []any{
			map[string]any{"pid": 1 << 30, "name": "wrapper", "argv": []any{"/opt/claude-tools/bin/wrapper", "run", "--"}},
			map[string]any{"pid": os.Getpid(), "name": "2.1.330", "argv": []any{"/home/me/.local/bin/claude"}},
		}, false},
		// this test process is another of the pane's processes, the Agent's Claude Code a process that is gone
		{"another process", []any{
			map[string]any{"pid": 1 << 30, "name": "2.1.330", "argv": []any{"/home/me/.local/bin/claude"}},
			map[string]any{"pid": os.Getpid(), "name": "node", "argv": []any{"node", "server.js"}},
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, _ := herdrNode(t)
			f.reply("pane.process_info", map[string]any{"process_info": map[string]any{"pane_id": "w1:p1", "foreground_processes": tc.front}})
			n.mu.Lock()
			n.agents = map[string]host.Agent{"s1": {Agent: "claude", PaneID: "w1:p1"}}
			n.mu.Unlock()
			n.portService().Scan(context.Background())
			ps := n.portsOf("w1:p1")
			if listed := slices.ContainsFunc(ps, func(p proto.Port) bool { return p.Port == port }); listed != tc.listed {
				t.Fatalf("port %d listed: %v, want %v (%+v)", port, listed, tc.listed, ps)
			}
		})
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
