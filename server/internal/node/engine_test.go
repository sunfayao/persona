package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

func hostKinds(n *Node) []string {
	var out []string
	for _, h := range n.hosts.All() {
		out = append(out, h.Kind())
	}
	return out
}

// The combined engine lists both hosts' sessions at once and drives each pane through its own host.
func TestEngineBothListsBothHosts(t *testing.T) {
	n, f, home := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	t.Setenv("HERDR_SOCKET_PATH", f.sock)
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, hcCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"b"}`)
	hc.setTerminals(hcTerminal("t1", "claude-code", "working", 4242, "claude", hcCwd, "", "", ""))
	seedAgents(t, n, f, claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", "/tmp/s", ""))
	pushes, _ := pollNode(t, n)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.watchEngine(ctx)

	if _, err := request(t, n, "engine.use", map[string]any{"engine": proto.EngineHerdrBoth}); err != nil {
		t.Fatal(err)
	}
	waitPushed(t, pushes, func(m map[string]proto.Session) bool { return live(m, sidA) && live(m, sidB) })
	got := byID(listSessions(t, n))
	a, b := got[sidA], got[sidB]
	if a.PaneID != "w1:p1" || b.PaneID != "herdr-core:t1" || b.Status != proto.StatusWorking {
		t.Fatalf("combined sessions: %+v / %+v", a, b)
	}
	if kinds := hostKinds(n); strings.Join(kinds, ",") != host.Herdr+","+host.HerdrCore+","+host.Tmux {
		t.Errorf("hosts %v", kinds)
	}
	if n.defaultHost().Kind() != host.Herdr {
		t.Error("new sessions of the combined engine must start on classic herdr")
	}
	// Both watchers run: herdr-core's stream is subscribed to, herdr's stays on its own.
	select {
	case <-hc.subbed:
	case <-time.After(3 * time.Second):
		t.Error("herdr-core's events not subscribed to in the combined engine")
	}
}

// waitPushed reads sessions.changed pushes until want matches or the test times out.
func waitPushed(t *testing.T, pushes <-chan hubPush, want func(map[string]proto.Session) bool) map[string]proto.Session {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case p := <-pushes:
			if p.Event != "sessions.changed" {
				continue
			}
			var v struct {
				Sessions []proto.Session `json:"sessions"`
			}
			_ = json.Unmarshal(p.Params, &v)
			if m := byID(v.Sessions); want(m) {
				return m
			}
		case <-deadline:
			t.Fatal("no matching sessions.changed")
			return nil
		}
	}
}

// node.info reports every engine: available, with their versions, when they answer.
func TestEngineDetection(t *testing.T) {
	n, f, _ := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	t.Setenv("HERDR_SOCKET_PATH", f.sock)
	hc.handle("hello", func(map[string]any) any { return map[string]any{"version": "0.1.16", "generation": 1} })
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "herdr"), []byte("#!/bin/sh\necho herdr 0.9.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	n.hosts.Set(engineHosts(proto.EngineHerdrCore, nil)...) // herdr not the engine: its CLI says the version
	got := n.engines(context.Background(), "")
	if len(got) != 9 || got[0] != (proto.Engine{ID: "herdr", Name: "herdr", Available: true, Version: "0.9.3"}) ||
		got[1] != (proto.Engine{ID: "herdr-core", Name: "Herdr App", Available: true, Version: "0.1.16"}) ||
		got[2] != (proto.Engine{ID: "herdr-both", Name: "herdr + Herdr App", Available: true}) ||
		got[3] != (proto.Engine{ID: "orca", Name: "Orca"}) || // no Orca here (TestMain): listed, not available
		got[4] != (proto.Engine{ID: "tern", Name: "Tern"}) || // nor Tern
		got[5] != (proto.Engine{ID: "rex", Name: "Rex"}) || // nor Rex
		got[6] != (proto.Engine{ID: "paseo", Name: "Paseo"}) || // nor Paseo
		got[7] != (proto.Engine{ID: "cindy", Name: "Cindy"}) || // nor Cindy
		got[8] != (proto.Engine{ID: "t3code", Name: "T3 Code"}) { // nor T3 Code
		t.Errorf("engines %+v", got)
	}
	if tr := f.trace(); len(tr) != 0 {
		t.Errorf("herdr asked while not the engine: %v", tr)
	}
	t.Setenv("HC_SOCKET", "/tmp/persona-no-such-dir/c.sock")
	got = n.engines(context.Background(), "0.9.3")
	if got[1].Available || got[0].Version != "0.9.3" || got[2].Available {
		t.Errorf("without herdr-core: %+v", got)
	}
}

// hubPipe gives n a hub connection without starting anything: what n pushes comes out of the channel.
func hubPipe(t *testing.T, n *Node) <-chan hubPush {
	t.Helper()
	pr, pw := io.Pipe()
	n.peer, n.events = rpc.New("n", pw), newHerdrEvents()
	out := make(chan hubPush, 64)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var m hubPush
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				out <- m
			}
		}
	}()
	t.Cleanup(func() { pw.Close() })
	return out
}

func live(m map[string]proto.Session, id string) bool {
	s, ok := m[id]
	return ok && s.Status != proto.StatusOffline && s.PaneID != ""
}

// A refresh that started on herdr and finishes after a switch to herdr-core does not bring herdr's agents back.
func TestEngineSwitchDropsAnOldRefresh(t *testing.T) {
	n, f, home := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	writeClaudePID(t, home, 4242, sidB)
	hc.setTerminals(hcTerminal("t1", "claude-code", "idle", 4242, "claude", hcCwd, "", "", ""))
	entered, release := make(chan struct{}), make(chan struct{})
	f.handle("session.snapshot", func(map[string]any) any {
		close(entered)
		<-release
		return snapshot(nil, nil, []any{claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", "/tmp/s", "")}, nil)
	})
	done := make(chan error, 1)
	go func() { done <- n.refreshHosts(context.Background()) }()
	<-entered
	if err := n.useEngine(context.Background(), proto.EngineHerdrCore); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, errEngineChanged) {
		t.Errorf("old refresh = %v", err)
	}
	if _, ok := n.agent(sidA); ok {
		t.Error("herdr's agent came back")
	}
	waitFor(t, "herdr-core's agent", func() bool { _, ok := n.agent(sidB); return ok }) // the switch's list is built in the background
	if _, ok := n.agent(sidA); ok {
		t.Error("herdr's agent came back")
	}
}

// engine.use answers once the engine is switched; the new engine's list follows when it is built, however long that
// takes (on a machine with dozens of agents and cold caches it took longer than the hub's 30 s, and the app's engine
// picker waited on that call: #635).
func TestEngineUseAnswersBeforeTheList(t *testing.T) {
	n, _, home := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, hcCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"b"}`)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	hc.handle("terminal.list", func(map[string]any) any {
		once.Do(func() { close(entered) })
		<-release
		return map[string]any{"terminals": []any{hcTerminal("t1", "claude-code", "working", 4242, "claude", hcCwd, "", "", "")}}
	})
	pushes := hubPipe(t, n)

	done := make(chan error, 1)
	go func() {
		_, err := n.handle(context.Background(), "engine.use", json.RawMessage(`{"engine":"herdr-core"}`))
		done <- err
	}()
	<-entered // the list is being built, and stays stuck until released
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("engine.use waited for the session list")
	}
	if got := n.engine(); got != proto.EngineHerdrCore {
		t.Fatalf("engine %q", got)
	}

	unblock.Do(func() { close(release) })
	deadline := time.After(5 * time.Second)
	for {
		select {
		case p := <-pushes:
			if p.Event == "sessions.changed" && strings.Contains(string(p.Params), sidB) {
				return
			}
		case <-deadline:
			t.Fatal("the new engine's list was never pushed")
		}
	}
}

