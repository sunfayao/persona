package processtable

import (
	"strconv"
	"strings"
	"testing"
)

// ps -Ao pid=,ppid=,tty=,comm= on macOS: daemons have no terminal (??); comm is a path.
func TestParsePs(t *testing.T) {
	t.Parallel()
	procs := parsePs(`    1     0 ??       /sbin/launchd
  900     1 ttys003  -zsh
  950   900 ttys003  /Users/me/.local/bin/claude
  960   950 ttys003  /opt/homebrew/bin/node
  970     1 ttys003  /usr/bin/python3
  980     1 ??       /usr/libexec/some daemon
`)
	if p := procs[960]; p.PPID != 950 || p.TTY != "ttys003" || p.Command != "node" {
		t.Fatalf("960 = %+v", p)
	}
	if procs[1].TTY != "" || procs[980].Command != "some daemon" {
		t.Fatalf("launchd %+v, daemon %+v", procs[1], procs[980])
	}
}

// parseStatFields is the strings.Fields parser parseStat replaced, kept as its reference.
func parseStatFields(s string) (Process, bool) {
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return Process{}, false
	}
	f := strings.Fields(s[end+1:])
	if len(f) < 5 {
		return Process{}, false
	}
	ppid, err := strconv.Atoi(f[1])
	if err != nil {
		return Process{}, false
	}
	tty := f[4]
	if tty == "0" {
		tty = ""
	}
	return Process{PPID: ppid, TTY: tty, Command: s[open+1 : end]}, true
}

// /Process/<pid>/stat on Linux: comm may hold spaces and parentheses; tty_nr 0 is no terminal.
// The byte parser reads every line the way the strings.Fields one did, odd names and broken lines included.
func TestParseStatBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line string
		want Process
		ok   bool
	}{
		{"4242 (node) S 4200 4242 4100 34817 4242 4194304\n", Process{PPID: 4200, TTY: "34817", Command: "node"}, true},
		{"7 (my server) R 1 7 7 0 -1\n", Process{PPID: 1, Command: "my server"}, true},
		{"8 (a) S 1 (b) S 2 8 8 34818 -1", Process{PPID: 2, TTY: "34818", Command: "a) S 1 (b"}, true},
		{"9 ((sd-pam)) S 1 9 9 0 -1", Process{PPID: 1, Command: "(sd-pam)"}, true},
		{"10 () Z 3 10 10 0", Process{PPID: 3, Command: ""}, true},
		{"11 (tab\tname)\tS\t5\t11 11  \t34816\n", Process{PPID: 5, TTY: "34816", Command: "tab\tname"}, true},
		{"12 (x)S 6 12 12 00", Process{PPID: 6, TTY: "00", Command: "x"}, true},
		{"13 (x) S +6 13 13 0 -1 extra fields 1 2 3 4 5 6 7 8 9", Process{PPID: 6, Command: "x"}, true},
		{"14 (x) S 6 14 14", Process{}, false},             // truncated before tty_nr
		{"15 (x) S 6 15 15 \n", Process{}, false},          // tty_nr missing, trailing space
		{"16 (x) S notapid 16 16 0", Process{}, false},     // ppid not a number
		{"17 (unterminated S 1 17 17 0", Process{}, false}, // no ')'
		{"18 x) S 1 18 18 0", Process{}, false},            // no '('
		{"19 )x( S 1 19 19 0", Process{}, false},           // ')' before '('
		{"", Process{}, false},
	} {
		got, ok := parseStat([]byte(tc.line))
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseStat(%q) = %+v %v, want %+v %v", tc.line, got, ok, tc.want, tc.ok)
		}
		if ref, refOK := parseStatFields(tc.line); got != ref || ok != refOK {
			t.Errorf("parseStat(%q) = %+v %v, the Fields parser %+v %v", tc.line, got, ok, ref, refOK)
		}
	}
}

// The agent's processes: what runs under it, and what runs on its terminal after its parent exited.
func TestOwned(t *testing.T) {
	t.Parallel()
	procs := map[int]Process{
		1:   {PPID: 0},
		900: {PPID: 1, TTY: "ttys003"},   // the pane's shell
		950: {PPID: 900, TTY: "ttys003"}, // claude
		955: {PPID: 950},                 // a Bash tool's shell, no terminal
		960: {PPID: 955},                 // npm run dev
		961: {PPID: 960},                 // vite
		970: {PPID: 1, TTY: "ttys003"},   // `python -m http.server &` whose shell exited
		980: {PPID: 1, TTY: "ttys004"},   // another terminal
		990: {PPID: 1},                   // a daemon
	}
	got := Owned([]int{950}, procs)
	for _, pid := range []int{950, 955, 960, 961, 970, 900} {
		if !got[pid] {
			t.Errorf("%d should be the agent's", pid)
		}
	}
	for _, pid := range []int{1, 980, 990} {
		if got[pid] {
			t.Errorf("%d is not the agent's", pid)
		}
	}
}
