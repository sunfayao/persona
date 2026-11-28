package shellexec

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/proto"
)

// check is p's directory and timeout, or why the node does not run it. The node checks everything it is sent, whatever
// the caller checked before.
func (e *Executor) check(p proto.MachineExecParams) (string, time.Duration, error) {
	switch {
	case strings.TrimSpace(p.Command) == "":
		return "", 0, errors.New("machine.exec: the command is empty")
	case len(p.Command) > proto.MachineExecMaxCommand:
		return "", 0, fmt.Errorf("machine.exec: the command is longer than %d bytes", proto.MachineExecMaxCommand)
	case !utf8.ValidString(p.Command) || strings.ContainsRune(p.Command, 0) || strings.ContainsRune(p.Cwd, 0):
		return "", 0, errors.New("machine.exec: the command or cwd is not text (invalid UTF-8 or a NUL byte)")
	case len(p.Cwd) > proto.MachineExecMaxCwd:
		return "", 0, fmt.Errorf("machine.exec: cwd is longer than %d bytes", proto.MachineExecMaxCwd)
	case p.TimeoutMS <= 0 || p.TimeoutMS > proto.MachineExecMaxTimeout.Milliseconds():
		return "", 0, fmt.Errorf("machine.exec: timeout_ms must be 1 to %d", proto.MachineExecMaxTimeout.Milliseconds())
	}
	cwd := p.Cwd
	if cwd == "" {
		cwd = "~"
	}
	dir := e.path(cwd, e.d.Home)
	if dir == "" {
		return "", 0, errors.New("machine.exec: this node does not know its home directory; give cwd as an absolute path")
	}
	word := ""
	if e.d.Refused(dir) {
		word = p.Cwd
	}
	for _, w := range shellWords(p.Command) {
		if word == "" && e.d.Refused(e.path(w, dir)) {
			word = w
		}
	}
	if word != "" {
		return "", 0, fmt.Errorf("machine.exec: refused, nothing ran: %q names a file that holds Persona's credentials "+
			"or state (hub.json, manager.json, link.json, the keys beside them and the data directory's secrets), which "+
			"shell commands do not read; ask the user to look at it on that computer", word)
	}
	return dir, time.Duration(p.TimeoutMS) * time.Millisecond, nil
}

// The credential guardrail (Dependencies.Refused) reads the command's text, not what it runs: a name built while it
// runs (a variable, $(…), a glob, an encoding) is not seen. It keeps an obvious read of a credential file out of this
// tool, where nothing else does: the agent sandbox and the node's path guard keep such files from Agents and from
// what a conversation names by path, and machine.exec has neither. It is not a sandbox.

// shellWords are the words of command a path could be: split at blanks, at shell operators and at the separators of
// assignments, option values and path lists (= : ,), with quotes, backslashes and braces taken out, so "hub.json",
// h'u'b.json, hub\.json, {hub,link}.json's link.json and ${HOME} read as the names they make.
func shellWords(command string) []string {
	var words []string
	var cur strings.Builder
	for _, r := range command + " " {
		switch {
		case strings.ContainsRune(`'"\{}`, r):
		case unicode.IsSpace(r) || strings.ContainsRune(";&|()<>`=:,", r):
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	return words
}

// path is word read as a path from directory from: ~ and $HOME at its start are the home, $PERSONA_HOME the data
// directory, and any other word not absolute is under from. "" when it starts with a directory the node does not know.
func (e *Executor) path(word, from string) string {
	for _, v := range []struct{ name, dir string }{{"~", e.d.Home}, {"$HOME", e.d.Home}, {"$PERSONA_HOME", e.d.DataDir}} {
		if rest, ok := strings.CutPrefix(word, v.name); ok && (rest == "" || rest[0] == '/') {
			if v.dir == "" {
				return ""
			}
			return filepath.Clean(v.dir + rest)
		}
	}
	if filepath.IsAbs(word) {
		return filepath.Clean(word)
	}
	return filepath.Join(from, word)
}

// shell is the login shell the command runs in, non-interactive (-lc): on macOS $SHELL when it is zsh or bash, else
// /bin/zsh; elsewhere bash. A login shell reads the user's profile, which puts what they installed on PATH
// (/opt/homebrew/bin, ~/.local/bin).
func (e *Executor) shell() string {
	if e.d.GOOS != "darwin" {
		return "bash"
	}
	if sh := lookup(e.d.Env, "SHELL"); filepath.IsAbs(sh) && (filepath.Base(sh) == "zsh" || filepath.Base(sh) == "bash") {
		return sh
	}
	return "/bin/zsh"
}

// nonInteractive are set for every command, over the node's own values: no colour or cursor escapes in output meant
// for reading (TERM, NO_COLOR), no pager waiting for a key (PAGER, GIT_PAGER), and git failing at once where it would
// ask for a user name or password on a terminal the command does not have (GIT_TERMINAL_PROMPT).
var nonInteractive = []string{"TERM=dumb", "NO_COLOR=1", "PAGER=cat", "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0"}

// env is the command's environment: the node's, with nonInteractive, and with the user's own bin folders added at the
// end of PATH where they are missing, for a node started with the system's bare PATH (launchd, systemd) whose user's
// profile does not add them.
func (e *Executor) env() []string {
	out := make([]string, 0, len(e.d.Env)+len(nonInteractive)+1)
	for _, kv := range e.d.Env {
		k, _, _ := strings.Cut(kv, "=")
		if k != "PATH" && lookup(nonInteractive, k) == "" {
			out = append(out, kv)
		}
	}
	dirs := filepath.SplitList(lookup(e.d.Env, "PATH"))
	if len(dirs) == 0 {
		dirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	}
	var extra []string
	if e.d.GOOS == "darwin" {
		extra = append(extra, "/opt/homebrew/bin", "/usr/local/bin")
	}
	if e.d.Home != "" {
		extra = append(extra, filepath.Join(e.d.Home, ".local", "bin"))
	}
	for _, d := range extra {
		if !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return append(append(out, nonInteractive...), "PATH="+strings.Join(dirs, string(filepath.ListSeparator)))
}

// lookup is name's value in env ("" when it is not there).
func lookup(env []string, name string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return ""
}
