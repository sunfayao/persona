package devices

import "github.com/sunfayao/persona/server/internal/osproc"

func proxyProcessAlive(pid int) bool {
	return osproc.Alive(pid)
}

func killProxyProcess(pid int) {
	_ = osproc.Kill(pid)
}
