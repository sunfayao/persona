import { credentialOwner } from "./api";
import { currentHosts, ENGINE_HOSTS } from "./engines";
import type { SpaceLike } from "./agent-filter";
import { defaultManager, shownManager } from "./manager-choice";
import { sessionWorkspace } from "./agent-repos";
import { normalizePath } from "./files";
import type { Machine, Session } from "./types";
import { workspaceTarget, type HostWorkspace, type WorkspaceCatalogSnapshot } from "./workspace-catalog";
import { directoryKey } from "./workspace-directory";
import type { WorkspaceTarget } from "./workspace-target";

export const worktreeActionKey = (target: WorkspaceTarget) => JSON.stringify([
  target.owner, target.manager, target.machineId, target.host, target.workspaceId, target.cwd,
]);

function sameDesktopRoot(a: string, b: string): boolean {
  const key = directoryKey(a);
  return key !== null && key === directoryKey(b);
}
const desktopProject = (host: string) => host === "codex_app" || host === "claude_desktop";

/** Hosts that make worktrees of a workspace: herdr and Orca, and Paseo of its Git workspaces, where Paseo makes them itself
 *  (docs/integrations/paseo.md, "Capabilities"); a Paseo folder has none. */
const makesWorktrees = (host: string, workspace?: HostWorkspace) =>
  ["herdr", "orca"].includes(host) || host === "paseo" && workspace?.workspace_kind === "git";

/** The catalog row a target names and whether its host can take a new worktree now. Resolve the host-owned identity
 *  first. A desktop project's root directory distinguishes its catalog rows. */
function resolveWorktreeSource(target: WorkspaceTarget, catalog: WorkspaceCatalogSnapshot, machines: readonly Machine[]) {
  const owned = Boolean(target.owner && target.manager && target.owner === credentialOwner()
    && target.manager === shownManager() && catalog.owner === target.owner && catalog.manager === target.manager);
  const matches = owned ? catalog.workspaces.filter((w) => w.machine_id === target.machineId
    && w.source === target.host && w.source_id === target.workspaceId && w.source !== "session-directory"
    && (!desktopProject(target.host) || sameDesktopRoot(w.cwd, target.cwd))) : [];
  const workspace = matches.length === 1 && (desktopProject(target.host) || matches[0].cwd === target.cwd) ? matches[0] : undefined;
  const computers = machines.filter((m) => m.id === target.machineId && (m.manager || defaultManager()) === target.manager);
  const machine = computers.length === 1 ? computers[0] : undefined;
  const available = Boolean(workspace && machine?.online && workspace.online !== false && workspace.available !== false
    && currentHosts(machine).hosts.includes(target.host) && !machine.engines?.some((e) => e.id === target.host && !e.available));
  return { workspace, available };
}
/** Creation sources for a logical space. Runtime folders suggest sources for the reviewed creation dialog only;
 *  they never establish workspace ownership for closing, renaming or deletion. */
export function spaceWorktreeSources(space: SpaceLike, catalog: WorkspaceCatalogSnapshot, machines: readonly Machine[]) {
  const sessions = space.sessions.filter((s) => s.machine_id === space.machineId && (!s.manager || s.manager === catalog.manager));
  const bound = new Set(sessions.map((s) => sessionWorkspace(s, catalog.workspaces, catalog.manager)));
  const path = (cwd: string) => normalizePath(cwd.replaceAll("\\", "/"));
  const folders = new Set(sessions.filter((s) => s.source_kind === "runtime" && !s.workspace_id
    && !ENGINE_HOSTS.includes(s.space_kind || s.host || ""))
    .map((s) => path(s.worktree || s.workspace_cwd || s.cwd || "")).filter(Boolean));
  return catalog.workspaces.filter((w) => w.machine_id === space.machineId && (bound.has(w) || folders.has(path(w.cwd))))
    .map((w) => workspaceTarget(w, catalog.owner, catalog.manager))
    .filter((target) => {
      const source = resolveWorktreeSource(target, catalog, machines);
      return source.available && makesWorktrees(target.host, source.workspace);
    });
}

/** A Paseo space (its project) is renamed in Paseo: offered while the catalog has the project's workspaces from Paseo on
 *  that computer, which a bridge that renames them lists (docs/integrations/paseo.md "Spaces and workspaces"). */
export function renamesRuntimeSpace(s: Session | undefined, catalog: WorkspaceCatalogSnapshot): boolean {
  if (!s?.space_id || s.source_kind !== "runtime" || (s.space_kind || s.host) !== "paseo") return false;
  if (s.manager && s.manager !== catalog.manager) return false;
  return catalog.workspaces.some((w) => w.machine_id === s.machine_id && w.source === "paseo" && w.project_id === s.space_id);
}
