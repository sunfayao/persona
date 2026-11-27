package extmgr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
	pserve "github.com/sunfayao/persona/server/internal/extension/serve"
)

// serveBoxedExtensionForTest answers GET /probe on its HTTP side with what it may do from where it runs.
func serveBoxedExtensionForTest() {
	ln, err := net.Listen("unix", os.Getenv("PERSONA_EXTENSION_SOCKET"))
	if err != nil {
		os.Exit(1)
	}
	var mu sync.Mutex
	var init extension.HubInfo
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hub := init
		mu.Unlock()
		out := map[string]any{"home": os.Getenv("HOME"), "proxy": os.Getenv("HTTPS_PROXY"), "hub_url": os.Getenv("PERSONA_HUB_URL"),
			"init_url": hub.URL}
		get := func(u, token string) string {
			req, _ := http.NewRequest("GET", u, nil)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			c := &http.Client{Timeout: 10 * time.Second}
			resp, err := c.Do(req)
			if err != nil {
				return "error"
			}
			resp.Body.Close()
			return fmt.Sprint(resp.StatusCode)
		}
		out["hub"] = get(hub.URL+"/api/v1/sessions?q=x", hub.Token)
		out["denied"] = get("http://denied.example.org/", "")
		c, err := net.DialTimeout("tcp", "192.0.2.1:80", 2*time.Second)
		if c != nil {
			c.Close()
		}
		out["dial"] = err == nil
		// An agent runtime's way to the node (permissions.agent_runtimes): its socket, and the node's own path.
		if sock := os.Getenv("PERSONA_RUNTIME_SOCKET"); sock != "" {
			out["runtime"] = askLine(sock)
		}
		if p := r.URL.Query().Get("node"); p != "" {
			out["node_direct"] = askLine(p)
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "boxed", Version: "1.0.0",
		Init: func(_ context.Context, ip extension.InitializeParams, _ *pserve.Hub) (map[string]any, error) {
			mu.Lock()
			init = ip.Hub
			mu.Unlock()
			return nil, nil
		}})
}

// askLine sends a line on the unix socket at path and is the line answered, or "error".
func askLine(path string) string {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return "error"
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping\n")); err != nil {
		return "error"
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return "error"
	}
	return strings.TrimSpace(line)
}

func TestExtensionSandboxConcurrentChoices(t *testing.T) {
	h, store := extensionTestHub(t)
	ps := h.extensions
	ctx := context.Background()
	on := true
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 20 {
				id := fmt.Sprintf("x%d-%d", i, j)
				p := &extension.Extension{Record: extension.Record{ID: id, Source: extension.SourceInstalled, Origin: "dir:/x"},
					Manifest: &extension.Manifest{ID: id, Runtime: &extension.Runtime{}}}
				// Not in the registry: setSandbox's restart forgets the choice again (forgetSandbox).
				if err := ps.setSandbox(ctx, p, &on, "test"); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	var saved sandboxSettings
	if _, err := store.Setting(ctx, extensionSandboxKey, &saved); err != nil {
		t.Fatal(err)
	}
	ps.sandboxes.mu.Lock()
	want, _ := json.Marshal(ps.sandboxes.settings)
	ps.sandboxes.mu.Unlock()
	if got, _ := json.Marshal(saved); string(got) != string(want) {
		t.Fatalf("saved %s\nwant %s", got, want)
	}
}
