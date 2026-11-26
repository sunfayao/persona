package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/extension"
	pserve "github.com/sunfayao/persona/server/internal/extension/serve"
)

// serveLiveExtensionForTest has a connector of its own (examplelive), a channel bound to it and a page. Each start
// writes the connectors its initialize lists as active, one line a start, to LIVE_LOG; its sign-in's first step says
// it came from the program.
func serveLiveExtensionForTest() {
	logPath := os.Getenv("LIVE_LOG")
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-live", Version: "1.0.0",
		Init: func(_ context.Context, p extension.InitializeParams, _ *pserve.Hub) (map[string]any, error) {
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			_, err = fmt.Fprintln(f, strings.Join(p.Active["connectors"], ","))
			return nil, err
		},
		Namespaces: map[string]pserve.Handler{
			"connector": func(_ context.Context, method string, _ json.RawMessage) (any, error) {
				switch method {
				case "status":
					return map[string]string{"state": "ok"}, nil
				case "login.step":
					return map[string]string{"step": "code", "hint": "from the program"}, nil
				}
				return nil, pserve.UnknownMethod(method)
			},
			"ui": func(_ context.Context, method string, _ json.RawMessage) (any, error) {
				if method == "render" {
					return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "text", "text": "live page"}}}}}, nil
				}
				return nil, pserve.UnknownMethod(method)
			}}})
}

// livePackage is the extension serveLiveExtensionForTest runs, started with the hub, its starts written to log.
func livePackage(t *testing.T, log string) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "live"), []byte(fmt.Sprintf("#!/bin/sh\nPERSONA_TEST_PLUGIN=live LIVE_LOG=%q exec %s \"$@\"\n", log, self)), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-live", "name": "Example Live", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/live"}}, "activation_events": ["on_startup"],
		"contributes": {
			"connectors": [{"id": "examplelive", "name": "Example Live", "capabilities": ["status", "login"]}],
			"channel": [{"id": "examplelive", "name": "Example Live", "kind": "im", "connector": "examplelive", "capabilities": {}}],
			"ui": [{"id": "panel", "title": "Panel", "placement": "extension_page"}]}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}
