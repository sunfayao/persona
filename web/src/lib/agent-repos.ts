import { baseName, pathSlashes } from "./format";
import type { Session } from "./types";
import { type HostWorkspace } from "./workspace-catalog";
import { sessionHost } from "./agent-groups";
import { directoryKey } from "./workspace-directory";

/** Where a session's cwd sits: the repository it belongs to and the working tree inside it. */
export type Repo = { repo: string; name: string; worktree: string; main: boolean };

// The layouts a working tree commonly lives in (herdr, Claude Code, a home-level collection): the parts before the
// last one name the repository, everything from there on is the tree.
const WORKTREE_SEGMENTS = [["/.worktrees/"], ["/.claude/worktrees/"], ["/.herdr/worktrees/"]];

const trimmed = (value?: string) => {
  const path = pathSlashes(value ?? "");
  if (/^[a-z]:\/+$/i.test(path)) return `${path.slice(0, 2)}/`;
  return path.replace(/\/+$/, "") || (path.startsWith("/") ? "/" : "");
};
// Keep display spelling, but compare Windows directory identities without case or separator differences.
const pathKey = (value?: string) => {
  const path = trimmed(value);
  return /^(?:[a-z]:\/|\/\/)/i.test(path) ? path.toLowerCase() : path;
};
const withinPath = (path: string, root: string) => path === root || path.startsWith(root.endsWith("/") ? root : `${root}/`);

/**
 * The repository and working tree a cwd belongs to. The host may name the repository (the session's repo_root, #2840:
 * Paseo keeps its worktrees under its own folder): a cwd inside it is its main checkout, any other is one of its
 * working trees. Otherwise from the path alone: a tree under one of the known layouts belongs to the
 * directory above that marker; any other path is its own repository's main checkout.
 */
export function repoOf(cwd: string, repoRoot?: string): Repo {
  if (!cwd) return { repo: "", name: "", worktree: "", main: true };
  const path = trimmed(cwd);
  const root = trimmed(repoRoot);
  if (root) {
    const inside = withinPath(pathKey(path), pathKey(root));
    return { repo: root, name: baseName(root), worktree: inside ? root : path, main: inside };
  }
  for (const [marker] of WORKTREE_SEGMENTS) {
    // Search the display path itself: Unicode lowercasing can change string length and invalidate slice offsets.
    const at = /^(?:[a-z]:\/|\/\/)/i.test(path)
      ? path.search(new RegExp(marker.replaceAll(".", "\\."), "i"))
      : path.indexOf(marker);
    if (at <= 0) continue;
    const rest = path.slice(at + marker.length).split("/").filter(Boolean);
    const repo = path.slice(0, at);
    // ~/.worktrees/<repo>/<tree>: the marker sits at the home level, so the repository is the first part after it and
    // the tree is what follows.
    if (rest.length >= 2) return { repo: `${repo}${marker}${rest[0]}`, name: rest[0]!, worktree: path, main: false };
    return { repo, name: baseName(repo), worktree: path, main: false };
  }
  return { repo: path, name: baseName(path), worktree: path, main: true };
}

/** How many folders below the top of its Git working tree a session runs, when the node says. The top is its reported
 *  tree, else the main checkout for a cwd inside repo_root, else the cwd itself (0: a linked tree kept outside the
 *  repository, such as a desktop app's worktree folder). Git reports physical paths, so this counts from the
 *  node-verified physical cwd when there is one; a count, unlike a path, also holds for the cwd as given when a
 *  symlinked folder leads to it. Undefined without Git metadata. */
function gitTreeDepth(s: Pick<Session, "repo_root" | "worktree" | "cwd" | "workspace_cwd">): number | undefined {
  const tree = catalogPath(s.worktree) || catalogPath(s.repo_root);
  if (!tree) return undefined;
  const cwd = catalogPath(s.workspace_cwd) || catalogPath(s.cwd);
  return cwd && within(cwd, tree) ? depthOf(cwd) - depthOf(tree) : 0;
}

