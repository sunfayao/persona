package link

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func routeTestAddresses(ips ...string) func() ([]net.Addr, error) {
	return func() ([]net.Addr, error) {
		addrs := make([]net.Addr, 0, len(ips))
		for _, raw := range ips {
			ip, addr, _ := net.ParseCIDR(raw)
			addr.IP = ip
			addrs = append(addrs, addr)
		}
		return addrs, nil
	}
}

func TestRouteMemoryInvalidation(t *testing.T) {
	t.Setenv("https_proxy", "http://first.example.com:3128")
	t.Setenv("no_proxy", "")
	addresses := routeTestAddresses("192.0.2.5/24", "2001:db8::8/64")
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, m *routeMemory, c *Config)
	}{
		{"credential", func(_ *testing.T, _ *routeMemory, c *Config) { c.Token = "new-token" }},
		{"hub identity", func(_ *testing.T, _ *routeMemory, c *Config) { c.Hub = "another-hub" }},
		{"SSH setting", func(_ *testing.T, _ *routeMemory, c *Config) { c.SSH.Target = "localhost:443" }},
		{"explicit proxy", func(_ *testing.T, _ *routeMemory, c *Config) { c.Proxy = "http://first.example.com:3128" }},
		{"environment proxy", func(t *testing.T, _ *routeMemory, _ *Config) {
			t.Setenv("https_proxy", "http://second.example.com:3128")
		}},
		{"environment bypass list", func(t *testing.T, _ *routeMemory, _ *Config) {
			t.Setenv("no_proxy", "other.example.com")
		}},
		{"IPv6 prefix changed", func(_ *testing.T, m *routeMemory, _ *Config) {
			m.addresses = routeTestAddresses("192.0.2.5/24", "2001:db8:1::8/64")
		}},
		{"missing file", func(t *testing.T, m *routeMemory, _ *Config) {
			if err := os.Remove(RouteHintPath(m.path)); err != nil {
				t.Fatal(err)
			}
		}},
		{"corrupt file", func(t *testing.T, m *routeMemory, _ *Config) {
			if err := os.WriteFile(RouteHintPath(m.path), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Hub: "hub", Machine: "laptop", Token: "original-token", Trust: TrustSystem,
				Endpoints: []Endpoint{{URL: "https://hub.example.com"}}, SSH: &SSH{Server: "user@example.com"}}
			path := filepath.Join(t.TempDir(), "link.json")
			if err := Update(path, func(f *Config) error { *f = c; return nil }); err != nil {
				t.Fatal(err)
			}
			before := routeMemory{path: path, addresses: addresses}
			before.beforeDial(&c)
			before.connected(KindSSH)
			if h := readRouteHint(path); h.Kind != KindSSH {
				t.Fatalf("saved %q, want ssh", h.Kind)
			}

			after := routeMemory{path: path, addresses: addresses}
			tc.change(t, &after, &c)
			last := after.beforeDial(&c)
			if got := routeOrder(&c, true, last); last != "" || !slices.Equal(got, []string{"", KindProxy, KindSSH}) {
				t.Fatalf("invalidated hint %q produced routing order %v", last, got)
			}
			if _, err := os.Stat(RouteHintPath(path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalidated cache was left on disk (%v)", err)
			}
		})
	}
}

func TestRouteMemoryNetworkChange(t *testing.T) {
	c, path := seedRouteMemory(t)
	m := routeMemory{path: path, addresses: routeTestAddresses("192.0.2.5/24", "2001:db8::8/64")}
	if got := m.beforeDial(&c); got != KindSSH {
		t.Fatalf("first dial: got %q, want ssh", got)
	}
	m.addresses = routeTestAddresses("192.0.2.5/24", "2001:db8::9/64")
	m.networkChanged()
	if got := m.beforeDial(&c); got != KindSSH || readRouteHint(path).Kind != KindSSH {
		t.Fatalf("rotating IPv6 on the same prefix lost the hint: got %q, want ssh", got)
	}
	m.addresses = routeTestAddresses("192.0.2.5/24", "2001:db8:1::9/64")
	m.networkChanged()
	if got := m.hint.Kind; got != "" || readRouteHint(path).Kind != "" {
		t.Fatalf("a different prefix kept the hint: got %q, want no hint", got)
	}
}

