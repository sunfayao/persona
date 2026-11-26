package node

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/node/preview"
)

func writeLaunch(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, ".claude", "launch.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// preview.servers: the entries of the session's launch.json, each probed; whether there is a browser.
func TestPreviewServersProbes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<title>My app</title>"))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	free, _ := net.Listen("tcp", "127.0.0.1:0")
	freePort := free.Addr().(*net.TCPAddr).Port
	free.Close()
	n, _ := testNode(t)
	root := t.TempDir()
	writeLaunch(t, root, `{"configurations": [
  {"name": "staging", "url": "https://staging.example.com/"},
  {"name": "web", "runtimeExecutable": "npm", "runtimeArgs": ["run", "dev"], "port": `+port+`},
  {"name": "api", "runtimeExecutable": "go", "runtimeArgs": ["run", "."], "port": `+strconv.Itoa(freePort)+`}
]}`)
	t.Setenv("PERSONA_CHROME", "")
	res, err := n.previewServersRPC(context.Background(), json.RawMessage(`{"cwd":`+strconv.Quote(root)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	got := res.(previewServers)
	if got.File != filepath.Join(root, preview.LaunchFile) || len(got.Servers) != 3 {
		t.Fatalf("servers: %+v", got)
	}
	web, api, staging := got.Servers[0], got.Servers[1], got.Servers[2]
	if web.Name != "web" || !web.Listening || web.Title != "My app" {
		t.Errorf("web: %+v", web)
	}
	if api.Name != "api" || api.Listening {
		t.Errorf("api: %+v", api)
	}
	if staging.Problem != "not_local" {
		t.Errorf("the usable entries come first, then the others: %+v", got.Servers)
	}

	none, err := n.previewServersRPC(context.Background(), json.RawMessage(`{"cwd":`+strconv.Quote(t.TempDir())+`}`))
	if err != nil || none.(previewServers).File != "" || none.(previewServers).Servers == nil {
		t.Errorf("a folder without launch.json: %+v %v", none, err)
	}
	if _, err := n.previewServersRPC(context.Background(), json.RawMessage(`{"cwd":"relative"}`)); err == nil {
		t.Error("a relative folder was read")
	}
}

// Start: a new tab in the session's workspace, named after the entry, in its folder, running its command quoted.
func TestPreviewStartRunsTheEntryInATab(t *testing.T) {
	streamingHerdr()
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	writeLaunch(t, cwd, `{"configurations": [
  {"name": "web", "runtimeExecutable": "npm", "runtimeArgs": ["run", "dev"], "port": 5173, "env": {"BROWSER": "none"}},
  {"name": "docs", "url": "http://localhost:4000/"}
]}`)
	f.reply("session.snapshot", paneSnapshot(cwd))
	if _, err := n.listPanes(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.reset()
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t3"}, "root_pane": map[string]any{"pane_id": "w1:p3"}})
	f.reply("session.snapshot", snapshot(nil, nil, nil, []any{map[string]any{"pane_id": "w1:p3", "tab_id": "w1:t3", "workspace_id": "w1"}}))
	start := time.Now()
	res, err := request(t, n, "preview.start", map[string]any{"cwd": cwd, "name": "web", "workspace": "w1"})
	if err != nil {
		t.Fatal(err)
	}
	if res["pane"] != "w1:p3" || res["terminal"] != "pane:w1:p3" || res["port"] != float64(5173) {
		t.Errorf("result = %v", res)
	}
	assertTrace(t, f.trace("tab.create"), []string{
		call("tab.create", map[string]any{"workspace_id": "w1", "cwd": cwd, "label": "web", "focus": false}),
	})
	assertTrace(t, f.trace("pane.send_input"), []string{
		call("pane.send_input", map[string]any{"pane_id": "w1:p3", "text": "env 'BROWSER=none' npm run dev", "keys": []string{"Enter"}}),
	})
	if time.Since(start) > 5*time.Second {
		t.Errorf("start took %s", time.Since(start))
	}

	f.reset()
	for name, p := range map[string]map[string]any{
		"an entry that only opens a page": {"cwd": cwd, "name": "docs", "workspace": "w1"},
		"no such entry":                   {"cwd": cwd, "name": "api", "workspace": "w1"},
		"another workspace":               {"cwd": cwd, "name": "web", "workspace": "w404"},
		"no workspace":                    {"cwd": cwd, "name": "web"},
		"a folder without launch.json":    {"cwd": home, "name": "web", "workspace": "w1"},
	} {
		if _, err := request(t, n, "preview.start", p); err == nil {
			t.Errorf("%s: started", name)
		}
	}
	if got := f.trace("tab.create"); len(got) != 0 {
		t.Errorf("tab.create for a refused start: %v", got)
	}
}

// paseoWorkspaceOn gives n a connected runtime that keeps workspaces (Paseo's), listing workspace id in dir: enough for
// n to tell the id from a terminal host's (no socket: nothing here calls it).
func paseoWorkspaceOn(n *Node, id, dir string) {
	if n.runtimes == nil {
		n.runtimes = newRuntimeRegistry()
	}
	h := n.runtimes.add(nil)
	h.mu.Lock()
	h.ready, h.hello.Runtime, h.hello.Caps.Workspaces = true, paseoRuntimeID, true
	h.workspaces = []runtimeWorkspace{{ID: id, Cwd: dir, Kind: workspaceCheckout}}
	h.mu.Unlock()
}

// herdrSpaces is herdr with workspace w1 in other and w2 in proj, as the layout (session.snapshot) and as the list the
// node reads the workspaces' folders from (workspace.list).
func herdrSpaces(f *fakeHerdr, other, proj string) map[string]any {
	spaces := snapshot([]any{map[string]any{"workspace_id": "w1", "label": "other", "number": 1, "cwd": other},
		map[string]any{"workspace_id": "w2", "label": "proj", "number": 2, "cwd": proj}}, nil, nil, nil)
	f.reply("session.snapshot", spaces)
	f.reply("workspace.list", map[string]any{"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "other", "cwd": other},
		map[string]any{"workspace_id": "w2", "label": "proj", "cwd": proj}}})
	return spaces
}

// Start for a runtime's session (a Paseo session names its runtime's workspace, which has no terminal): the server runs
// in the terminal engine's workspace that holds its folder, not in the first one; when only the home folder's holds
// it, in a new workspace in its folder.
func TestPreviewStartForARuntimeSessionRunsWhereItsFolderIs(t *testing.T) {
	streamingHerdr()
	n, f, home := herdrNode(t)
	other := mkdir(t, filepath.Join(home, "other"))
	cwd := mkdir(t, filepath.Join(home, "proj"))
	writeLaunch(t, cwd, `{"configurations": [{"name": "web", "runtimeExecutable": "npm", "runtimeArgs": ["run", "dev"], "port": 5173}]}`)
	paseoWorkspaceOn(n, "wks_1", cwd)
	herdrSpaces(f, other, cwd)
	if _, err := n.listPanes(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.reset()
	herdrSpaces(f, other, cwd)
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w2:t3"}, "root_pane": map[string]any{"pane_id": "w2:p3"}})
	res, err := request(t, n, "preview.start", map[string]any{"cwd": cwd, "name": "web", "workspace": "wks_1", "kind": "paseo"})
	if err != nil || res["pane"] != "w2:p3" {
		t.Fatalf("start: %v %v", res, err)
	}
	assertTrace(t, f.trace("workspace.create", "tab.create"), []string{
		call("tab.create", map[string]any{"workspace_id": "w2", "cwd": cwd, "label": "web", "focus": false}),
	})

	// Only the home folder's workspace holds it, which holds everything: a new workspace in the server's folder.
	f.reset()
	f.reply("workspace.list", map[string]any{"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "home", "cwd": home}}})
	f.reply("workspace.create", map[string]any{"workspace": map[string]any{"workspace_id": "w3"},
		"tab": map[string]any{"tab_id": "w3:t1"}, "root_pane": map[string]any{"pane_id": "w3:p1"}})
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	res, err = request(t, n, "preview.start", map[string]any{"cwd": cwd, "name": "web", "workspace": "wks_1", "kind": "paseo"})
	if err != nil || res["pane"] != "w3:p1" {
		t.Fatalf("start in a new workspace: %v %v", res, err)
	}
	assertTrace(t, f.trace("workspace.create", "tab.create", "tab.rename"), []string{
		call("workspace.create", map[string]any{"cwd": cwd, "label": "proj", "focus": false}),
		call("tab.rename", map[string]any{"tab_id": "w3:t1", "label": "web"}),
	})
	assertTrace(t, f.trace("pane.send_input"), []string{
		call("pane.send_input", map[string]any{"pane_id": "w3:p1", "text": "npm run dev", "keys": []string{"Enter"}}),
	})
}

// A computer whose engines open no tabs has nowhere to run a runtime session's server: a coded refusal the hub words
// in the reader's language, and nothing started.
func TestPreviewStartWithNoTerminalEngine(t *testing.T) {
	n, _ := runtimeNode(t)
	n.hosts = host.NewRegistry()
	cwd := mkdir(t, filepath.Join(t.TempDir(), "proj"))
	writeLaunch(t, cwd, `{"configurations": [{"name": "web", "runtimeExecutable": "npm", "runtimeArgs": ["run", "dev"], "port": 5173}]}`)
	paseoWorkspaceOn(n, "wks_1", cwd)
	_, err := request(t, n, "preview.start", map[string]any{"cwd": cwd, "name": "web", "workspace": "wks_1", "kind": "paseo"})
	if rpcCode(err) != codePreviewNoTerminal {
		t.Fatalf("no terminal engine: %v (code %q)", err, rpcCode(err))
	}
}
