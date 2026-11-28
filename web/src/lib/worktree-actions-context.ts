import { credentialOwner } from "./api";
import type { SpaceLike } from "./agent-filter";
import { defaultManager, shownManager } from "./manager-choice";
import { sessionWorkspace } from "./agent-repos";
import { normalizePath } from "./files";
import type { Machine } from "./types";
import { workspaceTarget, type WorkspaceCatalogSnapshot } from "./workspace-catalog";
import type { WorkspaceTarget } from "./workspace-target";

export const worktreeActionKey = (target: WorkspaceTarget) => JSON.stringify([
  target.owner, target.manager, target.machineId, target.host, target.workspaceId, target.cwd,
]);

/** Whether a target can take a new worktree now: a herdr workspace (the one host that makes worktrees) the catalog lists
 *  exactly as named, on an online computer whose herdr is there. Resolve the host-owned identity first. */
function takesWorktree(target: WorkspaceTarget, catalog: WorkspaceCatalogSnapshot, machines: readonly Machine[]): boolean {
  if (target.host !== "herdr") return false;
  const owned = Boolean(target.owner && target.manager && target.owner === credentialOwner()
    && target.manager === shownManager() && catalog.owner === target.owner && catalog.manager === target.manager);
  const matches = owned ? catalog.workspaces.filter((w) => w.machine_id === target.machineId
    && w.source === target.host && w.source_id === target.workspaceId) : [];
  const workspace = matches.length === 1 && matches[0].cwd === target.cwd ? matches[0] : undefined;
  const computers = machines.filter((m) => m.id === target.machineId && (m.manager || defaultManager()) === target.manager);
  const machine = computers.length === 1 ? computers[0] : undefined;
  return Boolean(workspace && machine?.online && workspace.online !== false && workspace.available !== false
    && !machine.engines?.some((e) => e.id === "herdr" && !e.available));
}
/** Creation sources for a logical space. Runtime folders suggest sources for the reviewed creation dialog only;
 *  they never establish workspace ownership for closing, renaming or deletion. */
export function spaceWorktreeSources(space: SpaceLike, catalog: WorkspaceCatalogSnapshot, machines: readonly Machine[]) {
  const sessions = space.sessions.filter((s) => s.machine_id === space.machineId && (!s.manager || s.manager === catalog.manager));
  const bound = new Set(sessions.map((s) => sessionWorkspace(s, catalog.workspaces, catalog.manager)));
  const path = (cwd: string) => normalizePath(cwd.replaceAll("\\", "/"));
  const folders = new Set(sessions.filter((s) => s.source_kind === "runtime" && !s.workspace_id
    && (s.space_kind || s.host) !== "herdr")
    .map((s) => path(s.worktree || s.workspace_cwd || s.cwd || "")).filter(Boolean));
  return catalog.workspaces.filter((w) => w.machine_id === space.machineId && (bound.has(w) || folders.has(path(w.cwd))))
    .map((w) => workspaceTarget(w, catalog.owner, catalog.manager))
    .filter((target) => takesWorktree(target, catalog, machines));
}
