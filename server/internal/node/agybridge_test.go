package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/runtimepkg"
)

func TestAgyBridgeWakesOnExternalDisconnect(t *testing.T) {
	oldFind, oldCmd, oldProbe := findAgyExecutable, agyBridgeCommand, agyBridgeProbeEvery
	t.Cleanup(func() {
		findAgyExecutable, agyBridgeCommand, agyBridgeProbeEvery = oldFind, oldCmd, oldProbe
	})
	agyBridgeProbeEvery = 30 * time.Second
	findAgyExecutable = func() string { return "agy" }
	synctest.Test(t, func(t *testing.T) {
		start := make(chan struct{}, 1)
		agyBridgeCommand = func(context.Context, *RuntimeHost, string, string) (*exec.Cmd, error) {
			start <- struct{}{}
			return nil, errors.New("test companion")
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		host := NewRuntimeHost("test", "runtime.sock")
		client, hello := startAgyHello(t, ctx, host, "external")
		if err := <-hello; err != nil {
			t.Fatal(err)
		}
		host.setAgyBridge(true)
		defer host.setAgyBridge(false)
		synctest.Wait()
		select {
		case <-start:
			t.Fatal("started while external agy was connected")
		default:
		}
		_ = client.Close()
		select {
		case <-start:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("builtin agy did not resume within 100ms of external disconnect")
		}
	})
}

func startAgyHello(t *testing.T, ctx context.Context, host *RuntimeHost, instance string) (net.Conn, <-chan error) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	go host.serve(ctx, server)
	peer := rpc.New("test", client)
	go peer.Serve(ctx, client)
	hello := make(chan error, 1)
	go func() {
		hello <- peer.Call(ctx, "runtime.hello", map[string]any{"runtime": "agy", "instance": instance}, nil)
	}()
	return client, hello
}

func TestAgyBridgeSupervisesAndYieldsToExternalRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script fixture")
	}
	dir := t.TempDir()
	marks := filepath.Join(dir, "starts")
	oldFind, oldCmd, oldMin, oldProbe := findAgyExecutable, agyBridgeCommand, agyBridgeRestartMin, agyBridgeProbeEvery
	t.Cleanup(func() {
		findAgyExecutable, agyBridgeCommand = oldFind, oldCmd
		agyBridgeRestartMin, agyBridgeProbeEvery = oldMin, oldProbe
	})
	agyBridgeRestartMin, agyBridgeProbeEvery = 10*time.Millisecond, 10*time.Millisecond
	var probes atomic.Int32
	findAgyExecutable = func() string {
		probes.Add(1)
		return "/opt/agent/bin/agy"
	}
	agyBridgeCommand = func(ctx context.Context, rh *RuntimeHost, agyBin, instance string) (*exec.Cmd, error) {
		script := fmt.Sprintf("echo %s >> %q; trap 'echo stopped >> %q; exit 0' TERM; while :; do sleep 0.02; done", agyBin, marks, marks)
		return exec.Command("/bin/sh", "-c", script), nil
	}

	n, hub := runtimeNode(t)
	assistantHub(hub)
	hello := assistantRuntimeHello()
	hello["runtime"], hello["agent"], hello["launcher"] = "agy", "agy", "agy"
	ext := dialRuntime(t, n, hello)

	// While an external agy runtime is connected, the built-in supervisor yields.
	n.runtimeHost.setAgyBridge(true)
	defer n.runtimeHost.setAgyBridge(false)
	waitFor(t, "probed while external agy runtime connected", func() bool { return probes.Load() >= 2 })
	if len(starts(marks)) != 0 {
		t.Fatalf("started while external agy runtime was connected: %v", starts(marks))
	}

	// Once the external runtime leaves, the built-in supervisor starts companion.mjs and stops cleanly.
	_ = ext.conn.Close()
	waitFor(t, "built-in agy companion started", func() bool { return len(starts(marks)) == 1 })
	if got := starts(marks); got[0] != "/opt/agent/bin/agy" {
		t.Fatalf("unexpected agy binary: %v", got)
	}
	n.runtimeHost.setAgyBridge(false)
	if got := starts(marks); len(got) != 2 || got[1] != "stopped" {
		t.Fatalf("companion was not stopped cleanly: %v", got)
	}
}

