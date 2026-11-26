package node

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/nwverse/persona/server/internal/node/harness/codex"
	"github.com/nwverse/persona/server/internal/proto"
)

func (n *Node) codexAppLauncherModels(ctx context.Context, refresh bool) (proto.LauncherModels, error) {
	home := n.codexHome()
	key := n.home + "\x00" + codexAppLauncher + "\x00codex\x00" + home
	launcherModelsCache.Lock()
	at, ok := launcherModelsCache.at[key]
	cached := launcherModelsCache.got[key]
	launcherModelsCache.Unlock()
	ask := func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), launcherModelsTimeout)
		defer cancel()
		var models []proto.AgentModel
		if cli, err := codexAppCLI(); err == nil && cli != "" {
			models, _ = queryCodexCLIModels(ctx, cli, home)
		}
		if len(models) == 0 {
			models = codex.ReadHomeModels(home)
		}
		if models == nil {
			models = []proto.AgentModel{}
		}
		res := proto.LauncherModels{Agent: "codex", Models: models, SupportsStartEffort: true}
		launcherModelsCache.Lock()
		if len(launcherModelsCache.got) > 64 {
			launcherModelsCache.at, launcherModelsCache.got = map[string]time.Time{}, map[string]proto.LauncherModels{}
		}
		launcherModelsCache.at[key], launcherModelsCache.got[key] = time.Now(), res
		launcherModelsCache.Unlock()
		return res, nil
	}
	if ok && !refresh {
		switch age := time.Since(at); {
		case age < launcherModelsTTL:
			return cached, nil
		case age < launcherModelsStale:
			launcherModelsCache.flight.DoChan(key, ask)
			return cached, nil
		}
	}
	v, err, _ := launcherModelsCache.flight.Do(key, ask)
	if err != nil {
		return proto.LauncherModels{}, err
	}
	return v.(proto.LauncherModels), nil
}

func queryCodexCLIModels(ctx context.Context, cli, home string) ([]proto.AgentModel, error) {
	cmd := exec.CommandContext(ctx, cli, "app-server")
	cmd.Dir = os.TempDir()
	if home != "" {
		cmd.Env = append(desktopSeedEnv(os.Environ()), "CODEX_HOME="+home)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = in.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	return codexModelList(in, out)
}
