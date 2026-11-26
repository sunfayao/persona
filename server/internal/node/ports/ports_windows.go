package ports

import "github.com/nwverse/persona/server/internal/portprobe"

// Port discovery on Windows: no ps, no lsof, no /proc. The process tree comes from a Toolhelp
// snapshot (see processtable.List), the listening sockets and their owners from iphlpapi's GetExtendedTcpTable; no child
// process is started.

const (
	afInet  = 2
	afInet6 = 23
)

// winListeners are the listening TCP sockets, IPv4 and IPv6, of the processes owners has and of those whose
// environment names one of roots' panes (paneListeners, which adds those to owners).
func winListeners(owners map[int]string, roots map[string][]int, panesOf func(pid int) []string) ([]listener, error) {
	var all []listener
	for _, t := range []struct {
		af    uint32
		parse func([]byte) []listener
	}{{afInet, parseWinTCP4}, {afInet6, parseWinTCP6}} {
		b, err := portprobe.TCPListenerTable(t.af)
		if err != nil {
			return nil, err
		}
		all = append(all, t.parse(b)...)
	}
	return paneListeners(all, owners, roots, panesOf), nil
}
