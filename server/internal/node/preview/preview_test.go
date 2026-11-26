package preview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeLaunch(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, ".claude", "launch.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// launch.json as Claude Desktop reads it, with VS Code's comments and trailing commas; each entry it cannot use says why.
func TestReadLaunchFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := writeLaunch(t, root, `{
  // the dev servers
  "version": "0.0.1",
  "configurations": [
    { "name": "web", "runtimeExecutable": "npm", "runtimeArgs": ["run", "dev"], "port": 5173, }, /* the app */
    { "name": "api", "runtimeExecutable": "go", "runtimeArgs": ["run", "./cmd/api"], "port": 8080, "cwd": "server",
      "env": { "PORT": "8080", "MODE": "a b" } },
    { "name": "docs", "url": "http://docs.localhost:4000/guide" },
    { "name": "storybook // not a comment", "runtimeExecutable": "npx", "runtimeArgs": ["storybook", "dev"], "port": "6006" },
    { "name": "staging", "url": "https://staging.example.com/" },
    { "name": "noport", "runtimeExecutable": "npm" },
    { "runtimeExecutable": "npm", "port": 1 },
    { "name": "nothing", "port": 3000 },
    { "name": "badenv", "runtimeExecutable": "x", "port": 3001, "env": { "A=B": "c" } },
    { "name": 7 },
  ],
}`)
	got, err := ReadLaunchFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []Server{
		{Name: "web", Port: 5173, Command: []string{"npm", "run", "dev"}, Cwd: root},
		{Name: "api", Port: 8080, Command: []string{"go", "run", "./cmd/api"}, Cwd: filepath.Join(root, "server"), Env: map[string]string{"PORT": "8080", "MODE": "a b"}},
		{Name: "docs", Port: 4000, URL: "http://docs.localhost:4000/guide"},
		{Name: "storybook // not a comment", Port: 6006, Command: []string{"npx", "storybook", "dev"}, Cwd: root},
		{Name: "staging", Problem: problemNotLocal},
		{Name: "noport", Problem: problemNoPort},
		{Problem: problemNoName},
		{Name: "nothing", Port: 3000, Problem: problemNoRun},
		{Name: "badenv", Port: 3001, Problem: problemBadEnv},
		{Problem: problemNoName},
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("entries:\n%s", g)
	}
	if got := LaunchCommand(&want[1]); got != `env 'MODE=a b' 'PORT=8080' go run ./cmd/api` {
		t.Errorf("api's command line: %s", got)
	}
	if got := LaunchCommand(&Server{Command: []string{"npm", "run", "dev; rm -rf ~"}}); got != `npm run 'dev; rm -rf ~'` {
		t.Errorf("an argument is not quoted: %s", got)
	}

	for name, body := range map[string]string{"not JSON": `{"configurations": [`, "too big": `{"x":"` + strings.Repeat("a", launchFileMax) + `"}`} {
		if _, err := ReadLaunchFile(writeLaunch(t, t.TempDir(), body)); err == nil {
			t.Errorf("%s: read without an error", name)
		}
	}
}

// The session's folder, or the nearest one above it up to the repository's root.
func TestFindLaunchFile(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	mkdir(t, filepath.Join(repo, ".git"))
	deep := mkdir(t, filepath.Join(repo, "packages", "app", "src"))
	if got := New(Config{}).FindLaunchFile(deep); got != "" {
		t.Fatalf("found %s with none there", got)
	}
	top := writeLaunch(t, repo, `{"configurations": []}`)
	if got := New(Config{}).FindLaunchFile(deep); got != top {
		t.Fatalf("from a subfolder: %q, want the repository's %q", got, top)
	}
	app := writeLaunch(t, filepath.Join(repo, "packages", "app"), `{"configurations": []}`)
	if got := New(Config{}).FindLaunchFile(deep); got != app {
		t.Fatalf("the nearer one: %q, want %q", got, app)
	}
	// not above the repository's root
	outer := t.TempDir()
	writeLaunch(t, outer, `{"configurations": []}`)
	inner := filepath.Join(outer, "repo")
	mkdir(t, filepath.Join(inner, ".git"))
	if got := New(Config{}).FindLaunchFile(inner); got != "" {
		t.Fatalf("found %s above the repository", got)
	}
}

func TestPreviewStaysOnThisComputer(t *testing.T) {
	t.Parallel()
	for u, want := range map[string]bool{
		"http://localhost:5173/":        true,
		"https://localhost:8443/x":      true,
		"http://app.localhost:3000/":    true,
		"http://127.0.0.1:8000/":        true,
		"http://127.1.2.3/":             true,
		"http://[::1]:5173/":            true,
		"about:blank":                   true,
		"http://LOCALHOST.:3000/":       true,
		"https://example.com/":          false,
		"http://192.0.2.10:8080/":       false,
		"http://198.51.100.7/":          false,
		"http://localhost.example.com/": false,
		"file:///etc/passwd":            false,
		"javascript:alert(1)":           false,
		"ftp://localhost/":              false,
		"http://203.0.113.5/latest/":    false,
	} {
		if got := allowedTop(u); got != want {
			t.Errorf("allowedTop(%q) = %v, want %v", u, got, want)
		}
	}
	tab := &previewTab{url: "http://localhost:5173/a/b?c"}
	for in, want := range map[string]string{
		"/settings":            "http://localhost:5173/settings",
		"localhost:3000/x":     "http://localhost:3000/x",
		" http://[::1]:8000/ ": "http://[::1]:8000/",
		"https://example.com":  "https://example.com",
	} {
		if got := tab.resolve(in); got != want {
			t.Errorf("resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMobileUA(t *testing.T) {
	t.Parallel()
	desk := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	want := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36"
	if got := mobileUA(desk); got != want {
		t.Fatalf("mobileUA = %q", got)
	}
}

func TestPreviewViewportClamp(t *testing.T) {
	t.Parallel()
	got := previewViewport{w: 10, h: 99999, dpr: 3.5}.clamp()
	if got != (previewViewport{w: previewMinSide, h: previewMaxHeight, dpr: previewMaxDPR}) {
		t.Fatalf("clamp = %+v", got)
	}
	if got := (previewViewport{w: 800, h: 600}).clamp(); got.dpr != 1 {
		t.Fatalf("no dpr: %+v", got)
	}
}

func TestFindBrowserHonoursTheVariable(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "my-chrome")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(previewBrowserEnv, bin)
	if b, err := New(Config{}).findBrowser(); err != nil || b.path != bin || b.name != "my-chrome" {
		t.Fatalf("findBrowser = %+v, %v", b, err)
	}
	t.Setenv(previewBrowserEnv, filepath.Join(t.TempDir(), "absent"))
	if _, err := New(Config{}).findBrowser(); err == nil || !strings.Contains(err.Error(), previewBrowserEnv) {
		t.Fatalf("a browser that is not there: %v", err)
	}
}

func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
