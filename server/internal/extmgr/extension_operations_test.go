package extmgr

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/connector/github"
	"github.com/sunfayao/persona/server/internal/extension"
)

// waitOp waits until operation id is in one of states, and returns it.
func (h *Manager) opByID(id string) *extOp {
	ops := &h.extensions.ops
	ops.mu.Lock()
	defer ops.mu.Unlock()
	return ops.byID[id]
}

func waitOp(t *testing.T, h *Manager, id string, states ...string) ExtensionOperation {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		op := h.opByID(id)
		if op == nil {
			t.Fatalf("operation %s is gone", id)
		}
		v := h.snapshot(op)
		for _, s := range states {
			if v.State == s {
				return v
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s is %s, not %v: %+v", id, v.State, states, v)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func stepState(v ExtensionOperation, id string) ExtensionOpStep {
	for _, s := range v.Steps {
		if s.ID == id {
			return s
		}
	}
	return ExtensionOpStep{}
}

func opID(t *testing.T, code int, out map[string]any) string {
	t.Helper()
	if code != http.StatusAccepted {
		t.Fatalf("not 202: %d %v", code, out)
	}
	op, _ := out["operation"].(map[string]any)
	id, _ := op["id"].(string)
	if id == "" {
		t.Fatalf("no operation: %v", out)
	}
	return id
}

// opCall makes a call that answers 202 with an operation, and is its id.
func opCall(t *testing.T, h http.Handler, method, path, token, body string) string {
	t.Helper()
	code, out := call(t, h, method, path, token, body)
	return opID(t, code, out)
}

// TestMarketClientGitHubProxy: market requests to GitHub's hosts go through hub.json's github.proxy, others as the
// environment says (#3277: a package from GitHub stalled for minutes without it).
func TestMarketClientGitHubProxy(t *testing.T) {
	h, _, _ := marketHub(t)
	h.cfg.GitHub = &github.Config{Egress: connector.Egress{Proxy: "http://127.0.0.1:3128"}}
	h.extensions.markets.client = nil
	client, err := h.marketClient()
	if err != nil {
		t.Fatal(err)
	}
	tr := client.Transport.(*http.Transport)
	check := func(env string) {
		t.Helper()
		prev := envProxy
		t.Cleanup(func() { envProxy = prev })
		envProxy = func(*http.Request) (*url.URL, error) {
			if env == "" {
				return nil, nil
			}
			return &url.URL{Scheme: "http", Host: env}, nil
		}
		for host, want := range map[string]string{"api.github.com": "127.0.0.1:3128", "objects.githubusercontent.com": "127.0.0.1:3128",
			"ghcr.io": "127.0.0.1:3128", "pkg-containers.githubusercontent.com": "127.0.0.1:3128",
			"github.com": "127.0.0.1:3128", "example.com": env, "registry-mirror.example.com": env, "notgithub.com": env, "notghcr.io": env} {
			req, _ := http.NewRequest("GET", "https://"+host+"/x", nil)
			u, err := tr.Proxy(req)
			got := ""
			if u != nil {
				got = u.Host
			}
			if err != nil || got != want {
				t.Errorf("env proxy %q, %s: %q %v, want %q", env, host, got, err, want)
			}
		}
	}
	check("")               // no proxy in the environment: only GitHub's hosts go through github.proxy
	check("127.0.0.1:7897") // one in the environment: the others take it, GitHub's still github.proxy
}

// extension_market.proxy is independent of the GitHub connector and covers every marketplace host, including mirrors.
func TestMarketClientHTTPAndSOCKS5Proxy(t *testing.T) {
	for _, raw := range []string{"http://proxy.example:3128", "socks5://proxy.example:1080"} {
		t.Run(raw, func(t *testing.T) {
			h, _, _ := marketHub(t)
			h.cfg.GitHub = &github.Config{Egress: connector.Egress{Proxy: "http://127.0.0.1:3128"}}
			h.cfg.ExtensionMarket.Proxy = raw
			h.extensions.markets.client = nil
			prev := envProxy
			t.Cleanup(func() { envProxy = prev })
			envProxy = func(*http.Request) (*url.URL, error) {
				t.Error("the explicit marketplace proxy fell back to the environment")
				return nil, nil
			}
			client, err := h.marketClient()
			if err != nil {
				t.Fatal(err)
			}
			tr := client.Transport.(*http.Transport)
			for _, host := range []string{"api.github.com", "objects.githubusercontent.com", "ghcr.io", "market.example.com", "registry-mirror.example.com"} {
				req, _ := http.NewRequest("GET", "https://"+host+"/x", nil)
				u, err := tr.Proxy(req)
				if err != nil || u == nil || u.String() != raw {
					t.Errorf("%s: proxy %v, %v; want %s", host, u, err, raw)
				}
			}
			mk := &extension.Market{}
			if err := h.configureMarket(mk); err != nil {
				t.Fatal(err)
			}
			if mk.UpstreamProxy == nil || mk.UpstreamProxy.String() != raw {
				t.Fatalf("git marketplace proxy: %v", mk.UpstreamProxy)
			}
		})
	}
}

// An invalid explicit proxy fails instead of bypassing it for a direct connection.
func TestMarketClientInvalidProxyDoesNotFallBack(t *testing.T) {
	h, _, _ := marketHub(t)
	h.cfg.ExtensionMarket.Proxy = "https://proxy.example:3128"
	h.extensions.markets.client = nil
	client, err := h.marketClient()
	if err != nil {
		t.Fatal(err)
	}
	tr := client.Transport.(*http.Transport)
	req, _ := http.NewRequest("GET", "https://api.github.com/x", nil)
	if u, err := tr.Proxy(req); err == nil || u != nil {
		t.Fatalf("invalid proxy fell back: %v, %v", u, err)
	}
	if err := h.configureMarket(&extension.Market{}); err == nil {
		t.Fatal("an invalid proxy reached the marketplace")
	}
}