func TestAgyBridgeCancelsHandoffWhenExternalDisconnects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		host := NewRuntimeHost("test", "runtime.sock")
		host.agy.instance = "builtin"
		builtin, hello := startAgyHello(t, ctx, host, "builtin")
		defer builtin.Close()
		if err := <-hello; err != nil {
			t.Fatal(err)
		}
		handle, _ := host.agyPeers("builtin")
		host.reg.setSessions(handle, []proto.Session{{SessionID: "active", Status: proto.StatusWorking}})
		external, hello := startAgyHello(t, ctx, host, "external")
		synctest.Wait()
		select {
		case err := <-hello:
			t.Fatalf("external runtime did not wait for the working session: %v", err)
		default:
		}
		if err := handle.peer.Call(ctx, "test.probe", nil, nil); !errors.Is(err, rpc.ErrSuspended) {
			t.Fatalf("builtin RPC during drain: %v, want suspended", err)
		}
		_ = external.Close()
		synctest.Wait()
		if err := handle.peer.Call(ctx, "test.probe", nil, nil); err == nil || err.Error() != "no handler for test.probe" {
			t.Fatalf("builtin RPC after canceled drain: %v", err)
		}
	})
}

func TestAgyBridgeJSRuntimeRequiresSQLite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script fixture")
	}
	dir := t.TempDir()
	writeFakeNode := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	noSQLite := writeFakeNode("node-20", "exit 1")
	t.Setenv("PERSONA_NODEJS", noSQLite)
	t.Setenv("PATH", dir)
	t.Setenv("HOME", dir)
	if js, err := findAgyJSRuntime(context.Background()); err == nil && js.Path == noSQLite {
		t.Fatalf("accepted Node without node:sqlite: %+v", js)
	}

	withSQLite := writeFakeNode("node-22", "printf 22.14.0")
	t.Setenv("PERSONA_NODEJS", withSQLite)
	js, err := findAgyJSRuntime(context.Background())
	if err != nil || js.Path != withSQLite {
		t.Fatalf("findAgyJSRuntime = %+v, %v; want %s", js, err, withSQLite)
	}
}

// The companion is told the node's own program, which serves its chats' Persona tools (`<program> mcp`): a Persona
// Link install calls it persona-node, and the companion's lookup by name found only personad (#14582).
func TestAgyBridgeCommandPassesTheNodeProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script fixture")
	}
	dir := t.TempDir()
	for rel, body := range map[string]string{
		"antigravity/package.json":  `{"type":"module"}`,
		"antigravity/companion.mjs": "console.log('ok')\n",
		"runtime-sdk/package.json":  `{"type":"module"}`,
		"runtime-sdk/index.js":      "export {}\n",
	} {
		p := filepath.Join(dir, "pkgs", agyPackage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fakeNode := filepath.Join(dir, "node-22")
	if err := os.WriteFile(fakeNode, []byte("#!/bin/sh\nprintf 22.19.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runtimepkg.OverrideEnv, filepath.Join(dir, "pkgs"))
	t.Setenv("PERSONA_NODEJS", fakeNode)
	program := filepath.Join(dir, "home", ".persona", "bin", "persona-node")
	oldSelf := selfProgram
	selfProgram = func(string, string) (string, string, error) { return program, program, nil }
	t.Cleanup(func() { selfProgram = oldSelf })

	rh := NewRuntimeHost("test", filepath.Join(dir, "state", "runtime.sock"))
	cmd, err := agyBridgeCommand(context.Background(), rh, "/usr/local/bin/agy", "builtin-agy-1-1")
	if err != nil {
		t.Fatalf("agyBridgeCommand: %v", err)
	}
	i := slices.Index(cmd.Args, "--personad-bin")
	if i < 0 || i+1 >= len(cmd.Args) || cmd.Args[i+1] != program {
		t.Fatalf("cmd.Args %q: want --personad-bin %s", cmd.Args, program)
	}
}
