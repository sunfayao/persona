package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

func TestAssistantCacheProxyExcludesUnsupportedAndSpare(t *testing.T) {
	f := cliTestBash(t)
	c := assistantCompaction{endpoint: "https://api.example.com", endpointKnown: true}
	on := &assistantwire.CacheKeepalive{Enabled: true, IntervalMinutes: 55, MaxRequests: 24}
	f.n.setManaged([]proto.ManagedLauncher{{Name: "codex-provider", Agent: "codex"}, {Name: "other-provider", Agent: "agy"}})
	for _, launcher := range []string{"codex-provider", "other-provider"} {
		t.Run(launcher, func(t *testing.T) {
			if p := f.n.startCacheWarm("example", launcher, c, modelExecution{CacheKeepalive: on}, false); p != nil {
				p.Close()
				t.Fatal("non-Claude launcher mounted a proxy")
			}
		})
	}
	for _, e := range []modelExecution{{IsMain: true}, {FollowsSetting: true}, {CacheKeepalive: on, Strict: true}} {
		if p := f.n.startCacheWarm("example", "claude", c, e, false); p != nil {
			p.Close()
			t.Fatal("excluded execution mounted proxy")
		}
	}
	if p := f.n.startCacheWarm("example", "claude", c, modelExecution{CacheKeepalive: on}, true); p != nil {
		p.Close()
		t.Fatal("spare/guest mounted proxy")
	}
	if p := f.n.startCacheWarm("example", proto.ExternalLauncherPrefix+strings.Repeat("a", 32), c, modelExecution{CacheKeepalive: on}, false); p != nil {
		p.Close()
		t.Fatal("external runtime mounted proxy")
	}
}

// A main or lead conversation on a Persona-managed Claude launcher whose gateway is an HTTPS address (#12786): the
// start mounts the proxy in its settings, which win over the provider's own ANTHROPIC_BASE_URL that `personad launch`
// puts in the process environment; the proxy forwards to that HTTPS gateway. A start the manager gave no role (as a
// session.send without one did) runs straight on the gateway.
func TestAssistantCacheProxyMountsForHTTPSManagedLauncher(t *testing.T) {
	const gateway = "https://gateway.example/anthropic"
	for _, tc := range []struct {
		name  string
		allow bool
	}{{"attested", true}, {"no-role", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := cliTestBash(t)
			f.fakeLaunch(t)
			f.n.setManaged([]proto.ManagedLauncher{{Name: "gw", Kind: "api", BaseURL: gateway, Model: "example-model"}})
			e := cliExec()
			e.Model = "example-model"
			e.IsMain = true
			if tc.allow {
				e.CacheKeepalive = &assistantwire.CacheKeepalive{Enabled: true, IntervalMinutes: 55, MaxRequests: 24}
			}
			c := f.n.assistantCompactionPolicy("gw", t.TempDir(), "", e)
			if !c.endpointKnown || c.endpoint != gateway {
				t.Fatalf("managed endpoint %q known=%v", c.endpoint, c.endpointKnown)
			}
			id := newUUID()
			p, err := f.n.spawnClaude(id, t.TempDir(), "gw", false, false, e)
			if err != nil {
				t.Fatal(err)
			}
			defer f.n.stopAssistant(p.id)
			if (p.cacheProxy != nil) != tc.allow {
				t.Fatalf("proxy mounted: %v, want %v", p.cacheProxy != nil, tc.allow)
			}
			dir := f.n.dir
			if dir == "" {
				dir = filepath.Join(f.n.home, ".persona")
			}
			b, err := os.ReadFile(filepath.Join(dir, "launcher-settings", "assistant-compact-"+id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var s struct {
				Env map[string]any `json:"env"`
			}
			if err := json.Unmarshal(b, &s); err != nil {
				t.Fatal(err)
			}
			base, _ := s.Env["ANTHROPIC_BASE_URL"].(string)
			if tc.allow {
				if base != p.cacheProxy.BaseURL() || !strings.HasPrefix(base, "http://127.0.0.1:") {
					t.Fatalf("settings base %q, want the proxy", base)
				}
				b := f.n.cacheWarmPool()
				b.mu.Lock()
				_, pooled := b.entries[id]
				b.mu.Unlock()
				if !pooled {
					t.Fatal("proxy not registered for refreshes")
				}
			} else if strings.HasPrefix(base, "http://127.0.0.1:") {
				t.Fatalf("unattested start points at a proxy: %q", base)
			}
		})
	}
}
