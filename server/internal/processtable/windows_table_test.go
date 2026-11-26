package processtable

import (
	"testing"
	"time"
)

// winTCP4Row is a MIB_TCPROW_OWNER_PID as Windows lays it out.

func TestWinProcTable(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	procs := winProcTable([]winEntry{
		{pid: 4, ppid: 0, name: "System"},
		{pid: 900, ppid: 4, name: "bash.exe", start: t0},
		{pid: 950, ppid: 900, name: "claude.exe", start: t0.Add(time.Second)},
		{pid: 960, ppid: 950, name: "node.exe", start: t0.Add(2 * time.Second)},
		{pid: 970, ppid: 980, name: "python.exe", start: t0.Add(3 * time.Second)}, // 980 is newer: not its parent
		{pid: 980, ppid: 4, name: "notepad.exe", start: t0.Add(time.Hour)},
		{pid: 990, ppid: 900, name: "svc.exe"},      // start unknown
		{pid: 0, ppid: 0, name: "[System Process]"}, // its own parent
	})
	for pid, ppid := range map[int]int{950: 900, 960: 950, 970: 0, 990: 0, 0: 0, 4: 0} {
		if got := procs[pid].PPID; got != ppid {
			t.Errorf("parent of %d = %d, want %d", pid, got, ppid)
		}
	}
	for pid, p := range procs {
		if p.TTY != "" {
			t.Errorf("%d has a terminal %q", pid, p.TTY)
		}
	}
	if procs[960].Command != "node.exe" {
		t.Errorf("960 = %+v", procs[960])
	}
	got := Owned([]int{950}, procs)
	if !got[950] || !got[960] || got[970] || got[900] {
		t.Fatalf("owned = %v", got)
	}
}

// Git Bash breaks Windows' process tree (#9815): a dev server whose tree lost the agent is still the pane's when its
// environment names the pane, and only then. The agent's own sockets stay out, a pane not scanned gets nothing, a
// process with two sockets is read once, and without an environment reader only the tree counts.
