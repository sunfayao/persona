package node

import (
	"context"
	"runtime"

	"github.com/sunfayao/persona/server/internal/node/engine/orca"
	"github.com/sunfayao/persona/server/internal/nodeclient"
	"github.com/sunfayao/persona/server/internal/orcart"
)

type orcaHost = orca.Host
type orcaProc = orca.Process

const (
	errNoOrcaTerminal = orca.ErrNoTerminal
	orcaPaneEnv       = nodeclient.OrcaPaneEnv
	orcaLegacyPaneEnv = nodeclient.OrcaLegacyPaneEnv
	orcaProcsMaxAge   = orca.ProcessMaxAge
)

func orcaConfig(c *orcart.Client) orca.Config {
	return orca.Config{Client: c, Git: git, Refuse: refusal, NewID: newUUID,
		BackendLaunch: func(ctx context.Context) bool { return backendLaunchFrom(ctx) != nil }, Windows: runtime.GOOS == "windows", ScanWindows: scanOrcaProcsOS}
}

var newOrcaHost = func(c *orcart.Client) *orca.Host { return orca.New(orcaConfig(c)) }

func scanOrcaProcs() (map[string][]orca.Process, error) {
	return orca.New(orcaConfig(nil)).ScanProcesses()
}
