package pathutil

import (
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// Resolve turns a path as the conversation wrote it into one on this machine: ~ is the home, a relative path is
// in cwd. "" when there is nothing to resolve it against.
func Resolve(path, cwd, home string) string {
	path = proto.ExpandHome(strings.TrimSpace(path), home)
	switch {
	case path == "":
		return ""
	case !filepath.IsAbs(path):
		if cwd == "" {
			return ""
		}
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}

// Expand turns a path as the app gives it (~/…, relative to cwd, empty for cwd itself) into a clean one; "" when
// there is nothing to go by. Links are not followed.
func Expand(path, cwd, home string) string {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		path = cwd
	case path == "~" || strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	case !filepath.IsAbs(path) && cwd != "":
		path = filepath.Join(cwd, path)
	}
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func Within(root, real string) bool {
	rel, err := filepath.Rel(root, real)
	return err == nil && filepath.IsLocal(rel)
}

// Abs makes a path absolute against the node's own working directory (a relative path asked with no cwd), with
// Windows short names spelled out (longPath). The file tools resolve every client path through here, so the guard and
// the operations see the names the entries have.
func Abs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return longPath(a)
	}
	return p
}
