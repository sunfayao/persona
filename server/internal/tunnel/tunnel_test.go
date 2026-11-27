package tunnel

import (
	"bufio"
	"context"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"
)

func hubKey(t *testing.T) (*HubKey, string) {
	t.Helper()
	k, err := LoadOrCreateHubKey(filepath.Join(t.TempDir(), "tailcat-hub.json"))
	if err != nil {
		t.Fatal(err)
	}
	return k, k.Public()
}

// A node with a region already picked: InitNodeKey needs no network then.
func nodeKeyFile(t *testing.T, region int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tailcat.json")
	if err := writeJSON(p, &NodeKey{Key: key.NewNode(), PresharedKey: tailcat.NewPresharedKey(), RegionID: region}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKeyFilesAreFixedAndPrivate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tailcat-hub.json")
	a, err := LoadOrCreateHubKey(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreateHubKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.Public() != b.Public() || !strings.HasPrefix(a.Public(), "nodekey:") {
		t.Fatalf("hub key changed or malformed: %s vs %s", a.Public(), b.Public())
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("hub key file mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestInitNodeKeyAddsHubsAndKeepsTheAddress(t *testing.T) {
	p := nodeKeyFile(t, 7)
	_, hub1 := hubKey(t)
	_, hub2 := hubKey(t)
	if _, err := LoadNodeKey(p); err == nil || !strings.Contains(err.Error(), "no hub key") {
		t.Fatalf("a node with no hub key loaded (tailcat would let everyone in): %v", err)
	}
	k1, err := InitNodeKey(context.Background(), p, hub1, "")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := InitNodeKey(context.Background(), p, hub2, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InitNodeKey(context.Background(), p, hub2, ""); err != nil { // again: no duplicate
		t.Fatal(err)
	}
	k, err := LoadNodeKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if k1.Addr() != k2.Addr() || k.Addr() != k1.Addr() {
		t.Fatal("adding a hub changed the address")
	}
	if len(k.Hubs) != 2 {
		t.Fatalf("hubs %v, want the two", k.Hubs)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("node key file mode %v, want 0600", fi.Mode().Perm())
	}
	ci, err := tailcat.ParseAddr(k.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if ci.RegionID != 7 || len(ci.Region) != 0 || ci.PresharedKey.IsZero() || ci.ServerPublic.NodePublic != k.Key.Public() {
		t.Fatalf("address %+v: want region id 7 only, the PSK and the node key", ci)
	}
	if _, err := InitNodeKey(context.Background(), p, "not-a-key", ""); err == nil {
		t.Fatal("took a malformed hub key")
	}
}

// fakeBinary is a "personad" that runs here and says its version.
func fakeBinary(t *testing.T, dir, version string) string {
	t.Helper()
	p := filepath.Join(dir, "personad-"+version)
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// A wrong checksum (or a short stream) is refused before anything is replaced.
func TestUploadChecksTheHeader(t *testing.T) {
	dir := t.TempDir()
	exe := fakeBinary(t, dir, "v1")
	for _, head := range []string{
		`{"size":10,"sha256":"00"}`,
		`{"size":0,"sha256":"` + strings.Repeat("0", 64) + `"}`,
		`{"size":5,"sha256":"` + strings.Repeat("0", 64) + `"}`,
	} {
		a, b := net.Pipe()
		go func() { _ = install(bufio.NewReader(b), exe); b.Close() }()
		_, _ = io.WriteString(a, head+"\n")
		a.Close()
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "v1") {
		t.Fatalf("exe is %q, want v1 kept", b)
	}
}

// The real thing, over Tailscale's public DERP relays (needs the network: PERSONA_TEST_TAILCAT=1): the allowed hub
// reaches the node port and can upload; another key gets no answer at all.
func TestTailcatEndToEnd(t *testing.T) {
	if os.Getenv("PERSONA_TEST_TAILCAT") != "1" {
		t.Skip("needs the network: PERSONA_TEST_TAILCAT=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	hk, hub := hubKey(t)
	p := filepath.Join(t.TempDir(), "tailcat.json")
	k, err := InitNodeKey(ctx, p, hub, "") // picks the closest region: the network part
	if err != nil {
		t.Fatal(err)
	}
	s := k.Server(t.Logf)
	s.Logf = Logf(t.Logf)
	defer s.Close()
	ln, err := s.Listen(ctx, "tcp", ":18662")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	up, err := s.Listen(ctx, "tcp", ":18663")
	if err != nil {
		t.Fatal(err)
	}
	exe := fakeBinary(t, t.TempDir(), "v1")
	upgraded := make(chan struct{}, 1)
	go ServeUpload(up, exe, func() { upgraded <- struct{}{} })

	// The address the node prints (no relay details in it) is the one its server answers to.
	ci, err := tailcat.ParseAddr(s.TailcatAddr())
	if err != nil {
		t.Fatal(err)
	}
	mine, _ := tailcat.ParseAddr(k.Addr())
	if !ci.ServerPublic.Equal(mine.ServerPublic) || !ci.ServerDiscoPublic.Equal(mine.ServerDiscoPublic) || !ci.PresharedKey.Equal(mine.PresharedKey) {
		t.Fatal("the printed address does not match the server's keys")
	}

	d := &Dialer{Key: hk.Key, Logf: t.Logf}
	defer d.Close()
	start := time.Now()
	c, err := d.Dial(ctx, string(k.Addr()), NodePort)
	if err != nil {
		t.Fatalf("hub dial: %v", err)
	}
	t.Logf("tunnel up in %s", time.Since(start).Round(time.Millisecond))
	_, _ = io.WriteString(c, "{\"id\":\"1\"}\n")
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || line != "{\"id\":\"1\"}\n" {
		t.Fatalf("echo: %q %v", line, err)
	}
	c.Close()
	if path, err := d.Path(ctx, string(k.Addr())); err != nil {
		t.Errorf("path: %v", err)
	} else {
		t.Logf("path: %s", path)
	}

	uc, err := d.Dial(ctx, string(k.Addr()), UploadPort)
	if err != nil {
		t.Fatal(err)
	}
	if err := Upload(ctx, uc, fakeBinary(t, t.TempDir(), "v2")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	uc.Close()
	select {
	case <-upgraded:
	case <-time.After(10 * time.Second):
		t.Fatal("no restart after the upload")
	}

	// Ports other than the node's two are not served.
	pctx, pcancel := context.WithTimeout(ctx, 10*time.Second) // filtered ports time out rather than refuse
	defer pcancel()
	if c, err := d.Dial(pctx, string(k.Addr()), 22); err == nil {
		c.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Read(make([]byte, 1)); err == nil {
			t.Fatal("port 22 answered on the node's address")
		}
		c.Close()
	}

	// A client with any other key: never let in.
	stranger := &Dialer{Key: key.NewNode(), Logf: t.Logf}
	defer stranger.Close()
	sctx, scancel := context.WithTimeout(ctx, 30*time.Second)
	defer scancel()
	if c, err := stranger.Dial(sctx, string(k.Addr()), NodePort); err == nil {
		c.Close()
		t.Fatal("a key the node does not list got in")
	}
}

// The hub and `personad hub tailcat-key` creating the key at once agree on one.
func TestHubKeyCreatedOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tailcat-hub.json")
	const n = 8
	keys := make(chan string, n)
	for range n {
		go func() {
			k, err := LoadOrCreateHubKey(p)
			if err != nil {
				keys <- "error: " + err.Error()
				return
			}
			keys <- k.Public()
		}()
	}
	first := <-keys
	for range n - 1 {
		if k := <-keys; k != first {
			t.Fatalf("two keys: %s and %s", first, k)
		}
	}
	if k, _ := LoadOrCreateHubKey(p); k.Public() != first {
		t.Fatal("the file holds another key than the one handed out")
	}
}

// A key file copied in by hand (cp keeps 0644) is made the owner's only when read.
func TestKeyFileMadePrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tailcat-hub.json")
	if _, err := LoadOrCreateHubKey(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateHubKey(p); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v after reading, want 0600", fi.Mode().Perm())
	}
}

// bigBinary is size bytes gzip cannot shrink: far more than the socket buffers hold, like a real personad.
func bigBinary(t *testing.T, size int) string {
	t.Helper()
	b := make([]byte, size)
	_, _ = rand.Read(b)
	p := filepath.Join(t.TempDir(), "personad-big")
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// A node that turns an upload away reads what the hub is still sending before it closes: closing with unread data
// answers with a reset, which can destroy the answer before the hub reads it.
func TestRefusedUploadIsDrained(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go ServeUpload(ln, fakeBinary(t, t.TempDir(), "v1"), nil)
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	body, _ := os.ReadFile(bigBinary(t, 16<<20))
	if _, err := c.Write(append([]byte(`{"size":-1,"sha256":""}`+"\n"), body...)); err != nil {
		t.Fatalf("send after the refusal: %v, want read and dropped", err)
	}
	if line, _ := bufio.NewReader(c).ReadString('\n'); !strings.Contains(line, "out of range") {
		t.Fatalf("answer %q, want the size refused", line)
	}
}

// The hub reads the answer while it sends: a node that refuses and closes at once (an older one) still says why.
func TestUploadHearsAnEarlyRefusal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = bufio.NewReader(c).ReadString('\n')
		_, _ = io.WriteString(c, `{"error":"size 1 out of range"}`+"\n")
		c.Close() // with the binary unread: a reset
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Upload(ctx, c, bigBinary(t, 16<<20)); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("upload: %v, want the node's reason", err)
	}
}
