package node

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantskills"
	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/node/slashcommands"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/shellcmd"
)

// sessionSkillCatalog derives the execution environment from the target, never from client paths or launchers.
// A catalog read does not join, resume or start a conversation.
func (n *Node) sessionSkillCatalog(ctx context.Context, owner, id string) (assistantskills.Catalog, error) {
	unavailable := func() (assistantskills.Catalog, error) {
		return assistantskills.Catalog{}, skillSelectionError(assistantskills.ErrReference)
	}
	if owner == "" || id == "" || n.isAssistantSession(id) || n.runtimeAssistant(id) || n.guestOf(id) != nil {
		return unavailable()
	}
	if c, t, err := n.codexOwner(id); err == nil {
		if !filepath.IsAbs(t.cwd) {
			return unavailable()
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var out codexSkillList
		if err := c.wire.Call(ctx, "skills/list", map[string]any{"cwds": []string{t.cwd}, "forceReload": true}, &out); err != nil {
			return unavailable()
		}
		entries, complete, err := n.codexSkillEntries(t.cwd, out)
		if err != nil {
			return unavailable()
		}
		// The exact connection (and its actual CODEX_HOME) is the execution boundary. Never probe another launcher.
		scope, _ := json.Marshal([]string{owner, n.machineID, id, t.cwd, c.spec.Home, c.generation})
		return assistantskills.Catalog{Version: 1, Complete: complete, Skills: assistantskills.Bind(string(scope), entries)}, nil
	}
	a, live := n.agent(id)
	if !live || a.Agent != "claude" || !filepath.IsAbs(a.Cwd) || n.pendingSwitch(id) != nil {
		return unavailable()
	}
	h, _, err := n.paneHost(a.PaneID)
	if err != nil || h.Kind() != host.Herdr {
		return unavailable() // an external runtime needs its own catalog and delivery contract
	}
	st, _ := n.state.get(id)
	launcher := st.Launcher
	if launcher == "" {
		launcher = n.inferLauncher(id)
	}
	if launcher == "" {
		return unavailable()
	}
	catalog, err := n.skillCatalog(ctx, skillCatalogRequest{Owner: owner, Machine: n.machineID, SessionID: id,
		Cwd: a.Cwd, Launcher: launcher, Scope: id})
	if err != nil {
		return unavailable()
	}
	return catalog, nil
}

func (n *Node) sessionSelectedSkills(ctx context.Context, id, text string) ([]assistantskills.Entry, error) {
	refs, err := assistantskills.References(text)
	if err != nil {
		return nil, skillSelectionError(err)
	}
	if len(refs) == 0 {
		return nil, nil
	}
	owner, _ := ctx.Value(skillOwnerKey{}).(string)
	catalog, err := n.sessionSkillCatalog(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	selected, err := assistantskills.Resolve(text, catalog.Skills)
	if err != nil {
		return nil, skillSelectionError(err)
	}
	return selected, nil
}

func selectedSkillInstructions(entries []assistantskills.Entry) string {
	skills := make([]prompts.AssistantSelectedSkill, len(entries))
	for i, e := range entries {
		skills[i] = prompts.AssistantSelectedSkill{Name: e.Name, Path: e.Path, Ref: e.ID}
	}
	return prompts.RenderAssistantSelectedSkills(skills)
}

func init() { nodeRPC(devicewire.SlashCommandsMethod, (*Node).listSlashCommands) }

func (n *Node) listSlashCommands(ctx context.Context, q proto.SlashCommandQuery) (proto.SlashCommandCatalog, error) {
	out := proto.SlashCommandCatalog{Commands: []proto.SlashCommand{}}
	target, err := n.slashTarget(ctx, q)
	if err != nil {
		return out, err
	}
	if target.Agent == "claude" {
		probe, err := n.keptProbe(ctx, target.Launcher)
		if err == nil && slices.Contains(probe.Args, "--disable-slash-commands") {
			return out, nil
		}
	}
	out.Commands = n.slashCatalog.List(ctx, target, func(ctx context.Context, args ...string) (*exec.Cmd, error) {
		return n.slashProbeCommand(ctx, target, args)
	}, func(ctx context.Context) []proto.SlashCommand {
		return n.slashLocalCommands(ctx, target)
	})
	return out, nil
}

func (n *Node) slashTarget(ctx context.Context, q proto.SlashCommandQuery) (slashcommands.Target, error) {
	bad := slashcommands.Target{}
	var cwd, launcher, agent, profile string
	if q.SessionID != "" {
		if q.Cwd != "" || q.Launcher != "" || len(q.SessionID) > 256 {
			return bad, slashcommands.ErrTarget
		}
		id := n.resolveAlias(q.SessionID)
		if n.codexBlocked(id) || n.isAssistantSession(id) || n.runtimeAssistant(id) || n.guestOf(id) != nil || n.pendingSwitch(id) != nil {
			return bad, slashcommands.ErrTarget
		}
		n.mu.Lock()
		row, live := n.agents[id]
		n.mu.Unlock()
		if !live || host.ParsePane(row.PaneID).Kind != host.Herdr {
			return bad, slashcommands.ErrTarget
		}
		st, _ := n.state.get(id)
		cwd, agent, launcher, profile = row.Cwd, row.Agent, st.Launcher, st.ConfigDir
		if agent == "claude" || agent == "" {
			agent = "claude"
			if run, ok := n.sessionEnv(id); ok && run.ConfigDir != "" {
				profile = run.ConfigDir
			}
			if launcher == "" {
				launcher = n.inferLauncher(id)
			}
		}
		if launcher == "" {
			launcher = agent
		}
	} else {
		cwd, launcher = q.Cwd, q.Launcher
		if !shellcmd.NameRe.MatchString(launcher) || isDesktopLauncher(launcher) || isCodexAppLauncher(launcher) ||
			!filepath.IsAbs(cwd) || len(cwd) > 4096 {
			return bad, slashcommands.ErrTarget
		}
		switch launcher {
		case "claude", "codex", agyAgent, traexAgent:
			agent = launcher
		default:
			if !n.knownLauncher(ctx, launcher) {
				return bad, slashcommands.ErrTarget
			}
			agent = n.agentKindOf(launcher, "")
		}
	}
	switch agent {
	case "claude", "codex", agyAgent, traexAgent:
	default:
		return bad, slashcommands.ErrTarget
	}
	if !filepath.IsAbs(cwd) || !shellcmd.NameRe.MatchString(launcher) || n.cwdBlocked(cwd) {
		return bad, slashcommands.ErrTarget
	}
	cwd, info, err := n.statSafe(cwd)
	if err != nil || !info.IsDir() || n.cwdBlocked(cwd) {
		return bad, slashcommands.ErrTarget
	}
	if agent == "claude" {
		switch profile {
		case "default":
			profile = filepath.Join(n.home, ".claude")
		case "":
			if probe, err := n.keptProbe(ctx, launcher); err == nil {
				profile = proto.ExpandHome(probe.Env["CLAUDE_CONFIG_DIR"], n.home)
			}
			if profile == "" {
				if start := n.claudeStartDir.Load(); launcher == "claude" && start != nil {
					profile = proto.ExpandHome(*start, n.home)
				}
			}
			if profile == "" {
				profile = n.defaultClaudeDir()
			}
		}
	}
	return slashcommands.Target{Agent: agent, Launcher: launcher, Cwd: cwd, Profile: profile,
		Fingerprint: n.executionFingerprint(launcher, modelExecution{})}, nil
}

func (n *Node) slashProbeCommand(ctx context.Context, target slashcommands.Target, args []string) (*exec.Cmd, error) {
	shell, err := interactiveShell()
	if err != nil {
		return nil, err
	}
	launcher := n.launcherForScript(target.Launcher)
	if target.Agent == "claude" && target.Profile != "" {
		if target.Launcher == "claude" {
			launcher = "claude"
		}
		launcher = "CLAUDE_CONFIG_DIR=" + scriptPath(target.Profile) + " " + launcher
	}
	cmd := exec.CommandContext(ctx, shell, append([]string{"-ic", launcher + ` "$@"`, "persona"}, args...)...)
	cmd.Dir = target.Cwd
	cmd.Env = withoutEnv(cmd.Environ(), []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID", "PERSONA_AGENT"})
	killGroupOnCancel(cmd)
	return cmd, nil
}

func (n *Node) slashLocalCommands(ctx context.Context, target slashcommands.Target) []proto.SlashCommand {
	if target.Agent != "claude" {
		return nil
	}
	skills, err := slashcommands.ClaudeSkills(target.Cwd, target.Profile, n.home)
	if err != nil {
		return nil
	}
	var own map[string]any
	if path := n.launcherOwnSettings(ctx, target.Launcher); path != "" {
		own, err = readLauncherSettings(path, target.Cwd, n.home)
		if err != nil {
			return nil
		}
	}
	skills, err = visibleClaudeSkills(skills, target.Profile, target.Cwd,
		map[string]any{"skillOverrides": n.userSkillsOff(target.Profile, target.Cwd)}, own)
	if err != nil {
		return nil
	}
	rows := make([]proto.SlashCommand, 0, len(skills))
	for _, skill := range skills {
		rows = append(rows, proto.SlashCommand{Name: skill.Name, Description: skill.Description, Source: proto.SlashSkill})
	}
	return append(rows, slashcommands.ClaudeCommands(target.Cwd, target.Profile, n.home)...)
}
