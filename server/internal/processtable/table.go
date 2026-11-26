package processtable

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Process is one process: its parent and its controlling terminal ("" when none).
type Process struct {
	PPID    int
	TTY     string
	Command string
}

// List reads the process table.
func List(ctx context.Context) (map[int]Process, error) {
	switch runtime.GOOS {
	case "linux":
		return procfsProcs("/proc")
	case "windows":
		return winProcs()
	default:
		return psProcs(ctx)
	}
}

// Owned returns the processes under roots, and those on the same terminal as a root (a server started with `&` whose
// shell has exited is no longer under it, but keeps the terminal).
func Owned(roots []int, procs map[int]Process) map[int]bool {
	children := map[int][]int{}
	for pid, p := range procs {
		children[p.PPID] = append(children[p.PPID], pid)
	}
	ttys := map[string]bool{}
	out := map[int]bool{}
	var walk func(int)
	walk = func(pid int) {
		if out[pid] {
			return
		}
		out[pid] = true
		for _, c := range children[pid] {
			walk(c)
		}
	}
	for _, r := range roots {
		if _, ok := procs[r]; !ok {
			continue
		}
		if t := procs[r].TTY; t != "" {
			ttys[t] = true
		}
		walk(r)
	}
	for pid, p := range procs {
		if p.TTY != "" && ttys[p.TTY] {
			walk(pid)
		}
	}
	return out
}

// psProcs lists processes with ps (macOS): "pid ppid tty comm".
func psProcs(ctx context.Context) (map[int]Process, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-Ao", "pid=,ppid=,tty=,comm=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parsePs(string(out)), nil
}

func parsePs(out string) map[int]Process {
	procs := map[int]Process{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		tty := f[2]
		if tty == "??" || tty == "?" || tty == "-" {
			tty = ""
		}
		cmd := ""
		if len(f) > 3 {
			cmd = filepath.Base(strings.Join(f[3:], " "))
		}
		procs[pid] = Process{PPID: ppid, TTY: tty, Command: cmd}
	}
	return procs
}

// lsofListeners: the TCP listeners of pids (macOS), from `lsof -Fpcn`: "p<pid>", "c<command>", "n<addr>:<port>".

// parseStat reads one /proc/<pid>/stat line (procfsProcs, Linux): "pid (comm) state ppid pgrp session tty_nr …". comm
// may hold spaces and parentheses, so it ends at the last ')'. It runs for every process on every sweep, so it walks
// the bytes instead of splitting the line, and copies out only one string, from comm to tty_nr, that cmd and tty share.
func parseStat(b []byte) (Process, bool) {
	open, end := bytes.IndexByte(b, '('), bytes.LastIndexByte(b, ')')
	if open < 0 || end < open {
		return Process{}, false
	}
	// The fields after comm, as [from, to) offsets in b: 0 state, 1 ppid, 2 pgrp, 3 session, 4 tty_nr.
	var from, to [5]int
	i := end + 1
	for k := range from {
		for i < len(b) && isStatSpace(b[i]) {
			i++
		}
		from[k] = i
		for i < len(b) && !isStatSpace(b[i]) {
			i++
		}
		if i == from[k] {
			return Process{}, false // a short line
		}
		to[k] = i
	}
	ppid, err := strconv.Atoi(string(b[from[1]:to[1]]))
	if err != nil {
		return Process{}, false
	}
	s := string(b[open+1 : to[4]])
	tty := s[from[4]-open-1:]
	if tty == "0" {
		tty = ""
	}
	return Process{PPID: ppid, TTY: tty, Command: s[:end-open-1]}, true
}

// isStatSpace separates the fields after comm, which the kernel writes in ASCII.
func isStatSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\v' || c == '\f'
}
