// What each leader conversation's direction is called (docs/assistant/unified-entry.md "Leader conversations"): a leader
// conversation's page header shows that name, not the conversation's title. The names come from the hub's direction tree
// (GET /api/v1/assistant/directions/tree, #4352): each current direction in the leaders' order with its name and its
// `counts` (Waiting on you is shown, #4331). The tree is read and held in one place, lib/direction-tree.ts (the To-dos
// page and the Agents list draw the same one); this module only takes the names from it whenever it changes. The hub
// sends `directions` whenever the tree changes (a leader made or ended anywhere, a name, a task's state), and the page
// reads the tree again then (readDirectionTree).
import { useSyncExternalStore } from "react";
import { directionTreeNow, normalizeExternalDirection, readDirectionTreeOnce, refreshDirectionTree, resetDirectionTree, subscribeDirectionTree, type DirectionTree, type ExternalDirection } from "./direction-tree";

/** The tree's directions as leaders: the secretary group left out. */
export function treeLeaders(tree: DirectionTree | undefined): LeaderInfo[] {
  return (tree?.directions ?? []).filter((d) => d.id && !d.secretary).map((d) => ({ id: d.id, name: d.name, ...normalizeExternalDirection(d), counts: { running: d.running, needs_you: d.counts.needs_you } }));
}

export interface LeaderInfo {
  id: string;
  title?: string;
  name?: string;
  project?: string;
  ended?: boolean;
  /** An outside Agent (#8236), with no Persona conversation. The pair is optional and may be incomplete. */
  external?: boolean;
  machine_id?: string;
  session_id?: string;
  counts?: LeaderCounts;
}

/** What a direction shows of its tasks (#6626): whether anything under it runs (the tree's `running`) and how many of
 *  its open tasks wait on you. No working count: the mark says it. */
export interface LeaderCounts {
  running: boolean;
  needs_you: number;
}

/** The names of the current leaders, by conversation id (ended ones and blank names left out). */
export function directionNames(list: readonly LeaderInfo[] | undefined): Map<string, string> {
  const out = new Map<string, string>();
  for (const l of list ?? []) {
    const name = l.name?.trim();
    if (l.id && name && !l.ended) out.set(l.id, name);
  }
  return out;
}

/** The current external directions, by their own opaque direction id (never by the outside session id). */
export function leaderExternalDirections(list: readonly LeaderInfo[] | undefined): Map<string, ExternalDirection> {
  const out = new Map<string, ExternalDirection>();
  for (const l of list ?? []) {
    const external = normalizeExternalDirection(l);
    if (l.id && !l.ended && external) out.set(l.id, external);
  }
  return out;
}

/** What a direction says at its end: "等你 N" while tasks wait on you, else "". What runs under it is the activity mark
 *  alone, never words as well (#6626: one meaning, one expression). */
export function countsLabel(c: Pick<LeaderCounts, "needs_you"> | undefined, t: (key: string, o: { count: number }) => string): string {
  return c && c.needs_you > 0 ? t("sidebar.leader_needs_you", { count: c.needs_you }) : "";
}

const listeners = new Set<() => void>();
let names: Map<string, string> = new Map();
let externals: Map<string, ExternalDirection> = new Map();

function set(list: readonly LeaderInfo[] | undefined) {
  names = directionNames(list);
  externals = leaderExternalDirections(list);
  listeners.forEach((l) => l());
}

/** The current leaders with their names, as the tree had them: taken at once. Returns the leaders' ids in order. */
export function applyLeadersEvent(list: readonly LeaderInfo[] | undefined): string[] {
  set(list);
  return (list ?? []).map((l) => l.id).filter(Boolean);
}

/** The tree's leaders, every time the tree changes (lib/direction-tree.ts reads it, for every screen at once). */
function followTree() {
  const t = directionTreeNow();
  if (t) applyLeadersEvent(treeLeaders(t));
}
subscribeDirectionTree(followTree);

/** Forget the names (another login, tests). */
export function resetDirectionNames() {
  names = new Map();
  externals = new Map();
  resetDirectionTree();
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => void listeners.delete(l);
};

/** The direction name of one conversation, or undefined when it leads none (or the names are not in yet): what its page's
 *  header calls a leader conversation. Reads what the tree gave, asks for nothing itself. */
export function useDirectionName(id: string): string | undefined {
  const name = () => names.get(id);
  return useSyncExternalStore(subscribe, name, name);
}

/** A permanent external id is already fully described by the tree, not a missing conversation to reread forever. */
export const isExternalDirection = (id: string): boolean => externals.has(id);

/** Reads the tree (on every hello, `since` the socket opened: a read sent since then serves; with `changed`, on
 *  `directions`, once more after any read out): the leaders' ids in order, or null when it could not be read (an older
 *  hub, a failure), in which case nothing changes. The names are already taken from it by then (followTree). */
export async function readDirectionTree(changed = false, since?: number): Promise<string[] | null> {
  const t = await (changed ? refreshDirectionTree() : readDirectionTreeOnce(since));
  return t ? treeLeaders(t).map((l) => l.id) : null;
}
