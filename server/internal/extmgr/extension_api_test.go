package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/connector/proc"
	"github.com/sunfayao/persona/server/internal/extension"
	pserve "github.com/sunfayao/persona/server/internal/extension/serve"
	"github.com/sunfayao/persona/server/internal/extwire"
)

// serveAPIExtensionForTest serves part of the app's API (/api/v1/examplefeed/…, and /api/v1/lark/… taken over from the
// hub) on its socket, and pushes two events once initialized: one of a type it declared, one it did not.
func serveAPIExtensionForTest() {
	ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
	if err != nil {
		os.Exit(1)
	}
	var sends, avatarReads atomic.Int32
	var role, caps atomic.Value
	role.Store("")
	caps.Store([]string(nil))
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveTestLarkAvatars(w, r, &avatarReads) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/role") {
			_ = json.NewEncoder(w).Encode(map[string]any{"role": role.Load(), "capabilities": caps.Load()})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/send") {
			_ = json.NewEncoder(w).Encode(map[string]any{"n": sends.Add(1)})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/cached") {
			w.Header().Set("Cache-Control", "private, max-age=3600")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path, "raw": r.URL.EscapedPath(), "device": r.Header.Get("X-Persona-Device"),
			"auth": r.Header.Get("Authorization"), "forged": r.Header.Get("X-Persona-Forged")})
	}))
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-feed", Version: "1.0.0",
		Init: func(_ context.Context, ip extension.InitializeParams, h *pserve.Hub) (map[string]any, error) {
			role.Store(ip.Hub.Role)
			caps.Store(ip.Hub.Capabilities)
			go func() {
				time.Sleep(300 * time.Millisecond)
				_ = h.Notify("api.broadcast", map[string]any{"type": "examplefeed", "n": 1})
				_ = h.Notify("api.broadcast", map[string]any{"type": "sessions", "n": 2})
			}()
			return nil, nil
		},
		Namespaces: map[string]pserve.Handler{"assistant": func(_ context.Context, method string, params json.RawMessage) (any, error) {
			switch method {
			case "credentials":
				return map[string]any{"env": map[string]string{"FEED_BASE": "http://127.0.0.1:9", "FEED_TOKEN": "feed-secret", "PERSONA_HUB_TOKEN": "stolen"}}, nil
			case "summary":
				var p struct {
					Summary string `json:"summary"`
				}
				_ = json.Unmarshal(params, &p)
				return map[string]any{"summary": strings.ReplaceAll(p.Summary, "c42", "示例频道")}, nil
			}
			return nil, fmt.Errorf("unknown %s", method)
		}}})
}