/** Exact runtime placement always wins. Directory inference is allowed only for an unbound Agent and a unique
 *  deepest containing directory, inside its Git working tree when the node reports one: a folder above that tree (the
 *  home folder, a folder of repositories) holds it on disk but is not where it works. Host-owned bindings never fall
 *  through to a different runtime at the same path. */
export function sessionWorkspace(s: Session, catalog: readonly HostWorkspace[], manager?: string): HostWorkspace | undefined {
  if (manager && s.manager && s.manager !== manager) return undefined;
  let local = catalog.filter((w) => w.machine_id === s.machine_id);
  const host = s.space_kind || s.host || sessionHost(s);
  const bound = s.workspace_id || s.space_id;
  let inferredProjectRoot: string | undefined;
  if (bound) {
    const exact = local.filter((w) => w.source_id === bound && (!host || w.source === host));
    if (exact.length === 1) return exact[0];
    if (exact.length > 1) {
      // An explicit desktop project may own several roots. Only those roots can resolve its current directory or Git root.
      return ["codex_app", "claude_desktop"].includes(host) ? directoryWorkspace([s.cwd, s.workspace_cwd, s.repo_root], exact, true) : undefined;
    }
    // Desktop project ids describe an editor project, not a terminal workspace binding.
    if (s.workspace_id || !["codex_app", "claude_desktop"].includes(host)) return undefined;
    const directoryPrefixes = host === "codex_app" ? ["codex-project:"] : ["codex-project:", "claude-project:"];
    const directoryPrefix = directoryPrefixes.find((prefix) => bound.startsWith(prefix));
    const directoryRoot = directoryPrefix ? bound.slice(directoryPrefix.length) : "";
    if (directoryKey(directoryRoot) !== null) inferredProjectRoot = directoryRoot;
    // An unavailable saved project may use a generic directory, never another host's workspace at the same path.
    // Older, directory-derived desktop placement has no saved identity and can still resolve a unique matching project.
    if (!inferredProjectRoot) local = local.filter((w) => w.source === "directory");
  }
  if (["herdr", "herdr-core", "botmux", "orca", "paseo"].includes(host)) return undefined;
  const depth = gitTreeDepth(s);
  return directoryWorkspace([s.cwd, s.workspace_cwd, inferredProjectRoot], local, false, [depth, depth, depth === undefined ? undefined : 0]);
}
function directoryWorkspace(paths: readonly (string | undefined)[], workspaces: readonly HostWorkspace[], boundProject = false, depths?: readonly (number | undefined)[]): HostWorkspace | undefined {
  for (const [index, raw] of paths.entries()) {
    const cwd = catalogPath(raw);
    if (!cwd) continue;
    const depth = depths?.[index];
    const candidates = workspaces.filter((w) => {
      const path = catalogPath(w.cwd);
      if (!path || !within(cwd, path)) return false;
      if (boundProject) return true;
      if (depth !== undefined && depthOf(cwd) - depthOf(path) > depth) return false;
      const below = cwd === path ? "" : cwd.slice(path.length + (path.endsWith("/") ? 0 : 1));
      return !WORKTREE_SEGMENTS.some(([marker]) => below.startsWith(marker.slice(1)));
    });
    const length = Math.max(0, ...candidates.map((w) => catalogPath(w.cwd).length));
    const deepest = candidates.filter((w) => catalogPath(w.cwd).length === length);
    if (deepest.length === 1) return deepest[0];
    if (deepest.length > 1) return undefined;
  }
  return undefined;
}

const catalogPath = (value?: string) => directoryKey(value ?? "") ?? pathKey(value);
/** Whether `path` is `dir` or below it, both already normalized by catalogPath. */
const within = withinPath;
/** How many folders deep a path normalized by catalogPath is: 0 at the root. */
const depthOf = (path: string) => (path === "/" ? 0 : path.split("/").length - 1);
