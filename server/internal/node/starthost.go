package node

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A start opens in herdr unless it names the Antigravity companion (session.start's host "agy", proto.HostAgy), or it
// adopts a pane of another host (a launcher switch in a tmux or botmux pane). Carried in the start's context, like an
// adopted pane (panes_actions.go).

type startHostKey struct{}

// withStartHost has the start in ctx open in host kind ("" : herdr).
func withStartHost(ctx context.Context, kind string) context.Context {
	if kind == "" {
		return ctx
	}
	return context.WithValue(ctx, startHostKey{}, kind)
}

// startKind is the host a start in ctx opens in: the one it named, else herdr.
func (n *Node) startKind(ctx context.Context) string {
	if k, _ := ctx.Value(startHostKey{}).(string); k != "" {
		return k
	}
	return host.Herdr
}

// checkStartHost refuses a start naming a host it cannot open in; nothing is started. "" and herdr are fine. agy, the
// Antigravity companion's terminal-less sessions (runtimeOfIn), is no terminal host: a start naming it while no
// companion is connected is refused with agy_companion_offline, which a start the hub sent there by default takes as
// its cue to start again in herdr (#13447).
func (n *Node) checkStartHost(kind string) error {
	switch kind {
	case "", host.Herdr:
		return nil
	case proto.HostAgy:
		if !n.agyRuntimeConnected() {
			return refusal(proto.CodeAgyCompanionOffline, i18n.T(i18n.Default, "rpc-errors."+proto.CodeAgyCompanionOffline), nil)
		}
		return nil
	}
	return fmt.Errorf("unknown host %q", kind)
}

// launcherFor is the host of kind as a Launcher. One that cannot start sessions (tmux, botmux) is refused, so a start
// there does not show "unsupported: <kind> cannot do this".
func (n *Node) launcherFor(kind string) (host.Launcher, error) {
	launch, err := hostCapFor[host.Launcher](n, kind)
	if errors.Is(err, host.ErrUnsupported) {
		return nil, refusal(engineMissing, fmt.Sprintf("%s 不支持从手机新开会话，这次没有发送。", kind), nil)
	}
	return launch, err
}

// hostCapFor is the host of kind ("" : herdr) as capability C (hostCap for a start that named its host).
func hostCapFor[C any](n *Node, kind string) (C, error) {
	var none C
	h := n.hosts.Get(cmp.Or(kind, host.Herdr))
	if h == nil {
		return none, fmt.Errorf("no %s host on this machine", cmp.Or(kind, host.Herdr))
	}
	c, ok := h.(C)
	if !ok {
		return none, unsupported(h)
	}
	return c, nil
}
