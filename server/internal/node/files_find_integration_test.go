package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/node/files"
)

func putFiles(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func gitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Sample Dev")
	t.Setenv("GIT_AUTHOR_EMAIL", "dev@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Sample Dev")
	t.Setenv("GIT_COMMITTER_EMAIL", "dev@example.com")
}

func TestFindFilesGuard(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home}
	putFiles(t, home, "proj/ok.txt", "proj/id_rsa", "proj/keys/id_ed25519", ".ssh/config", ".aws/credentials", ".netrc", "doc.md")

	res, err := n.fileService().Find(context.Background(), home, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Files {
		if strings.Contains(f, "id_rsa") || strings.Contains(f, "id_ed25519") || strings.HasPrefix(f, ".ssh/") || strings.HasPrefix(f, ".aws/") || f == ".netrc" {
			t.Errorf("%q is off limits and was listed", f)
		}
	}
	if !slices.Contains(res.Files, "proj/ok.txt") || !slices.Contains(res.Files, "doc.md") {
		t.Fatalf("ordinary files missing: %v", res.Files)
	}

	// the folders the guard refuses whole, and what is not a folder
	if _, err := n.fileService().Find(context.Background(), filepath.Join(home, ".ssh"), 0); err == nil {
		t.Error("a secrets folder was listed")
	}
	if _, err := n.fileService().Find(context.Background(), "/Users/x/Library/Mobile Documents/com~apple~CloudDocs", 0); err == nil || !strings.Contains(err.Error(), "iCloud") {
		t.Errorf("iCloud: %v", err)
	}
	if _, err := n.fileService().Find(context.Background(), filepath.Join(home, "doc.md"), 0); err == nil {
		t.Error("a file was listed as a folder")
	}
	if _, err := n.fileService().Find(context.Background(), filepath.Join(home, "missing"), 0); err == nil {
		t.Error("a missing folder was listed")
	}
	if _, err := n.fileService().Find(context.Background(), "", 0); err == nil {
		t.Error("an empty path was listed")
	}
	// a link into a secrets folder is refused before it is looked at
	if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(home, "sneak")); err == nil {
		if _, err := n.fileService().Find(context.Background(), filepath.Join(home, "sneak"), 0); err == nil {
			t.Error("a link to a secrets folder was listed")
		}
	}
}

func TestFindFilesAsRPC(t *testing.T) {
	root := t.TempDir()
	n := &Node{home: root, state: newState()}
	putFiles(t, root, "w/a.txt", "w/b/c.txt")
	params, _ := json.Marshal(map[string]any{"cwd": "~/w", "limit": 10})
	out, err := n.handle(context.Background(), "fs.find", params)
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(*files.FindResult)
	if !ok || !slices.Equal(res.Files, []string{"a.txt", "b/c.txt"}) {
		t.Fatalf("fs.find = %#v", out)
	}
}
