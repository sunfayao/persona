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

	"github.com/sunfayao/persona/server/internal/node/preview"
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
