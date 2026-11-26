package ports

import (
	"encoding/binary"
	"net"
)

// The rows of GetExtendedTcpTable's TCP_TABLE_OWNER_PID_* tables: MIB_TCPROW_OWNER_PID (six DWORDs) and
// MIB_TCP6ROW_OWNER_PID (two 16-byte addresses, six DWORDs). Both tables are a DWORD count followed by the rows.
const (
	winTCPRow4Size = 24
	winTCPRow6Size = 56
	winTCPListen   = 2 // MIB_TCP_STATE_LISTEN
)

// parseWinTCP4 reads a MIB_TCPTABLE_OWNER_PID: each row is state, local address, local port, remote address, remote
// port, owning pid. The address is in network order as it lies in memory, and so are the port's low two bytes.
func parseWinTCP4(b []byte) []listener {
	return parseWinTCP(b, winTCPRow4Size, func(r []byte) (state uint32, l listener) {
		l = listener{pid: int(binary.LittleEndian.Uint32(r[20:])), port: int(r[8])<<8 | int(r[9]), addr: net.IP(r[4:8]).String()}
		return binary.LittleEndian.Uint32(r[0:]), l
	})
}

// parseWinTCP6 reads a MIB_TCP6TABLE_OWNER_PID: each row is local address (16 bytes), local scope id, local port,
// remote address, remote scope id, remote port, state, owning pid. The scope id is dropped: a link-local listener is
// dialed on loopback like any other (dialTargets).
func parseWinTCP6(b []byte) []listener {
	return parseWinTCP(b, winTCPRow6Size, func(r []byte) (state uint32, l listener) {
		l = listener{pid: int(binary.LittleEndian.Uint32(r[52:])), port: int(r[20])<<8 | int(r[21]), addr: net.IP(r[0:16]).String()}
		return binary.LittleEndian.Uint32(r[48:]), l
	})
}

// parseWinTCP reads a table's rows, keeping the listening ones; a count larger than the bytes given stops at the
// last whole row.
func parseWinTCP(b []byte, size int, row func([]byte) (uint32, listener)) []listener {
	if len(b) < 4 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(b))
	var ls []listener
	for i := 0; i < n; i++ {
		off := 4 + i*size
		if off+size > len(b) {
			break
		}
		state, l := row(b[off : off+size])
		if state != winTCPListen || l.port <= 0 {
			continue
		}
		ls = append(ls, l)
	}
	return ls
}

// paneListeners are the listeners scanListeners counts on Windows, of all of them: a process's that a root's tree owns
// (owners), and a process's whose environment names a pane being scanned (panesOf, the panes its engine variables
// name), which it adds to owners. Git Bash loses the tree: MSYS2 emulates fork, and for `cd x && cmd` the forked copy
// that runs cmd as a new Windows process exits, so cmd's parent pid names a process that is gone and nothing under it
// is under the agent any more (#9815). What the pane's terminal set in its environment (ORCA_PANE_KEY, HERDR_PANE_ID…)
// is still there. A pane's agent itself stays out, as from the tree: its own sockets (IDE bridges, MCP) are not the
// agent's servers. panesOf is asked once per process, only for one that has a listener and no owner; nil asks nothing.
func paneListeners(all []listener, owners map[int]string, roots map[string][]int, panesOf func(pid int) []string) []listener {
	var out []listener
	asked := map[int]bool{}
	for _, l := range all {
		if _, ok := owners[l.pid]; !ok && !asked[l.pid] && panesOf != nil {
			asked[l.pid] = true
			for _, pane := range panesOf(l.pid) {
				if pids, ok := roots[pane]; ok && (len(pids) == 0 || pids[0] != l.pid) {
					owners[l.pid] = pane
					break
				}
			}
		}
		if _, ok := owners[l.pid]; ok {
			out = append(out, l)
		}
	}
	return out
}
