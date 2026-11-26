// Sessions an Agent started (a Codex subagent, an Agent a Paseo Agent created) folded into the session that started them
// (#5120, after cindy: a child Agent is never a task of its own). The hub's manager says which listed session a child
// folds under (`folded_under`) and gives that session a rollup of their states (`children`, docs/protocol/protocol.md
// "Session hierarchy"). The web leaves the children out of every list and count, shows the parent in its folded state,
// and draws each child as a card in the parent's chat, where its requests are answered too.
import { foldedStateWith, foldHome, foldsUnder } from "./side-fold";
import type { AgentState, Interaction, Session } from "./types";

/** The hub folds child sessions and its paged list takes `roots=1` and `parent_session_id=` (docs/protocol/protocol.md
 *  "Session hierarchy"): send those keys only when hello lists this. */
export const SESSIONS_HIERARCHY_FEATURE = "sessions_hierarchy";

/** The state a parent shows and is counted under with its folded children (the hub's `Session.FoldedState`): one of
 *  them waiting on the user makes it need you; one working makes it work, unless it has a reply the user has not read or
 *  awaits the user's own; one of its side sessions unread makes it unread (#7944, lib/side-fold.ts). */
export function foldedState(s: Session): AgentState {
  return foldedStateWith(s);
}

/** A session's key in the sets and maps of child rows (`listedKeys`, the rows open chats hold). */
export const rowKey = (mid: string, sid: string) => `${mid}\u0000${sid}`;

/** The keys of the sessions a list holds, for `isFoldedIn`. */
export function listedKeys(rows: readonly Session[]): Set<string> {
  return new Set(rows.map((s) => rowKey(s.machine_id, s.session_id)));
}

/** s folds under a session the list holds on its own machine. An inactive Codex Desktop worker keeps that hierarchy
 *  even when its parent is absent from the list. A side session follows the hub's side-fold policy. */
export function isFoldedIn(s: Session, listed: ReadonlySet<string>): boolean {
  const under = foldsUnder(s);
  if (under && listed.has(rowKey(s.machine_id, under))) return true;

  const parent = under || s.root_session_id || s.parent_session_id;
  return Boolean(parent && parent !== s.session_id && s.is_subagent && s.agent === "codex" &&
    s.agent_surface === "desktop" && s.source_kind === "codex_history" &&
    (s.status === "idle" || s.status === "done" || s.status === "offline") &&
    s.state !== "needs_you" && !s.pending_interactions && !s.children?.needs_you);
}

/** The sessions folded into mid/sid's chat, oldest first (by when they started, else were last updated): what folds
 *  under it, its side sessions among them, but not the rows that belong to one of those sides' chats (`folded_side`,
 *  #7944); for a side's chat, those rows. */
export function foldedChildren(rows: readonly Session[], mid: string, sid: string): Session[] {
  const at = (s: Session) => Date.parse(s.started_at ?? s.updated_at ?? "") || 0;
  return rows.filter((s) => s.machine_id === mid && foldHome(s) === sid).sort((a, b) => at(a) - at(b));
}

/** The session a request is counted under, whose page is its home: the one its session folds under, else its own. */
export function requestHome(ix: Pick<Interaction, "session_id" | "folded_under" | "folded_as" | "folded_side">): string {
  return foldsUnder(ix) || ix.session_id;
}

/** How many of a session's folded children are working. */
export function runningChildren(s: Session): number {
  return s.children?.working ?? 0;
}