// A switch ends every terminal stream of the old engine, telling the hub.
func TestEngineSwitchEndsTerminalStreams(t *testing.T) {
	n, _, _ := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	pushes := hubPipe(t, n)
	sctx, cancel := context.WithCancel(context.Background())
	n.setTermStream(sidA, &termStream{cancel: cancel})
	if err := n.useEngine(context.Background(), proto.EngineHerdrCore); err != nil {
		t.Fatal(err)
	}
	if sctx.Err() == nil {
		t.Error("the stream still runs")
	}
	if n.termStreamOf(sidA) != nil {
		t.Error("the stream is still listed")
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case p := <-pushes:
			if p.Event == "terminal.end" {
				if !strings.Contains(string(p.Params), sidA) || !strings.Contains(string(p.Params), "engine changed") {
					t.Errorf("terminal.end %s", p.Params)
				}
				return
			}
		case <-deadline:
			t.Fatal("no terminal.end")
		}
	}
}

// The new engine's list is pushed even though engine.use's own ctx has ended by then: since #2645 a request's ctx is
// cancelled once its reply is written, which is before the list is built (TestEngineUseAnswersBeforeTheList). The
// list is built on the connection's ctx (rpc.ConnContext).
func TestEngineUseListOutlivesItsRequest(t *testing.T) {
	n, _, home := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, hcCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"b"}`)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	hc.handle("terminal.list", func(map[string]any) any {
		once.Do(func() { close(entered) })
		<-release
		return map[string]any{"terminals": []any{hcTerminal("t1", "claude-code", "working", 4242, "claude", hcCwd, "", "", "")}}
	})

	// The hub's call over a real connection: its reply ends the request's own ctx (rpc.Peer.handle), the connection
	// stays.
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer, n.events = rpc.New("n", n2hW), newHerdrEvents()
	n.peer.OnRequest = n.handle
	hub := rpc.New("h", h2nW)
	pushes := make(chan string, 64)
	hub.OnEvent = func(event string, params json.RawMessage) {
		if event == "sessions.changed" {
			pushes <- string(params)
		}
	}
	conn, endConn := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(conn, n2hR) }()
	go func() { _ = n.peer.Serve(conn, h2nR) }()
	t.Cleanup(func() { endConn(); _ = n2hW.Close(); _ = h2nW.Close() })

	if err := hub.Call(conn, "engine.use", map[string]any{"engine": "herdr-core"}, nil); err != nil {
		t.Fatal(err)
	}
	select { // the list is still built: with the request's ctx it was abandoned before herdr-core was even asked
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the new engine's list was never built once engine.use was answered")
	}
	unblock.Do(func() { close(release) })
	deadline := time.After(5 * time.Second)
	for {
		select {
		case p := <-pushes:
			if strings.Contains(p, sidB) {
				return
			}
		case <-deadline:
			t.Fatal("the new engine's list was lost with engine.use's ctx")
		}
	}
}