func TestRouteMemoryUnknownNetwork(t *testing.T) {
	for name, unknown := range map[string]func() ([]net.Addr, error){
		"loopback only": routeTestAddresses("127.0.0.1/8", "::1/128"),
		"lookup failed": func() ([]net.Addr, error) { return nil, errors.New("unavailable") },
	} {
		t.Run(name, func(t *testing.T) {
			c, path := seedRouteMemory(t)
			known := routeTestAddresses("192.0.2.5/24", "2001:db8::9/64")
			m := routeMemory{path: path, addresses: unknown}
			if got := m.beforeDial(&c); got != "" {
				t.Fatalf("used %q on an unknown network", got)
			}
			if saved := readRouteHint(path).Kind; saved != KindSSH {
				t.Fatalf("unknown network discarded the saved hint: got %q, want ssh", saved)
			}
			// A fallback that connects before the network is known must not overwrite the verified hint.
			m.connected(KindProxy)
			m.networkChanged()
			m.addresses = known
			if got := m.beforeDial(&c); got != KindSSH {
				t.Fatalf("network returned: got %q, want ssh", got)
			}
			m.addresses = unknown
			m.connected(KindSSH) // temporarily unknown when a dial that started on a known network succeeds
			m.networkChanged()
			m.addresses = known
			if got := m.beforeDial(&c); got != KindSSH {
				t.Fatalf("transient lookup failure: got %q, want ssh", got)
			}
			m.addresses = unknown
			c.Token = "new-token"
			if got := m.beforeDial(&c); got != "" || readRouteHint(path).Kind != "" {
				t.Fatalf("new credential kept the old hint on an unknown network: got %q", got)
			}
		})
	}
}

func seedRouteMemory(t *testing.T) (Config, string) {
	t.Helper()
	c := Config{Hub: "hub", Machine: "laptop", Token: "original-token", Trust: TrustSystem}
	path := filepath.Join(t.TempDir(), "link.json")
	if err := Update(path, func(f *Config) error { *f = c; return nil }); err != nil {
		t.Fatal(err)
	}
	m := routeMemory{path: path, addresses: routeTestAddresses("192.0.2.5/24", "2001:db8::8/64")}
	m.beforeDial(&c)
	m.connected(KindSSH)
	if got := readRouteHint(path).Kind; got != KindSSH {
		t.Fatalf("seeded hint %q, want ssh", got)
	}
	return c, path
}

func TestRouteMemoryKeepsSSHOnlyOrder(t *testing.T) {
	for _, always := range []bool{true, false} {
		c := &Config{SSH: &SSH{Always: always}}
		if always {
			c.Endpoints = []Endpoint{{URL: "https://hub.example.com"}}
		}
		if got := routeOrder(c, true, KindProxy); !slices.Equal(got, []string{KindSSH}) {
			t.Errorf("SSH-only routing order %v with Always=%v", got, always)
		}
	}
}

func TestRouteMemoryDoesNotSaveOldPairing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.json")
	c := Config{Hub: "hub", Machine: "laptop", Token: "old-token", Trust: TrustSystem}
	if err := Update(path, func(f *Config) error { *f = c; return nil }); err != nil {
		t.Fatal(err)
	}
	m := routeMemory{path: path, addresses: routeTestAddresses("192.0.2.5/24")}
	m.beforeDial(&c)
	if err := Update(path, func(f *Config) error { f.Token = "new-token"; return nil }); err != nil {
		t.Fatal(err)
	}
	m.connected(KindSSH)
	if _, err := os.Stat(RouteHintPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("found a route hint for the old pairing (%v)", err)
	}
}
