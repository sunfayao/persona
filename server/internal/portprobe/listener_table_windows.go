package portprobe

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER
	maxTCPTable              = 16 << 20
)

// TCPListenerTable is GetExtendedTcpTable's listener table for one address family, grown until it fits (a socket opened
// between the size query and the read makes it larger).
func TCPListenerTable(af uint32) ([]byte, error) {
	size := uint32(16 << 10)
	for {
		b := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0,
			uintptr(af), tcpTableOwnerPIDListener, 0)
		switch windows.Errno(r) {
		case 0:
			return b[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			if size > maxTCPTable {
				return nil, fmt.Errorf("GetExtendedTcpTable: table of %d bytes", size)
			}
			size += 4 << 10
		default:
			return nil, fmt.Errorf("GetExtendedTcpTable: %w", windows.Errno(r))
		}
	}
}
