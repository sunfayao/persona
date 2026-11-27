package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
