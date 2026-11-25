import { t } from "../i18n";
import { repoOf } from "./agent-repos";
import { backgroundWaiting, stateOf } from "./format";
import { interruptedAgent, todoStatus, type SessionLookup, type Todo, type TodoStatus } from "./todos";
import { type WaitReason } from "./todo-wait-reason";
import { usageLimitAgent } from "./usage-limit";
import type { Session } from "./types";
import { stateName } from "./state-labels";
import type { DirectionTaskState } from "./direction-tree";


/** The one-word state every node of the tree shows (#2526): "Needs you" before "Working" before "Waiting in the
 *  background" (#6045) before "Done". */
export type TreeState = "needs_you" | "working" | "bg_waiting" | "waiting" | "done";

const TREE_RANK: Record<TreeState, number> = { needs_you: 0, working: 1, bg_waiting: 2, waiting: 3, done: 4 };

export function treeStateLabel(s: TreeState): string {
  return stateName(s);
}

/** A branch with nothing going on whose newest activity is older than this folds by default. */
export const STALE_MS = 24 * 60 * 60 * 1000;

export interface TreeAgent {
  key: string;
  session: Session;
  state: TreeState;
  /** The agents it spawned for the same todo (todo_agents.source "spawned", via this one). */
  children: TreeAgent[];
  folded: boolean;
}

export interface TreeTodo {
  key: string;
  todoId?: string;
  name: string;
  status?: TodoStatus;
  /** A to-do's state in the To-dos page's word (todoLook), said in place of `state`'s, so the two pages agree
   *  (#6458: a to-do waiting on CI read "Done" here and "Waiting" there). `state` still ranks, folds and colors it, but
   *  for a waiting one's word (#11090): attention on the owner, neutral otherwise (`wait`). */
  statusLabel?: string;
  /** What it waits on while its status is Waiting (#11090, lib/todo-wait-reason.ts waitOf). */
  wait?: WaitReason;
  state: TreeState;
  /** A task of the direction tree (#4352): its state as the hub decided it, shown in place of `state`'s word. */
  taskState?: DirectionTaskState;
  agents: TreeAgent[];
  sessions: Session[];
  folded: boolean;
}

export function agentTreeState(s: Session): TreeState {
  const st = stateOf(s);
  if (usageLimitAgent(s)) return "waiting"; // a limit resetting is a wait, not work (#1758)
  if (st === "needs_you" || interruptedAgent(s)) return "needs_you";
  if (s.starting) return "working";
  if (backgroundWaiting(s)) return "bg_waiting";
  if (st === "working") return "working";
  return "done";
}

/** A todo's state in the tree from its status; a working one whose agents only wait in the background (none works, one
 *  waits) is "Waiting in the background" (#6045), and so is one the To-dos page calls Waiting (its agents ended their
 *  turns and something brings one back, #3274): under way, never "Done" (#6458). */
export function todoTreeState(status: TodoStatus, agents: readonly Session[] = []): TreeState {
  if (status === "needs_you" || status === "interrupted") return "needs_you";
  if (status === "usage_limit") return "waiting";
  if (status === "waiting") return "bg_waiting";
  if (status === "working") {
    const states = agents.map(agentTreeState);
    return states.includes("bg_waiting") && !states.includes("working") ? "bg_waiting" : "working";
  }
  return "done";
}

function worst(states: TreeState[]): TreeState {
  return states.reduce<TreeState>((a, b) => (TREE_RANK[b] < TREE_RANK[a] ? b : a), "done");
}

function latest(sessions: Session[]): number {
  let at = 0;
  for (const s of sessions) at = Math.max(at, Date.parse(s.updated_at ?? "") || 0, Date.parse(s.state_since ?? "") || 0);
  return at;
}

/** The project a set of working directories belongs to: the repository most of them sit in ("By repository"'s rule, merged across computers). */
export function projectOf(cwds: string[]): string {
  const count = new Map<string, number>();
  for (const cwd of cwds) {
    const name = repoOf(cwd).name;
    if (name) count.set(name, (count.get(name) ?? 0) + 1);
  }
  let best = "";
  let n = 0;
  for (const [name, c] of count) if (c > n || (c === n && name.localeCompare(best) < 0)) [best, n] = [name, c];
  return best;
}

/** A todo's project from its active agents (the live session's cwd, else the cwd the link recorded); "" for none. */
function projectOfTodo(todo: Todo, lookup?: SessionLookup): string {
  const cwds = (todo.agents ?? [])
    .filter((a) => !a.released_at)
    .map((a) => lookup?.(a.machine_id, a.session_id)?.cwd || a.cwd || "");
  return projectOf(cwds);
}

/** The todos page's list by project (#2526): open todos under the project their agents work in, the ones with no agent
 *  in one group last. Each group says "Needs you" / "Working" / "Done" and starts folded when every todo in it is done-state and quiet
 *  for a day. Todos keep the order they come in (sortTodos). */
export interface TodoProjectGroup {
  key: string;
  name: string;
  state: TreeState;
  todos: Todo[];
  folded: boolean;
  /** A knowledge-base project (an area of work, #2581): open to-dos the user keeps there, so it never folds for being
   *  quiet, and "Done" (no agent at work) is not said of it. */
  area?: boolean;
}

/** A todo's agents that are still on it, as the lookup has them. */
function liveAgents(td: Todo, lookup: SessionLookup): Session[] {
  return (td.agents ?? []).filter((a) => !a.released_at).map((a) => lookup(a.machine_id, a.session_id)).filter((s): s is Session => Boolean(s));
}

/** Open to-dos by project (#2526, #2581): the project a to-do was put in first,
 *  else the repository its agents work in; those with neither last ("Unsorted"). Groups that need the user come
 *  first within each of the three; the to-dos keep their order. */
export function groupTodosByProject(
  todos: Todo[],
  lookup: SessionLookup,
  now = Date.now(),
  /** A to-do the To-dos page has in Needs you (its status section): its folder says Needs you whatever its agents do,
   *  so a failed start waiting on the owner (which the hub closed) does not read Done or fold away. */
  waiting: (td: Todo) => boolean = () => false,
): TodoProjectGroup[] {
  const map = new Map<string, { name: string; tier: number; list: Todo[] }>();
  for (const td of todos) {
    const repo = td.project ? "" : projectOfTodo(td, lookup);
    const [key, name, tier] = td.project
      ? [`todos-kb:${td.project}`, td.project, 0]
      : repo
        ? [`todos-project:${repo}`, repo, 1]
        : ["todos-project:\u0000", t("lib-todo-tree.unsorted"), 2];
    const g = map.get(key) ?? { name, tier, list: [] };
    g.list.push(td);
    map.set(key, g);
  }
  const groups = [...map.entries()].map(([key, { name, tier, list }]) => {
    const states = list.map((td): TreeState => (waiting(td) ? "needs_you" : todoTreeState(todoStatus(td, lookup), liveAgents(td, lookup))));
    const state = worst(states);
    const sessions = list.flatMap((td) => (td.agents ?? []).filter((a) => !a.released_at).map((a) => lookup(a.machine_id, a.session_id)).filter((s): s is Session => Boolean(s)));
    const touched = Math.max(latest(sessions), ...list.map((td) => Date.parse(td.updated_at) || 0));
    const area = tier === 0;
    return { group: { key, name, state, todos: list, folded: !area && state === "done" && now - touched > STALE_MS, ...(area && { area }) }, tier };
  });
  return groups.sort((a, b) => a.tier - b.tier || TREE_RANK[a.group.state] - TREE_RANK[b.group.state]).map((g) => g.group);
}
