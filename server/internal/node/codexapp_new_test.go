package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/osproc"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// fakeCodexAppSeed stands in for the App's codex starting a chat: it records what it was asked and writes the chat's
// rollout (as thread/name/set has the app-server do) under the App's home, with the originator Persona's chats carry.
type fakeCodexAppSeed struct {
	mu    sync.Mutex
	seeds []codexAppSeed
	fail  error
}

type codexAppSeed struct{ cli, home, dir, title, thread string }

func (f *fakeCodexAppSeed) seeded() []codexAppSeed {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]codexAppSeed(nil), f.seeds...)
}

func writeCodexAppRollout(t *testing.T, home, thread, originator, cwd string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "11", "12")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-2026-11-12T09-00-00-"+thread+".jsonl")
	meta := fmt.Sprintf(`{"timestamp":"2026-11-12T09:00:00Z","type":"session_meta","payload":{"id":%q,"originator":%q,"source":"vscode","cwd":%q}}`, thread, originator, cwd)
	if err := os.WriteFile(p, []byte(meta+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// codexAppNode is a node whose Codex home has the App's bus (one window owning nothing yet: the App opens a chat at
// codex://threads/<id> and owns it from then on), with the App's codex and its chat start faked.
func codexAppNode(t *testing.T) (*Node, *fakeCodexBus, *fakeCodexAppSeed, *[]string) {
	t.Helper()
	busHome, bus := startFakeCodexBus(t, "")
	n, _ := testNode(t)
	n.dir, n.home = t.TempDir(), filepath.Dir(busHome)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	shellLaunchers(t)
	cli := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := &fakeCodexAppSeed{}
	opened := &[]string{}
	oldCLI, oldSeed, oldOpen := codexAppCLI, seedCodexAppChat, openCodexThread
	codexAppCLI = func() (string, error) { return cli, nil }
	seedCodexAppChat = func(_ context.Context, cli, home, dir, title string) (string, error) {
		seed.mu.Lock()
		defer seed.mu.Unlock()
		if seed.fail != nil {
			return "", seed.fail
		}
		thread := newUUID()
		seed.seeds = append(seed.seeds, codexAppSeed{cli, home, dir, title, thread})
		writeCodexAppRollout(t, home, thread, codexAppClient, dir)
		bus.later = thread
		return thread, nil
	}
	var mu sync.Mutex
	openCodexThread = func(thread string) error {
		mu.Lock()
		*opened = append(*opened, thread)
		mu.Unlock()
		bus.opened.Store(true)
		return nil
	}
	t.Cleanup(func() { codexAppCLI, seedCodexAppChat, openCodexThread = oldCLI, oldSeed, oldOpen })
	return n, bus, seed, opened
}

func startCodexApp(t *testing.T, n *Node, params map[string]any) (proto.Session, error) {
	t.Helper()
	b, _ := json.Marshal(params)
	res, err := n.handle(context.Background(), "session.start", b)
	if err != nil {
		return proto.Session{}, err
	}
	out, _ := json.Marshal(res)
	var r struct {
		Session proto.Session `json:"session"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatal(err)
	}
	return r.Session, nil
}

// What the App could not run as asked is refused before a chat is started: no first message (a chat with none is not
// in the App's sidebar), a read-only plan, a folder that is not there, a secret hand-off, the App not running.
func TestACodexAppStartRefusesWhatTheAppCannotRun(t *testing.T) {
	n, _, seed, opened := codexAppNode(t)
	dir := t.TempDir()
	for _, c := range []struct {
		params map[string]any
		code   string
		says   string
	}{
		{map[string]any{"cwd": dir, "launcher": "codex-app", "prompt": "  "}, "", "第一条消息"},
		{map[string]any{"cwd": dir, "launcher": "codex-app", "task_instructions": "Keep the task scope."}, "codex_needs_prompt", ""},
		{map[string]any{"cwd": dir, "launcher": "codex-app", "prompt": "look", "permission_mode": "plan"}, "readonly_unsupported", ""},
		{map[string]any{"cwd": filepath.Join(dir, "a", "b"), "launcher": "codex-app", "prompt": "hi"}, "", "does not exist"},
		{map[string]any{"cwd": dir, "launcher": "codex-app", "prompt": "hi", "secret_ticket": strings.Repeat("a", 64)}, secretEnvAgentCode, ""},
	} {
		_, err := startCodexApp(t, n, c.params)
		var re *rpc.Error
		if err == nil || c.code != "" && (!errors.As(err, &re) || re.Code != c.code) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%v: %v, want %q %q", c.params, err, c.code, c.says)
		}
	}
	if len(seed.seeded()) != 0 || len(*opened) != 0 {
		t.Fatalf("seeded %v, opened %v", seed.seeded(), *opened)
	}
	n.home = t.TempDir() // a home with no bus: the App is not running for it
	noFakeCodexBus(t)
	_, err := startCodexApp(t, n, map[string]any{"cwd": dir, "launcher": "codex-app", "prompt": "hi"})
	if code, data := refusalOf(t, err); code != "codex_read_only" || data["delivered"] != deliveredNone {
		t.Fatalf("no App: %v", err)
	}
}

// A chat Persona started in the App is one of the App's chats without a record in the App's state, and its subagents
// are the App's subagents; Persona's other Codex threads (the assistant's, originator "persona") are not.
func TestPersonasAppChatsAreTheAppsChats(t *testing.T) {
	home, _ := startFakeCodexBus(t, "x")
	chat := writeCodexAppRollout(t, home, "aaaaaaaa-0000-4000-8000-000000000001", codexAppClient, "/w")
	assistant := writeCodexAppRollout(t, home, "aaaaaaaa-0000-4000-8000-000000000002", "persona", "/w")
	sub := filepath.Join(home, "sub.jsonl")
	os.WriteFile(sub, []byte(`{"type":"session_meta","payload":{"originator":"`+codexAppClient+`","source":{"subagent":{"thread_spawn":{"parent_thread_id":"aaaaaaaa-0000-4000-8000-000000000001","depth":1}}}}}`+"\n"), 0o600)
	if !codexDesktopChat(codexSource{home: home, thread: "aaaaaaaa-0000-4000-8000-000000000001", path: chat}) {
		t.Error("Persona's App chat is not the App's")
	}
	if codexDesktopChat(codexSource{home: home, thread: "aaaaaaaa-0000-4000-8000-000000000002", path: assistant}) {
		t.Error("the assistant's thread taken for the App's")
	}
	if o := readCodexOrigin(sub); !o.Desktop || o.Parent == "" || codexDesktopChat(codexSource{home: home, thread: "sub", path: sub}) {
		t.Errorf("subagent origin %+v", o)
	}
}

// The App's codex starts the chat over stdio as Persona's App client, in its folder, and names it, which writes it; it
// never starts a turn.
func TestCodexAppChatStartsWithTheAppsCodexAndNoTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the native Windows seed is covered by TestWindowsCodexMetadataSession")
	}
	bin, seen := t.TempDir(), filepath.Join(t.TempDir(), "seen")
	cli := filepath.Join(bin, "codex")
	// A fake app-server: answers metadata requests in order, writes what it got and where it ran.
	script := `#!/bin/sh
{ pwd; echo "args:$*"; echo "home:$CODEX_HOME"; } > ` + seen + `
read l1; echo "$l1" >> ` + seen + `; printf '{"id":1,"result":{}}\n'
read l2; echo "$l2" >> ` + seen + `
read l3; echo "$l3" >> ` + seen + `; printf '{"id":2,"error":{"code":-32601,"message":"unknown method"}}\n'
read l4; echo "$l4" >> ` + seen + `; printf '{"method":"thread/started","params":{}}\n{"id":3,"result":{"thread":{"id":"aaaaaaaa-0000-4000-8000-000000000111","path":"/x"}}}\n'
read l5; echo "$l5" >> ` + seen + `; printf '{"id":4,"result":{}}\n'
read l6
`
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, home := t.TempDir(), t.TempDir()
	thread, err := startCodexAppChat(context.Background(), cli, home, dir, "修复导出")
	if err != nil || thread != "aaaaaaaa-0000-4000-8000-000000000111" {
		t.Fatalf("%q %v", thread, err)
	}
	b, _ := os.ReadFile(seen)
	got := string(b)
	real, _ := filepath.EvalSymlinks(dir)
	if first := strings.SplitN(got, "\n", 2)[0]; first != dir && first != real {
		t.Errorf("ran in %q", first)
	}
	for _, want := range []string{"args:app-server", "home:" + home, `"name":"persona_codex_app"`, `"method":"initialized"`,
		`"method":"project/list"`, `"method":"thread/start","params":{"cwd":"` + dir + `"}`, `"method":"thread/name/set","params":{"name":"修复导出","threadId":"aaaaaaaa-0000-4000-8000-000000000111"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "turn/start") {
		t.Error("a turn was started")
	}
}

func TestCodexStdioCancellationStopsWrapperChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper fixture needs Unix shell and FIFO semantics")
	}
	home := t.TempDir()
	cli := filepath.Join(home, "codex")
	// The wrapper waits for a child with inherited stdout. Closing the wrapper alone cannot unblock initialize.
	script := `#!/bin/sh
mkfifo "$CODEX_HOME/block"
cat "$CODEX_HOME/block" &
printf '%s\n' "$!" > "$CODEX_HOME/child"
wait
`
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		server, err := startCodexStdio(ctx, cli, home, home)
		if server != nil {
			server.Close()
		}
		finished <- err
	}()
	var child int
	waitFor(t, "wrapper child to start", func() bool {
		b, _ := os.ReadFile(filepath.Join(home, "child"))
		child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return child > 0
	})
	t.Cleanup(func() { _ = osproc.Kill(child) })
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("initialize succeeded after cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initialize stayed blocked on the wrapper child's stdout")
	}
	waitFor(t, "wrapper child to exit", func() bool { return !osproc.Alive(child) })
}

func TestCodexAppSlugIsTheTitlesASCIIWords(t *testing.T) {
	for _, c := range []struct{ title, want string }{
		{"Fix the CSV export!", "fix-the-csv-export"},
		{"修复 CSV 导出的编码", "csv"},
		{"整理周报", "chat"},
		{strings.Repeat("abcdefghij ", 6), "abcdefghij-abcdefghij-abcdefghij"},
	} {
		if got := codexAppSlug(c.title); got != c.want {
			t.Errorf("%q: %q, want %q", c.title, got, c.want)
		}
	}
}
