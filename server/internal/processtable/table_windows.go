package processtable

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// winProcs is the process table from a Toolhelp snapshot, with each process's creation time where this user may
// read it (winProcTable uses it to drop a parent link that a reused pid would fake).
func winProcs() (map[int]Process, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	var entries []winEntry
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		entries = append(entries, winEntry{pid: int(e.ProcessID), ppid: int(e.ParentProcessID),
			name: windows.UTF16ToString(e.ExeFile[:]), start: processStart(e.ProcessID)})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	return winProcTable(entries), nil
}

// processStart is a process's creation time, zero when it cannot be opened (another user's, a protected one).
func processStart(pid uint32) time.Time {
	if pid == 0 {
		return time.Time{}
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return time.Time{}
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return time.Time{}
	}
	return time.Unix(0, created.Nanoseconds())
}
