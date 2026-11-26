//go:build unix

package link

import (
	"syscall"

	"github.com/sunfayao/persona/server/internal/osproc"
)

func sshProcAttr() *syscall.SysProcAttr {
	return osproc.SessionGroupAttr()
}
