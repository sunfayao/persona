package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/pathutil"
)

func TestDirectoryParentRoots(t *testing.T) {
	root := string(filepath.Separator)
	if filepath.Separator == '\\' {
		root = filepath.VolumeName(t.TempDir()) + `\`
	}
	cases := []struct{ path, want string }{
		{root, ""},
		{filepath.Join(root, "project"), root},
		{filepath.Join(root, "project", "src"), filepath.Join(root, "project")},
	}
	if filepath.Separator == '\\' {
		cases = append(cases, []struct{ path, want string }{
			{`C:\`, ""},
			{`C:/`, ""},
			{`\\server\share`, ""},
			{`\\server\share\`, ""},
			{`\\server\share\project`, `\\server\share\`},
		}...)
	}
	for _, tc := range cases {
		if got := directoryParent(tc.path); got != tc.want {
			t.Errorf("directoryParent(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestDirsAndResolveHomePaths(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	n := &Node{home: home}
	paths := []string{"~/project", project}
	if filepath.Separator == '\\' {
		paths = append(paths, `~\project`)
	}
	for _, path := range paths {
		listed, err := n.dirs(path)
		if err != nil {
			t.Fatalf("dirs(%q): %v", path, err)
		}
		if listed.Path != project || listed.Parent != home {
			t.Errorf("dirs(%q) = %+v, want %q under %q", path, listed, project, home)
		}
		if got := pathutil.Resolve(path, "", n.home); got != project {
			t.Errorf("resolvePath(%q) = %q, want %q", path, got, project)
		}
	}
	if got := pathutil.Resolve("~", "", n.home); got != home {
		t.Errorf("home = %q, want %q", got, home)
	}
	if got := pathutil.Resolve("project", "", n.home); got != "" {
		t.Errorf("a relative path without cwd resolved to %q", got)
	}
	if filepath.Separator != '\\' {
		if got := pathutil.Resolve(`~\project`, home, n.home); got != filepath.Join(home, `~\project`) {
			t.Errorf("POSIX backslash path changed: %q", got)
		}
	}
}