func apiPackage(t *testing.T) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "skills", "example-feed"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "feed"), []byte("#!/bin/sh\nPERSONA_TEST_PLUGIN=api exec "+self+" \"$@\"\n"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "skills", "example-feed", "SKILL.md"), []byte("---\nname: example-feed\ndescription: Read the example feed.\n---\nUse it.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "assistant.md"), []byte("## Example feed\n\nThe example feed is read with its skill.\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "skills", "example-feed", "scripts"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "skills", "example-feed", "scripts", "feed.sh"), []byte("#!/bin/bash\n"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"command": "feed", "script": "example-feed/scripts/feed.sh", "title": "示例",
		"connector": "examplefeed", "env": ["FEED_BASE", "FEED_TOKEN"], "base_env": "FEED_BASE", "secret": ["FEED_TOKEN"],
		"rules": [{"method": "GET", "path": "**", "action": "allow"}], "tool": {"name": "example_feed", "description": "Run feed."}}`), 0o644)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-feed", "name": "Example feed", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/feed"}, "http": true},
		"activation_events": ["on_startup"],
		"contributes": {
			"api": [{"id": "feed", "prefix": "examplefeed", "features": ["example_feed", "extension_ui_stream_v1"], "events": ["examplefeed"],
			         "receipts": [{"method": "POST", "path": "/rooms/*/send", "kind": "examplefeed_send"}]},
			        {"id": "lark", "prefix": "lark"}, {"id": "mine", "prefix": "machines"}],
			"assistant": [{"id": "feed", "skills": ["skills/example-feed"], "instructions": "assistant.md", "gate": "gate.json"}],
			"mcp_servers": [{"id": "feed", "command": "feed-agent", "args": ["mcp"], "allow": ["list"], "deny": ["decide"]}]}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// A plugin's part of the app's API: relayed with who is asking (never the credential), its features and its prefix in
// the report the hub shows them from (the receipts it declared with them: the hub keeps those), its declared events
// pushed to the apps; a prefix being moved out of the hub is the plugin's, the hub's own are not; its skills and
// instructions reach the assistant.
func TestUIStreamFeatureReport(t *testing.T) {
	if got := (&Manager{}).features(); slices.Contains(got, extwire.FeatureUIStream) {
		t.Fatal("uninitialized manager advertises stream support")
	}
	h, _ := extensionTestHub(t)
	if got := h.report().Features; featureCount(got, extwire.FeatureUIStream) != 1 {
		t.Fatalf("empty initialized registry must report transport support once: %v", got)
	}
}

func featureCount(features []string, feature string) int {
	count := 0
	for _, candidate := range features {
		if candidate == feature {
			count++
		}
	}
	return count
}

func callReq(t *testing.T, h http.Handler, req *http.Request, token string) (int, map[string]any) {
	t.Helper()
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// A plugin connector declares group with its meta_description (the manifest takes both); the registry takes it with an
// inbox only, and a meta_description longer than a paragraph is refused.
func TestExtensionConnectorDeclaresGroup(t *testing.T) {
	parse := func(caps, desc string) (*extension.Manifest, error) {
		return extension.Parse([]byte(`{"manifest_version": 1, "id": "example-chat", "name": "Example Chat", "version": "1.0.0",
			"engines": {"plugin_protocol": 1}, "runtime": {"command": "bin/chat"},
			"contributes": {"connectors": [{"id": "examplechat", "name": "Example Chat", "capabilities": ` + caps + `,
				"meta_description": ` + strconv.Quote(desc) + `}]}}`))
	}
	m, err := parse(`["inbox", "group", "meta"]`, "示例：messages 是这件事的消息")
	if err != nil {
		t.Fatal(err)
	}
	var items []extension.ConnectorItem
	_ = json.Unmarshal(m.Contributes["connectors"], &items)
	cm := connectorManifest(items[0])
	if !cm.Has(connector.CapGroup) || cm.MetaDescription != "示例：messages 是这件事的消息" {
		t.Fatalf("manifest %+v", cm)
	}
	reg := connector.NewRegistry(nil)
	if err := reg.Add(proc.New(proc.Config{ID: "examplechat", Name: "Example Chat", Manifest: cm})); err != nil {
		t.Fatal(err)
	}
	m, _ = parse(`["status", "group"]`, "")
	_ = json.Unmarshal(m.Contributes["connectors"], &items)
	if err := reg.Add(proc.New(proc.Config{ID: "examplechat2", Name: "x", Manifest: connectorManifest(items[0])})); err == nil ||
		!strings.Contains(err.Error(), "group without an inbox") {
		t.Fatalf("group without an inbox: %v", err)
	}
	if _, err := parse(`["inbox", "group", "meta"]`, strings.Repeat("长", 4001)); err == nil {
		t.Fatal("a meta_description of 4001 characters was taken")
	}
	for caps, desc := range map[string]string{`["inbox", "group"]`: "示例：没声明 meta", `["inbox", "group", "meta"]`: "", `["inbox", "meta"]`: "示例"} {
		if _, err := parse(caps, desc); err == nil {
			t.Errorf("%s with meta_description %q was taken", caps, desc)
		}
	}
	if _, err := parse(`["inbox", "group"]`, ""); err != nil {
		t.Fatalf("group alone: %v", err)
	}
}

// A plugin connector declaring a capability this hub does not have runs with the others, logged once per plugin and
// capability; the hub's own connectors are still held to what they declare.
func TestExtensionConnectorUnknownCapability(t *testing.T) {
	var logs []string
	ps := newExtensions(t.TempDir(), nil, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	// Each plugin and capability is logged once per process: another id each run (go test -count=2).
	id := "example-chat-" + randomHex(4)
	declared := connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox, "future_example", connector.CapGroup}}
	for range 2 {
		m := ps.knownOnly(id, declared)
		if !slices.Equal(m.Capabilities, []connector.Capability{connector.CapInbox, connector.CapGroup}) {
			t.Fatalf("capabilities %v", m.Capabilities)
		}
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "plugin "+id+`: this hub has no connector capability "future_example"`) {
		t.Fatalf("logs %q", logs)
	}
	reg := connector.NewRegistry(nil)
	if err := reg.Add(proc.New(proc.Config{ID: "examplechat", Name: "Example Chat", Manifest: ps.knownOnly(id, declared)})); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(proc.New(proc.Config{ID: "examplechat2", Name: "x", Manifest: declared})); err == nil ||
		!strings.Contains(err.Error(), "unknown capability") {
		t.Fatalf("not through a plugin: %v", err)
	}
}

// The additive catalog capability does not replace the connector's ordinary
// tools. Older hubs use the same unknown-capability filter for catalog (verified
// against the pre-catalog base); current hubs retain both declarations.
func TestExtensionConnectorCatalogRegistration(t *testing.T) {
	ps := newExtensions(t.TempDir(), nil, func(string, ...any) {})
	declared := connector.Manifest{Capabilities: []connector.Capability{connector.CapTools, connector.CapCatalog}}
	kept := ps.knownOnly("example-catalog", declared)
	if !slices.Equal(kept.Capabilities, declared.Capabilities) {
		t.Fatalf("capabilities %v", kept.Capabilities)
	}
	reg := connector.NewRegistry(nil)
	if err := reg.Add(proc.New(proc.Config{ID: "examplecatalog", Name: "Example Catalog", Manifest: kept})); err != nil {
		t.Fatal(err)
	}
}
