package node

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A start opens in herdr, or names the Antigravity companion; any other host is refused before anything starts.
func TestCheckStartHost(t *testing.T) {
	n, _ := testNode(t)
	n.hosts = host.NewRegistry(&bareHost{kind: host.Herdr})
	for _, c := range []struct {
		kind, code string
		ok         bool
	}{
		{"", "", true},
		{"herdr", "", true},
		{proto.HostAgy, proto.CodeAgyCompanionOffline, false}, // no companion connected
		{"tmux", "", false},
		{"orca", "", false}, // a host Persona no longer has
	} {
		err := n.checkStartHost(c.kind)
		if c.ok != (err == nil) {
			t.Errorf("%q: %v", c.kind, err)
			continue
		}
		if c.code != "" {
			if code, _ := refusalOf(t, err); code != c.code {
				t.Errorf("%q: code %q", c.kind, code)
			}
		}
	}
	if got := n.startKind(withStartHost(t.Context(), proto.HostAgy)); got != proto.HostAgy {
		t.Errorf("named host: %q", got)
	}
	if got := n.startKind(t.Context()); got != host.Herdr {
		t.Errorf("no host named: %q", got)
	}
}

// Rows say which host they live in.
func TestSessionRowsCarryTheirHost(t *testing.T) {
	n, _ := testNode(t)
	n.panes, n.panePIDs = map[string]string{}, map[string]panePID{}
	for pane, want := range map[string]string{"w1:p1": "herdr", "botmux:s1": "botmux", "": ""} {
		s := proto.Session{SessionID: sidA, PaneID: pane}
		n.decorate(&s)
		if s.Host != want {
			t.Errorf("pane %q: host %q, want %q", pane, s.Host, want)
		}
	}
}

// launchHost is an ownHost that can start sessions; its Spaces fails, so a start stops there (nothing typed) and the
// test sees that it was asked.
type launchHost struct {
	*ownHost
	mu     sync.Mutex
	spaces int
}

func (h *launchHost) Spaces(context.Context) ([]host.Space, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.spaces++
	return nil, errors.New("not running")
}
func (h *launchHost) CreateSpace(context.Context, string, string) (*host.Created, error) {
	return nil, errors.New("not running")
}
func (h *launchHost) CreateSlot(context.Context, string, string, string) (*host.Created, error) {
	return nil, errors.New("not running")
}
func (h *launchHost) Run(context.Context, host.PaneRef, string) error       { return nil }
func (h *launchHost) NameAgent(context.Context, host.PaneRef, string) error { return nil }
func (h *launchHost) asked() int                                            { h.mu.Lock(); defer h.mu.Unlock(); return h.spaces }

// startNode drives herdr through a launchHost: a start reaches it and stops at its spaces.
func startNode(t *testing.T) (*Node, *launchHost) {
	t.Helper()
	n, _, _, _ := ownNode(t)
	hh := &launchHost{ownHost: newOwnHost(host.Herdr)}
	n.hosts = host.NewRegistry(hh)
	return n, hh
}
