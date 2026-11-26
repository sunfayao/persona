package node

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/sunfayao/persona/server/internal/pathutil"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/shellcmd"
)

// memoryReaders are the commands readsOnlyMemory lets through, with the flags that would make one write or run
// something (sed -i / w, rg --pre…): refused.
var memoryReaders = map[string][]string{"cat": nil, "head": nil, "tail": {"-f", "--follow", "-F"}, "ls": nil, "wc": nil, "nl": nil,
	"grep": nil, "rg": {"--pre", "--pre-glob"}, "sed": {"-i", "--in-place"}}

// readsOnlyMemory: a command that only reads, and only the memory directory (cat / head / rg … on paths in it, a
// timeout in front, output to /dev/null at most). What it reads is what the "Memory" page shows anyway.
func readsOnlyMemory(command, cwd, dir string) bool {
	if dir == "" {
		return false
	}
	segs, ok := shellcmd.Split(command)
	if !ok || len(segs) == 0 {
		return false
	}
	touched := false
	for _, sg := range segs {
		w := sg.Words
		if len(w) > 2 && w[0] == "timeout" && timeoutArg.MatchString(w[1]) {
			w = w[2:]
		}
		if len(w) == 0 || sg.Subst || !safeRedirect(sg.Redirect) || strings.Contains(sg.Redirect, "/tmp/") {
			return false
		}
		bad, known := memoryReaders[w[0]]
		if !known {
			return false
		}
		for _, a := range w[1:] {
			for _, b := range bad {
				if a == b || strings.HasPrefix(a, b+"=") || (b == "-i" && strings.HasPrefix(a, "-i")) {
					return false
				}
			}
			if w[0] == "sed" && !strings.HasPrefix(a, "-") && strings.ContainsAny(a, "wWeE") && !strings.Contains(a, "/") {
				return false // a sed script that writes (w file) or runs (e) something
			}
			if strings.Contains(a, "/") && !strings.HasPrefix(a, "-") {
				if p := a; !allUnder([]string{p}, cwd, dir) && realPath(filepath.Clean(p)) != realPath(dir) {
					return false
				}
				touched = true
			}
		}
	}
	return touched
}

// allUnder: there are paths, and each (relative ones from cwd) is inside dir.
func allUnder(paths []string, cwd, dir string) bool {
	if dir == "" || len(paths) == 0 {
		return false
	}
	dir = realPath(dir)
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		rel, err := filepath.Rel(dir, realPath(filepath.Clean(p)))
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || rel == "." {
			return false
		}
	}
	return true
}

// codexMemoryPrompt tells Codex about the assistant's memory: the same directory and format as Claude Code's
// auto-memory for the assistant's directory (memory.go), so both agents read and write one memory and the app's
// "Memory" page shows it whichever runs. The index (MEMORY.md) goes in as it is now; the files are read when needed.
func codexMemoryPrompt(dir string) string {
	m := prompts.CodexMemory{Dir: dir}
	index, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if len(index) > 24*1024 {
		m.Index, m.Truncated = strings.TrimLeftFunc(string(index[:24*1024]), unicode.IsSpace), true
	} else {
		m.Index = strings.TrimSpace(string(index))
	}
	return prompts.RenderCodexMemory(m)
}

func realPath(p string) string { return pathutil.Real(p) }
