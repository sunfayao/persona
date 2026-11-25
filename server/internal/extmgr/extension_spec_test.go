package extmgr

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/host"
)

// Use the actual extension process handler without launching a process. Calls from that handler reach the assistant
// in this process (Options.Assistant); only the package and its granted permission record are synthetic.
func extensionSpec(t *testing.T, h *Manager, id string, granted extension.Permissions) host.Spec {
	t.Helper()
	if h.extensions == nil {
		h.extensions = newExtensions(t.TempDir(), nil, t.Logf)
		h.extensions.h = h
	}
	raw := fmt.Sprintf(`{"manifest_version":1,"id":%q,"name":"Example","version":"1.0.0",
		"engines":{"extension_protocol":1},"runtime":{"command":{%q:"bin/unused"},"http":true},"activation_events":["on_startup"],
		"contributes":{"api":[{"id":"example","prefix":"example"}]},"permissions":{"hub":["inbox.read","inbox.write"]}}`, id, runtime.GOOS+"-"+runtime.GOARCH)
	m, err := extension.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.extensions.reg.AddBuiltin(m, t.TempDir(), true); err != nil {
		t.Fatal(err)
	}
	p := h.extensions.reg.Get(id)
	p.Record.Granted = granted // the manifest deliberately asks for more than some tests granted
	spec, err := h.extensions.specOf(context.Background(), p, &hubExtension{id: id})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
