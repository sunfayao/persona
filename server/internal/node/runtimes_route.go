package node

import (
	"context"
	"slices"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Where a start's launcher runs: the Antigravity companion a start names (host agy), a runtime that offers the
// launcher, or a terminal (runtimeOf).

// runtimeOfIn resolves an explicitly selected AGY companion before the default lookup.
func (n *Node) runtimeOfIn(ctx context.Context, name string) *runtimeConn {
	if assistantLauncherPurpose(ctx) {
		return n.assistantRuntime(name)
	}
	if name == agyAgent && n.startKind(ctx) == agyAgent {
		runtimes := n.runtimes
		if runtimes == nil {
			return nil
		}
		runtimes.mu.Lock()
		defer runtimes.mu.Unlock()
		return runtimes.byRuntime(agyAgent)
	}
	return n.runtimeOf(name)
}

// agyRuntimeConnected: a start naming host agy has a companion to go to (runtimeOfIn's), a connected agy runtime.
func (n *Node) agyRuntimeConnected() bool {
	runtimes := n.runtimes
	if runtimes == nil {
		return false
	}
	runtimes.mu.Lock()
	defer runtimes.mu.Unlock()
	return runtimes.byRuntime(agyAgent) != nil
}

// usesAgyPane keeps explicitly targeted herdr starts in their pane. With herdr as the default, a
// discovered agy CLI also takes precedence over the companion for Agent starts and model discovery.
func (n *Node) usesAgyPane(ctx context.Context, name, agentType string) bool {
	kind := n.startKind(ctx)
	if assistantLauncherPurpose(ctx) || kind != host.Herdr ||
		n.agentKindOf(name, agentType) != agyAgent {
		return false
	}
	if named, _ := ctx.Value(startHostKey{}).(string); named != "" {
		return true
	}
	list, err := n.launchers(ctx)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(list, func(l proto.Launcher) bool {
		return l.Name == name || (l.Name == agyAgent && l.Agent == agyAgent)
	})
}

// runtimeLauncherModelsIn is runtimeLauncherModels for a start in ctx (runtimeOfIn): the advertisement of the runtime
// that runs the launcher there; false when none does.
func (n *Node) runtimeLauncherModelsIn(ctx context.Context, name string) (proto.LauncherModels, bool) {
	return launcherModelsOf(n.runtimeOfIn(ctx, name), name)
}

// agentKindOf is the kind of agent launcher name starts: what the caller said (the hub's word, from the form or a
// task), else a launcher kept in Persona's agent, else the login shell's word for it, else what its name says
// (launcherAgentType).
func (n *Node) agentKindOf(name, given string) string {
	if given != "" {
		return given
	}
	if m, ok := n.managedLauncher(name); ok {
		return m.AgentType()
	}
	launcherCache.Lock()
	defer launcherCache.Unlock()
	for _, l := range launcherCache.list {
		if l.Name == name && l.Agent != "" {
			return l.Agent
		}
	}
	return launcherAgentType(name)
}
