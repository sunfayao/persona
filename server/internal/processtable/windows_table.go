package processtable

import "time"

// winEntry is one process of a Toolhelp snapshot, with its creation time (zero when this user may not open it).
type winEntry struct {
	pid, ppid int
	name      string
	start     time.Time
}

// winProcTable is scanListeners' process table from a snapshot. Windows keeps a process's parent pid after the parent
// has exited, and pids are reused: a parent link is kept only when the parent is known to have started no later than
// the child, so a process that got an exited parent's pid does not adopt its children. Windows has no controlling
// terminal: tty stays empty and what a pane owns follows the process tree alone.
func winProcTable(entries []winEntry) map[int]Process {
	start := make(map[int]time.Time, len(entries))
	for _, e := range entries {
		start[e.pid] = e.start
	}
	out := make(map[int]Process, len(entries))
	for _, e := range entries {
		ppid := e.ppid
		if ps, ok := start[ppid]; ppid == e.pid || !ok || ps.IsZero() || e.start.IsZero() || ps.After(e.start) {
			ppid = 0
		}
		out[e.pid] = Process{PPID: ppid, Command: e.name}
	}
	return out
}
