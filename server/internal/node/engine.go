package node

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/sunfayao/persona/server/internal/herdr"
	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Every node drives herdr: Persona's terminal engine, built in and always on (docs/protocol/protocol.md "Engine").
// Next to it the node registers tmux (how a pane a Claude Code waits in is read and answered) and, when the hub says
// so, botmux (hostbotmux.go). Each host that pushes changes is watched on its own (placement.go, herdrevents.go).

// engineState is the hosts' change streams and whether botmux is one of them.
type engineState struct {
	mu   sync.Mutex
	base context.Context // Serve's context, nil before it

	// botmux: whether botmux's sessions are listed next to herdr's (useBotmux); nil until the hub said, or listed
	// sessions without saying. botmuxSaid is the same "not nil", read without the lock (botmuxPending).
	botmux     *bool
	botmuxSaid atomic.Bool
}

// startPolling starts polling the hosts and watching their changes, for as long as ctx lasts (Serve).
func (n *Node) startPolling(ctx context.Context) {
	go n.herdrAtStart(ctx)
	go n.pollAgents(ctx)
	go n.watchCodexCards(ctx)
	n.watchHosts(ctx)
}

// newHerdr is the herdr host at its default socket, or at the one the node's environment names.
func newHerdr(logf func(format string, args ...any)) host.Host {
	hc := herdr.New()
	hc.Log = logf
	if own := herdr.OwnSession(); logf != nil && own != "" {
		if hc.Session == own {
			logf("herdr: session %s (%s): this node's data directory has a herdr server of its own", own, hc.Socket)
		} else { // HERDR_SESSION / HERDR_SOCKET_PATH in the node's environment (a node started in a herdr pane has them)
			logf("herdr: this node's data directory has its own herdr session (%s), but its environment points at %s: using that, so its Agents belong to whichever node started that server", own, hc.Socket)
		}
	}
	return newHerdrHost(hc, logf)
}

// watchHosts starts watching every registered host that pushes changes, for as long as ctx lasts: herdr's pane-aware
// subscription, every other host's whole-host stream (watchHost). One host's events never refresh another host.
func (n *Node) watchHosts(ctx context.Context) {
	n.eng.mu.Lock()
	defer n.eng.mu.Unlock()
	n.eng.base = ctx
	for _, h := range n.hosts.All() {
		w, ok := h.(host.Watcher)
		if !ok {
			continue
		}
		if h.Kind() == host.Herdr {
			go n.watchHerdr(ctx)
			go n.watchFocus(ctx)
		} else {
			go n.watchHost(ctx, w, n.hostSig[h.Kind()]) // resetSignals built one for every Watcher host
		}
	}
}

func (n *Node) logf(format string, args ...any) {
	if n.logger != nil {
		n.logger.Printf(format, args...)
	}
}

// engines is what node.info reports of herdr on this machine: available when it answers (herdrVersion, what the herdr
// host said), its socket file is there or its binary is on PATH.
func engines(herdrVersion string) []proto.Engine {
	_, pathErr := exec.LookPath("herdr")
	available := herdrVersion != "" || herdr.SocketUp() || pathErr == nil
	return []proto.Engine{{ID: proto.EngineHerdr, Name: proto.EngineHerdr, Available: available, Version: herdrVersion}}
}
