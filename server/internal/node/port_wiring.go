package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/node/ports"
	"github.com/sunfayao/persona/server/internal/portprobe"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/streams"
)

// panesOfPID are the panes a process's engine pane variables name (its environment, cached per process), as the
// hosts write PaneIDs.
func (n *Node) panesOfPID(pid int) []string { return n.enginePaneRefs(paneEnvOfPID(pid)) }

// portDialer is how port.dial reaches a port here; a variable for tests, which stand in for Screen Sharing.
var portDialer = portprobe.Dial

// portDial is port.dial: a stream (streams.Mux) to the port on this machine, at the address the scan found it on,
// else at loopback. Only this machine's own addresses are ever dialed.
func (n *Node) portDial(ctx context.Context, params json.RawMessage) (any, error) {
	var p devicewire.PortDial
	if err := json.Unmarshal(params, &p); err != nil || p.ID == "" || p.Port <= 0 || p.Port > 65535 {
		return nil, errors.New("port.dial: need id and port")
	}
	link := p.Link && p.Port == screenPort && p.Pair != "" && n.links != nil
	if link && p.Attach {
		return n.reattachScreen(p.ID, p.Pair, false)
	}
	addr := ""
	if l, ok := n.listedPort(p.Port); ok {
		addr = l.Addr
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := portDialer(dctx, p.Port, addr)
	if err != nil {
		n.kickPorts() // it may be gone: the list says so sooner
		return nil, &rpc.Error{Code: "port_gone", Message: fmt.Sprintf("nothing is listening on port %d", p.Port)}
	}
	if err := n.streams.Open(p.ID); err != nil {
		_ = c.Close()
		return nil, err
	}
	if p.KeepAwake {
		c = &watchedConn{Conn: c, done: awake.watch()}
	}
	if p.Port == screenPort && p.Pair != "" {
		c = n.screens.observe(p.Pair, c) // follow Screen Sharing's login so screen.dial can bind to it (#3954)
		if n.screenStart != nil {
			n.askScreenPermission(p.Pair, nil) // once it signs in: macOS asks for the helper's grant, once per version
		}
		if n.inputs != nil {
			c = n.inputs.wrap(p.Pair, c) // the phone's scroll records out of the stream, to Persona Input (#7775)
		}
	}
	if link {
		_, relay := n.links.Open(p.Pair, c) // the session owns Screen Sharing's connection from here
		n.streams.Start(p.ID, relay)
		return map[string]any{"link": 1}, nil
	}
	if tcp, ok := c.(streams.HalfCloseConn); ok && p.StreamVersion == devicewire.PortStreamHalfCloseV1 {
		n.streams.StartHalfClose(p.ID, tcp, true)
		return devicewire.PortDialReply{StreamVersion: p.StreamVersion}, nil
	}
	n.streams.Start(p.ID, c)
	return map[string]any{}, nil
}

// reattachScreen is a hub relay rejoining the live screen session for pair (#6398): its RFB channel, or with video
// its current video channel. Nothing is dialed or started; the relay must send the session's token first. session_gone
// when there is no such session (or no live video in it): the phone starts over.
func (n *Node) reattachScreen(id, pair string, video bool) (any, error) {
	s := n.links.Get(pair)
	if s == nil || (video && !s.HasVideo()) {
		return nil, &rpc.Error{Code: "session_gone", Message: "no live screen session for this pair"}
	}
	if err := n.streams.Open(id); err != nil {
		return nil, err
	}
	relay, ok := s.Reattach(video)
	if !ok {
		n.streams.Abort(id, errors.New("the screen session ended"))
		return nil, &rpc.Error{Code: "session_gone", Message: "the screen session ended"}
	}
	n.streams.Start(id, relay)
	return map[string]any{"link": 1}, nil
}

func (n *Node) portService() *ports.Service {
	n.portsOnce.Do(func() {
		var logf func(string, ...any)
		if n.logger != nil {
			logf = n.logger.Printf
		}
		n.ports = ports.New(ports.Config{Panes: n.livePortPanes, PaneProcesses: n.paneProcesses, PanesOfPID: n.panesOfPID, Notify: n.notifySessions, Logf: logf})
	})
	return n.ports
}

func (n *Node) livePortPanes() map[string]bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	panes := map[string]bool{}
	for _, a := range n.agents {
		if a.PaneID != "" {
			panes[a.PaneID] = true
		}
	}
	return panes
}

func (n *Node) kickPorts()                             { n.portService().Kick() }
func (n *Node) portsOf(pane string) []proto.Port       { return n.portService().Of(pane) }
func (n *Node) listedPort(port int) (proto.Port, bool) { return n.portService().Listed(port) }
