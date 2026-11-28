import { t } from "../i18n";
import { baseName, byAttention, stateOf } from "./format";
import { hubWaits } from "./work";
import type { Machine, Session } from "./types";
import { byTime } from "./times";

import { unopened } from "./seen-outbox";
import { usageLimitLabel } from "./usage-limit";

// the host kinds a pane ref may be prefixed with (keep in step with Kinds in server/internal/host/host.go); herdr's ids go bare and may hold a colon
const HOST_KINDS = ["botmux"];

/** The host tool a pane belongs to, from its ref (`botmux:…`; anything else is herdr's); "" for none. */
export function paneHost(pane: string | undefined): string {
  if (!pane) return "";
  const kind = pane.slice(0, pane.indexOf(":"));
  return HOST_KINDS.includes(kind) ? kind : "herdr";
}

/** The host tool a session lives in, from its pane ref; "" for none. */
export function sessionHost(s: Session): string {
  return paneHost(s.pane_id);
}

/** Where an agent was opened (#2526): "feishu" for a botmux session (a Feishu group's topic), "terminal" for a pane of a
 *  terminal host, "desktop" for a desktop app's own chat (a Codex App thread); undefined for the rest (other runtimes,
 *  history no host lists), which carry no mark. */
export type SessionSource = "feishu" | "terminal" | "desktop";

export function sessionSource(s: Session): SessionSource | undefined {
  if (s.space_kind === "codex_app") return "desktop";
  const host = sessionHost(s);
  if (host === "botmux") return "feishu";
  return host ? "terminal" : undefined;
}

/** A source's name: its mark's tooltip and what a screen reader says for it. */
export function sourceName(source: SessionSource): string {
  return t(`lib-agent-groups.source_${source}`);
}

export interface AgentMutes {
  spaces: string[];
  sessions: string[];
  allowed?: string[];
}


export function splitAgentSessions(all: Session[], machines: Machine[], keepOffline?: (s: Session) => boolean) {
  const online = new Set(machines.filter((m) => m.online).map((m) => m.id));
  const agents = all.filter((s) => !s.assistant);
  const live = agents.filter((s) => (stateOf(s) !== "offline" && online.has(s.machine_id)) || keepOffline?.(s));
  const listed = new Set(live);
  const recent = agents
    .filter((s) => !listed.has(s))
    .sort((a, b) => byTime(b.updated_at, a.updated_at) || a.machine_id.localeCompare(b.machine_id) || a.session_id.localeCompare(b.session_id))
    .slice(0, 40);
  return { live, recent };
}

export interface Space {
  key: string;
  machineId: string;
  name: string;
  sessions: Session[];
}

/** Live agents by herdr space, in herdr's order (machine, workspace, tab); agents outside any space are `loose`. The
 * sidebar keeps that order: rows that jump around as agents change state are hard to click. */
export function buildSpaces(live: Session[], machines: Machine[]): { spaces: Space[]; loose: Session[] } {
  const map = new Map<string, Space>();
  const loose: Session[] = [];
  for (const s of live) {
    if (!s.space_id) {
      loose.push(s);
      continue;
    }
    const key = `${s.machine_id}/${s.space_id}`;
    if (!map.has(key)) map.set(key, { key, machineId: s.machine_id, name: s.space_name || s.space_id, sessions: [] });
    map.get(key)!.sessions.push(s);
  }
  // The full list is also used by the sidebar when agents are pinned. Use the
  // verified cwd to recognize directory-named spaces without changing custom spaces.
  const physical = new Map<string, Map<string, string>>();
  for (const s of live) {
    if (!s.workspace_cwd || !s.cwd) continue;
    let known = physical.get(s.machine_id);
    if (!known) physical.set(s.machine_id, known = new Map());
    const before = known.get(s.cwd);
    known.set(s.cwd, before !== undefined && before !== s.workspace_cwd ? "" : s.workspace_cwd);
  }
  const folders = new Map<string, Space>();
  for (const sp of map.values()) {
    const aliases = physical.get(sp.machineId);
    const paths = new Set(sp.sessions.map((s) => s.workspace_cwd || aliases?.get(s.cwd) || s.cwd).filter((p) => baseName(p) === sp.name));
    if (paths.size !== 1) continue;
    const key = JSON.stringify([sp.machineId, ...paths]);
    const first = folders.get(key);
    if (first) {
      first.sessions.push(...sp.sessions);
      map.delete(sp.key);
    } else folders.set(key, sp);
  }
  const order = (m: string) => machines.findIndex((x) => x.id === m);
  const bySlot = (a: Session, b: Session) => (a.slot_index ?? 0) - (b.slot_index ?? 0) || a.session_id.localeCompare(b.session_id);
  const spaces = [...map.values()].map((sp) => ({ ...sp, index: sp.sessions[0]?.space_index ?? 0, sessions: sp.sessions.sort(bySlot) }));
  spaces.sort((a, b) => order(a.machineId) - order(b.machineId) || a.index - b.index || a.name.localeCompare(b.name));
  return { spaces: spaces.map(({ index: _index, ...sp }) => sp), loose: loose.sort(byAttention) };
}

