package ports

import (
	"encoding/binary"
	"net"
	"slices"
	"testing"
)

// winTCP4Row is a MIB_TCPROW_OWNER_PID as Windows lays it out.
func winTCP4Row(state uint32, ip string, port uint16, pid uint32) []byte {
	r := make([]byte, winTCPRow4Size)
	binary.LittleEndian.PutUint32(r[0:], state)
	copy(r[4:8], net.ParseIP(ip).To4())
	binary.BigEndian.PutUint16(r[8:], port) // network order in the DWORD's low bytes
	binary.LittleEndian.PutUint32(r[20:], pid)
	return r
}

// winTCP6Row is a MIB_TCP6ROW_OWNER_PID as Windows lays it out.
func winTCP6Row(state uint32, ip string, scope uint32, port uint16, pid uint32) []byte {
	r := make([]byte, winTCPRow6Size)
	copy(r[0:16], net.ParseIP(ip).To16())
	binary.LittleEndian.PutUint32(r[16:], scope)
	binary.BigEndian.PutUint16(r[20:], port)
	binary.LittleEndian.PutUint32(r[48:], state)
	binary.LittleEndian.PutUint32(r[52:], pid)
	return r
}

func winTable(rows ...[]byte) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(rows)))
	for _, r := range rows {
		b = append(b, r...)
	}
	return b
}

// GetExtendedTcpTable's IPv4 rows: listening ones only, the port read in network order, the owner's pid.
func TestParseWinTCP4(t *testing.T) {
	t.Parallel()
	b := winTable(
		winTCP4Row(winTCPListen, "127.0.0.1", 5173, 4242),
		winTCP4Row(winTCPListen, "0.0.0.0", 8000, 4300),
		winTCP4Row(5, "127.0.0.1", 50000, 4242), // ESTABLISHED
	)
	want := []listener{{pid: 4242, port: 5173, addr: "127.0.0.1"}, {pid: 4300, port: 8000, addr: "0.0.0.0"}}
	if got := parseWinTCP4(b); !slices.Equal(got, want) {
		t.Fatalf("parseWinTCP4:\n got %+v\nwant %+v", got, want)
	}
}

// IPv6 rows: loopback, every address, a link-local one (its scope id dropped).
func TestParseWinTCP6(t *testing.T) {
	t.Parallel()
	b := winTable(
		winTCP6Row(winTCPListen, "::1", 0, 3000, 4401),
		winTCP6Row(winTCPListen, "::", 0, 3000, 4401),
		winTCP6Row(winTCPListen, "fe80::1", 7, 9229, 4500),
	)
	want := []listener{{pid: 4401, port: 3000, addr: "::1"}, {pid: 4401, port: 3000, addr: "::"}, {pid: 4500, port: 9229, addr: "fe80::1"}}
	if got := parseWinTCP6(b); !slices.Equal(got, want) {
		t.Fatalf("parseWinTCP6:\n got %+v\nwant %+v", got, want)
	}
}

// A table cut short (or empty) gives the whole rows it has, never a read past the end.
func TestParseWinTCPShort(t *testing.T) {
	t.Parallel()
	b := winTable(winTCP4Row(winTCPListen, "127.0.0.1", 5173, 1), winTCP4Row(winTCPListen, "127.0.0.1", 5174, 2))
	if got := parseWinTCP4(b[:len(b)-1]); len(got) != 1 || got[0].port != 5173 {
		t.Fatalf("cut short: %+v", got)
	}
	if got := parseWinTCP4(nil); got != nil {
		t.Fatalf("empty: %+v", got)
	}
	if got := parseWinTCP6([]byte{9, 0, 0, 0}); got != nil {
		t.Fatalf("a count with no rows: %+v", got)
	}
}

// The process table from a snapshot: no terminals; a parent link only to a parent that started no later (an exited
// parent's pid taken by a newer process does not adopt its children), and none when a start is unknown.
// Git Bash breaks Windows' process tree (#9815): a dev server whose tree lost the agent is still the pane's when its
// environment names the pane, and only then. The agent's own sockets stay out, a pane not scanned gets nothing, a
// process with two sockets is read once, and without an environment reader only the tree counts.
func TestPaneListenersFindWhatTheTreeLost(t *testing.T) {
	t.Parallel()
	roots := map[string][]int{"orca:t1:l1": {100, 101}, "w1:p1": {200}}
	all := []listener{
		{pid: 101, port: 3000, addr: "127.0.0.1"}, // under the agent: the tree has it
		{pid: 300, port: 5173, addr: "127.0.0.1"}, // its parent chain broke; ORCA_PANE_KEY names t1:l1
		{pid: 300, port: 5173, addr: "::1"},
		{pid: 100, port: 41000, addr: "127.0.0.1"}, // the agent itself: its IDE bridge
		{pid: 400, port: 8080, addr: "0.0.0.0"},    // another Orca pane, not an agent's here
		{pid: 500, port: 9000, addr: "127.0.0.1"},  // no engine variables (a system service)
		{pid: 600, port: 4000, addr: "127.0.0.1"},  // herdr inside an Orca terminal: the herdr pane, the first one scanned
	}
	env := map[int][]string{100: {"orca:t1:l1"}, 300: {"orca:t1:l1"}, 400: {"orca:t9:l9"}, 600: {"w1:p1", "orca:t1:l1"}}
	asked := map[int]int{}
	panesOf := func(pid int) []string { asked[pid]++; return env[pid] }

	owners := map[int]string{101: "orca:t1:l1"}
	got := paneListeners(all, owners, roots, panesOf)
	want := []listener{all[0], all[1], all[2], all[6]}
	if !slices.Equal(got, want) {
		t.Errorf("listeners:\n got %+v\nwant %+v", got, want)
	}
	if owners[300] != "orca:t1:l1" || owners[600] != "w1:p1" {
		t.Errorf("owners = %v", owners)
	}
	for _, pid := range []int{100, 400, 500} {
		if _, ok := owners[pid]; ok {
			t.Errorf("%d counted for %s", pid, owners[pid])
		}
	}
	if asked[101] != 0 || asked[300] != 1 {
		t.Errorf("environment read %v; want none for an owned process and one per process", asked)
	}

	if got := paneListeners(all, map[int]string{101: "orca:t1:l1"}, roots, nil); !slices.Equal(got, all[:1]) {
		t.Errorf("without a reader: %+v", got)
	}
}
