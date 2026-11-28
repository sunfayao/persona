package node

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sunfayao/persona/server/internal/runtimepkg"
)

// The node carries the bridges of the runtimes it runs itself (the agy and OpenCode bridges, runtimepkg) and runs them
// with a JavaScript runtime found on the machine: these are the pieces they share.

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// jsRuntime is a JavaScript runtime to run a bridge with: node, or an Electron app's binary run as node.
type jsRuntime struct {
	Path     string
	Electron bool   // ELECTRON_RUN_AS_NODE=1: an app's binary named by PERSONA_NODEJS
	Version  string // the Node.js version it said it is
	From     string // where it was found, for the log
}

// isNodeBinary: the file is a node (by name), not an app binary that runs as node only with ELECTRON_RUN_AS_NODE.
func isNodeBinary(path string) bool {
	b := strings.ToLower(filepath.Base(path))
	return b == "node" || b == "node.exe" || strings.HasPrefix(b, "node-") || strings.HasPrefix(b, "nodejs")
}

// commonNodePaths are where Node.js installers put node, newest nvm version first.
func commonNodePaths() []string {
	home := userHome()
	var out []string
	for _, dir := range agentSystemDirs { // Homebrew, /usr/local
		out = append(out, filepath.Join(dir, "node"))
	}
	out = append(out, filepath.Join(distroNodeDir, "node"), filepath.Join(home, ".volta", "bin", "node"),
		filepath.Join(home, ".local", "bin", "node"))
	nvm, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"))
	sort.Sort(sort.Reverse(sort.StringSlice(nvm)))
	return append(out, nvm...)
}

// env is base to run js with, in a new slice: ELECTRON_RUN_AS_NODE=1 for an app binary, and never for a node. A node
// started from an Electron host (an editor's extension host starts its children with it) inherits the variable; it
// is dropped, or everything the bridge starts would get it too, and an Electron app started from there would run as
// a bare Node.js.
func (js jsRuntime) env(base []string) []string {
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if !strings.HasPrefix(kv, "ELECTRON_RUN_AS_NODE=") {
			out = append(out, kv)
		}
	}
	if js.Electron {
		out = append(out, "ELECTRON_RUN_AS_NODE=1")
	}
	return out
}

// writePackage writes a package's files out under dir/<hash> once (a version written before is reused) and returns
// that directory. Written beside and renamed in, so a half-written version is never run.
func writePackage(dir string, pkg *runtimepkg.Package) (string, error) {
	final := filepath.Join(dir, pkg.Hash)
	if st, err := os.Stat(final); err == nil && st.IsDir() {
		return final, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".write-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for _, f := range pkg.Files {
		if !runtimepkg.ValidPath(f.Path) {
			return "", fmt.Errorf("bad path %q in package %s", f.Path, pkg.Name)
		}
		p := filepath.Join(tmp, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, f.Data, 0o600); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, final); err != nil && !dirExists(final) {
		return "", err
	}
	// Older versions go; the one written now stays.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() && e.Name() != pkg.Hash && !strings.HasPrefix(e.Name(), ".") {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
	return final, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// logLines hands each line of r to f (long lines cut), until r ends. A line longer than the buffer is cut too and
// its rest dropped, and the lines after it still come: nothing else reads r, and a program whose log nobody reads
// is stuck on its next write once the pipe is full (#3959).
func logLines(r io.Reader, f func(string)) {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		b, err := br.ReadSlice('\n')
		line := strings.TrimRight(string(b), "\r\n") // copied: the next read reuses b
		long := errors.Is(err, bufio.ErrBufferFull)
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = br.ReadSlice('\n')
		}
		if len(b) > 0 {
			if long || len(line) > 2000 {
				line = line[:min(len(line), 2000)] + "…"
			}
			f(line)
		}
		if err != nil {
			return
		}
	}
}
