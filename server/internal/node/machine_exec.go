package node

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/sunfayao/persona/server/internal/node/shellexec"
	"github.com/sunfayao/persona/server/internal/proto"
)

// machine.exec (proto.MethodMachineExec): the main conversation's machine_exec tool, one non-interactive command in
// this user's login shell, run by the shellexec domain. A command naming this node's credential files is refused
// (execRefused).
func init() { nodeRPC(proto.MethodMachineExec, (*Node).machineExec) }

func (n *Node) machineExec(ctx context.Context, p proto.MachineExecParams) (proto.MachineExecResult, error) {
	var logf func(string, ...any)
	if n.logger != nil {
		logf = n.logger.Printf
	}
	return shellexec.New(shellexec.Dependencies{Home: n.home, DataDir: n.dir, Refused: n.execRefused, Logf: logf}).Run(ctx, p)
}

// execRefused: a machine.exec command may not name path, by the rules that keep these files from what a conversation
// names by path and from Agents: the files classifyMachineSensitivePath calls hub_config wherever they are (hub.json
// and its backups, manager.json, link.json, secret.key, hub.key, handles.key), and in a data directory what the path
// guard refuses there (dataSecret: its credential files and secret folders, which the agent sandbox closes too). It
// reads the command's text: a guardrail, not a sandbox (shellexec/command.go).
func (n *Node) execRefused(path string) bool {
	p := fold(filepath.Clean(path))
	if _, class := classifyMachineSensitivePath(p); class == "hub_config" {
		return true
	}
	return slices.ContainsFunc(n.inDataDirs(p), dataSecret)
}
