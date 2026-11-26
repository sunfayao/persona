package files

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
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

func TestFindFilesInRepo(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	n := New(testConfig(root))
	repo := filepath.Join(root, "repo")
	putFiles(t, repo, "main.go", "src/app/App.tsx", "src/util.ts", "gone.txt", "notes/todo.md", ".gitignore")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n*.log\ndist/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPullGit(t, root, "init", "--initial-branch=main", repo)
	runPullGit(t, repo, "add", "--", ".")
	runPullGit(t, repo, "commit", "-m", "Add sample files")
	// after the commit: an untracked file, ignored ones, a tracked file deleted from disk
	putFiles(t, repo, "src/new.ts", "node_modules/pkg/index.js", "debug.log", "dist/out.js")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	res, err := n.Find(context.Background(), repo, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "main.go", "notes/todo.md", "src/app/App.tsx", "src/new.ts", "src/util.ts"}
	got := slices.Clone(res.Files)
	slices.Sort(got)
	if res.Source != "git" || !slices.Equal(got, want) || res.Truncated {
		t.Fatalf("repo list = %+v, want %v", res, want)
	}

	// A subdirectory of the work tree lists its own files, relative to it.
	sub, err := n.Find(context.Background(), filepath.Join(repo, "src"), 0)
	if err != nil {
		t.Fatal(err)
	}
	got = slices.Clone(sub.Files)
	slices.Sort(got)
	if sub.Source != "git" || !slices.Equal(got, []string{"app/App.tsx", "new.ts", "util.ts"}) {
		t.Fatalf("subdir list = %+v", sub)
	}
}

func TestFindFilesKeepsEmptyGitResult(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	n := New(testConfig(root))
	repo := filepath.Join(root, "repo")
	putFiles(t, repo, "generated/bundle.js")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("generated/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPullGit(t, root, "init", "--initial-branch=main", repo)

	res, err := n.Find(context.Background(), filepath.Join(repo, "generated"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "git" || len(res.Files) != 0 || res.Truncated {
		t.Fatalf("ignored directory = %+v, want an empty Git result", res)
	}
}

func TestFindFilesWalksWhatIsNotARepo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	n := New(testConfig(root))
	dir := filepath.Join(root, "plain")
	putFiles(t, dir, "top.txt", "a/mid.txt", "a/b/deep.txt", "node_modules/pkg/x.js", ".git/config", ".cache/blob", "__pycache__/m.pyc", "Z.md")
	if err := os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}

	res, err := n.Find(context.Background(), dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "walk" || res.Truncated {
		t.Fatalf("walk = %+v", res)
	}
	// breadth first: shallow files before deeper ones; skipped folders and the folder link are not entered
	want := []string{"Z.md", "link", "top.txt", "a/mid.txt", "a/b/deep.txt"}
	if !slices.Equal(res.Files, want) {
		t.Fatalf("walk files = %v, want %v", res.Files, want)
	}
	if res.Cwd != dir {
		t.Fatalf("cwd = %q", res.Cwd)
	}
}

func TestFindFilesCaps(t *testing.T) {
	root := t.TempDir()
	n := New(testConfig(root))
	putFiles(t, root, "plain/1.txt", "plain/2.txt", "plain/sub/3.txt", "plain/sub/4.txt")
	res, err := n.Find(context.Background(), filepath.Join(root, "plain"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Files) != 3 || res.Files[2] != "sub/3.txt" && res.Files[2] != "2.txt" {
		t.Fatalf("walk cap = %+v", res)
	}
	if res.Files[0] != "1.txt" || res.Files[1] != "2.txt" {
		t.Fatalf("the shallow files come first: %v", res.Files)
	}

	gitEnv(t)
	repo := filepath.Join(root, "repo")
	putFiles(t, repo, "a.txt", "b.txt", "c.txt", "d.txt")
	runPullGit(t, root, "init", "--initial-branch=main", repo)
	res, err = n.Find(context.Background(), repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "git" || !res.Truncated || len(res.Files) != 2 {
		t.Fatalf("git cap = %+v", res)
	}
	// untracked and uncommitted files are listed too; the asked limit above the cap is the cap
	res, err = n.Find(context.Background(), repo, maxFindFiles*10)
	if err != nil || len(res.Files) != 4 || res.Truncated {
		t.Fatalf("uncapped = %+v, %v", res, err)
	}
}

func TestFindFilesCachesBriefly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config := testConfig(root)
	clock := &testClock{now: time.Unix(100, 0)}
	config.Clock = clock
	n := New(config)
	dir := filepath.Join(root, "d")
	putFiles(t, dir, "one.txt")
	first, err := n.Find(context.Background(), dir, 0)
	if err != nil || len(first.Files) != 1 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	putFiles(t, dir, "two.txt")
	again, _ := n.Find(context.Background(), dir, 0)
	if len(again.Files) != 1 {
		t.Fatalf("a list inside its TTL is reused: %v", again.Files)
	}
	// a caller must not be able to change what the cache holds
	again.Files[0] = "changed"
	third, _ := n.Find(context.Background(), dir, 0)
	if third.Files[0] != "one.txt" {
		t.Fatalf("cache was changed through a result: %v", third.Files)
	}
	clock.now = clock.now.Add(findTTL + time.Second)
	fresh, _ := n.Find(context.Background(), dir, 0)
	if len(fresh.Files) != 2 {
		t.Fatalf("past its TTL the directory is listed again: %v", fresh.Files)
	}
}