export function isWaiting(s: Session): boolean {
  const waiting = typeof s.waiting === "boolean" ? s.waiting
    : Boolean(s.loop || s.background?.length || s.subagents?.some((a) => a.running) || s.workflows?.some((w) => w.running) || hubWaits(s.work));
  return ["idle", "unknown"].includes(stateOf(s)) && waiting;
}

const NO_MUTES: AgentMutes = { spaces: [], sessions: [] };

/** Awaiting Reply (#5096, docs/protocol/agent-states.md): seen, but it stopped on a question to the user nobody has answered yet
 *  (`awaits_reply`), and not muted. Such an agent is not quiet: it never folds into Idle Spaces. */
export function awaitsReply(s: Session, mutes: AgentMutes = NO_MUTES): boolean {
  return stateOf(s) === "idle" && Boolean(s.awaits_reply) && !isMuted(s, mutes);
}

/** Just finished (#9751, docs/protocol/agent-states.md): seen as far as its state goes, but the hub's triage found its turn
 *  done before anybody looked, less than three hours ago (`work.just_finished`), and not muted. Such an agent is not
 *  quiet: it never folds into Idle Spaces. */
export function justFinished(s: Session, mutes: AgentMutes = NO_MUTES): boolean {
  return stateOf(s) === "idle" && Boolean(s.work?.just_finished) && !isMuted(s, mutes);
}

/** Finished, not opened (#14447, docs/protocol/agent-states.md): seen as far as its state goes, but its last turn ended
 *  after the owner last looked (lib/seen-outbox.ts unopened), however long ago and whatever the triage concluded; not
 *  stopped by a usage limit (it goes on by itself, and its row says when) and not muted. Its row keeps Just finished's
 *  mark until it is opened (Just finished's own word for the first three hours). Such an agent is not quiet, so its
 *  space stays out of Idle Spaces; it ranks and counts as Seen. */
export function finishedUnopened(s: Session, mutes: AgentMutes = NO_MUTES): boolean {
  return unopened(s) && !usageLimitLabel(s) && !isMuted(s, mutes);
}

/** What an Awaiting Reply agent asks: what the triage says it waits for (blocked on the user), else the question its last reply
 *  put to the user; "" when neither says. */
export function replyAsk(s: Session): string {
  const w = s.work;
  if (w?.phase === "blocked" && w.on === "user" && w.line?.trim()) return w.line.trim();
  return s.asks?.trim() ?? "";
}

/** Nothing to look at: seen (or unknown), not waiting on the hub or itself, not waiting for the user's reply, not
 *  just finished, and not finished since the owner last opened it. */
export function isQuiet(s: Session, mutes: AgentMutes = NO_MUTES): boolean {
  return ["idle", "unknown"].includes(stateOf(s)) && !isWaiting(s) && !awaitsReply(s, mutes) && !justFinished(s, mutes) && !finishedUnopened(s, mutes);
}

function isMuted(s: Session, mutes: AgentMutes): boolean {
  const key = `${s.machine_id}/${s.session_id}`;
  if (mutes.sessions.includes(key)) return true;
  if (mutes.allowed?.includes(key)) return false;
  return Boolean(s.space_name && mutes.spaces.includes(`${s.machine_id}/${s.space_name}`));
}
