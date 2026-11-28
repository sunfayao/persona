// An exchange as one continuous answer (Claude desktop's turn): the agent's words in the order it wrote them, and
// between them each stretch of other work (tool calls, thoughts with words, background notices, folded history) as
// one quiet collapsed row that says in words what was done and opens to its steps. Nothing is replaced as the run
// goes on; the turn's total time is said once, under its finished answer (SegmentView's reply bar).

import { t } from "../i18n";
import { isChildSessionStep } from "./child-cards";
import { wakeSummary } from "./tasks";
import { toolLabel, type Step } from "./turns";
import type { Item } from "./types";
import { itemFacets } from "./turn-upcast";

export type ToolStep = Extract<Step, { kind: "tool" }>;

export type AgentStatus = "running" | "completed" | "failed" | "stopped";

/** A stretch of work (`children` hold its steps), or a step in the open (words, a compaction, a visualization). */
export type WorkNode = WorkGroupNode | { kind: "step"; step: Step };

export interface WorkGroupNode {
  kind: "group";
  /** Its first real step's key (`work-…`): stable as the stretch grows and from live to done. */
  id: string;
  /** Its first step's seq: a stretch inside a fold the reader opened starts open. */
  seq?: number;
  children: WorkNode[];
  /** The stretch still going at the end of a live exchange: the exchange's one live indicator. */
  live: boolean;
  /** Where its live clock starts (ms): the exchange's current run of work, not the stretch. */
  startedAt?: number;
}

interface WorkGroupOptions {
  /** The agent is at work on this exchange: its tail stretch is the live one. */
  live: boolean;
  /** When the exchange began (your message): the live clock counts from there, or from the latest wake. */
  turnStart?: string;
  /** Live words are being written after the steps: the tail stretch is over. */
  streaming?: boolean;
}

/** History that was never loaded, or the agent idle in between: a step further than this after the work before it
 *  starts a new run of work, and the time between is not counted. */
const IDLE_GAP_MS = 30 * 60 * 1000;

export const isAgentStep = (s: Step): boolean => s.kind === "tool" && itemFacets(s.use).tool?.kind === "agent";
export const isPlanStep = (s: Step): boolean => s.kind === "tool" && Boolean(s.result?.facets?.plan || itemFacets(s.use).plan);
/** Claude Code's Workflow call (#3434): its run's card where the hub lists the run, else the row it always was. */
export const isWorkflowStep = (s: Step): s is ToolStep => s.kind === "tool" && itemFacets(s.use).tool?.kind === "workflow";
/** A tool call drawn as an action row (not a subagent card, not a plan card, not a workflow's call). */
export const isRowStep = (s: Step): s is ToolStep => s.kind === "tool" && !s.result?.facets?.artifact && !isAgentStep(s) && !isPlanStep(s) && !isWorkflowStep(s);
const isCompactStep = (s: Step) => s.kind === "notice" && Boolean(s.item.compact);
/** A Codex visualize fragment is response content, like the words: it stands in the open where it was written. */
const isVisualizationStep = (s: Step) => s.kind === "visualization";

/** The item a step stands for (a tool: its call). */
function stepItem(s: Step): Item {
  return s.kind === "tool" ? s.use : s.item;
}

export const tsMs = (ts?: string): number | null => {
  const t = Date.parse(ts ?? "");
  return Number.isFinite(t) ? t : null;
};

function stepStart(s: Step): number | null {
  return tsMs(stepItem(s).ts);
}

/** When a step was over: a tool at its result, folded work at its last step. */
function stepEnd(s: Step): number | null {
  const start = stepStart(s);
  let end: number | null = null;
  if (s.kind === "tool") end = tsMs(s.result?.ts);
  else if (s.kind === "fold") end = tsMs(s.item.fold?.until);
  if (start === null) return end;
  return end === null ? start : Math.max(start, end);
}

const isWords = (s: Step) => s.kind === "text" && Boolean(s.item.text?.trim());
/** The notice of the agent's own background work reporting back to it (docs/protocol/protocol.md `wake`). */
const isWake = (s: Step) => s.kind === "notice" && Boolean(s.item.wake);

/** A thought without words (Claude Code writes most of them so) says nothing: it is no step of the work, so a turn of
 *  nothing else has no stretch and it never splits one (#5155). */
const isWordlessThought = (s: Step) => s.kind === "thinking" && !s.item.text?.trim();

/** The step a stretch is known by: its first real one, so a live stretch keeps its key as it grows and once it is
 *  done. A child session's card is not one: it comes in with the child's row, and the stretch keeps the key (and the
 *  reader's open state) it had without it. */
const anchorStep = (run: Step[]) => run.find((s) => s.kind !== "text" && !isChildSessionStep(s)) ?? run.find((s) => s.kind !== "text") ?? run[0];

/** A stretch's key: its anchor's seq and block (a fold's is the seq of the first step it stands for, block 0). */
function anchorKey(run: Step[]): string {
  const first = anchorStep(run);
  const it = stepItem(first);
  return `${it.seq}.${first.kind === "fold" ? 0 : (it.id.split("#")[1] ?? 0)}`;
}

function createGroup(run: Step[]): WorkGroupNode {
  const first = anchorStep(run);
  const start = stepStart(first);
  return {
    kind: "group",
    id: `work-${anchorKey(run)}`,
    seq: stepItem(first).seq,
    children: run.map((step) => ({ kind: "step", step })),
    live: false,
    ...(start !== null ? { startedAt: start } : {}),
  };
}

/** One exchange's steps (its words among them, in order) as what the chat draws: every piece of words, compaction and
 *  visualization in the open where it was written, each maximal stretch of other steps between them one group. While
 *  the agent works, the tail stretch is live (nothing written after it) and its clock is the exchange's current run. */
export function workBlocks(steps: Step[], opts: WorkGroupOptions): WorkNode[] {
  const out: WorkNode[] = [];
  let run: Step[] = [];
  const flush = () => {
    // a text without words is no boundary: it goes with the stretch around it, and alone it is drawn as nothing; a
    // wake alone (the agent's background work reporting back, then its words) is part of the answer, not a row
    if (run.some((s) => s.kind !== "text" && !isWake(s))) out.push(createGroup(run));
    run = [];
  };
  for (const s of steps) {
    if (isWordlessThought(s)) continue;
    if (isWords(s) || isCompactStep(s) || isVisualizationStep(s)) {
      flush();
      out.push({ kind: "step", step: s });
      continue;
    }
    run.push(s);
  }
  flush();
  const tail = out.at(-1);
  if (opts.live && !opts.streaming && tail?.kind === "group") {
    tail.live = true;
    const since = workTime(steps, opts.turnStart).since;
    if (since !== undefined) tail.startedAt = since;
  }
  return out;
}

/** How long the exchange worked (`ms`) and where its current run of work began (`since`, ms): from your message, or
 *  its first step without one, to the latest end of any step. A wake (the agent's own background work reporting back
 *  to an idle agent) or a step more than IDLE_GAP_MS after the work before it starts a new run; the idle time before
 *  it is not counted. A compaction is not work. A live clock counting from `since` never shows more than `ms` grows to. */
export function workTime(steps: Step[], turnStart?: string): { ms?: number; since?: number } {
  let total = 0;
  let start = tsMs(turnStart);
  let end: number | null = start;
  for (const s of steps) {
    if (isCompactStep(s)) continue;
    const at = stepStart(s);
    const woke = isWake(s);
    if (at !== null && end !== null && start !== null && (woke || at - end > IDLE_GAP_MS)) {
      total += end - start;
      start = at;
      end = at;
    }
    if (start === null) start = at;
    const e = stepEnd(s);
    if (e !== null) end = end === null ? e : Math.max(end, e);
  }
  if (start === null) return {};
  return { ms: total + Math.max(0, (end ?? start) - start), since: start };
}

/** A TodoWrite that can be its exchange's plan card: the tool took it (a refused one did not change the plan, #5602)
 *  and it lists two or more items (cindy draws no card for a list of one). The one rule for the card and the pill. */
export const isPlanList = (s: Step): s is ToolStep => isPlanStep(s) && !(s as ToolStep).result?.is_error && planTodos(s as ToolStep).length >= 2;

/** The step of an exchange that draws its plan card: its latest plan list, "" for none (cindy draws one card per plan,
 *  where its latest list was written). Its other plan updates stay in the summary as ordinary rows, each inspectable
 *  (super rule 2; cindy drops them). */
export function planCardStep(steps: Step[]): string {
  for (let i = steps.length - 1; i >= 0; i--) if (isPlanList(steps[i])) return steps[i].id;
  return "";
}

export interface Todo {
  content: string;
  status: "pending" | "in_progress" | "completed";
  activeForm?: string;
}

export function planTodos(s: ToolStep): Todo[] {
  const plan = s.result?.facets?.plan ?? itemFacets(s.use).plan;
  if (!plan) return [];
  return plan.entries.flatMap((entry): Todo[] => {
    if (!entry.title || entry.status === "cancelled") return [];
    const status = entry.status === "in_progress" ? "in_progress"
      : entry.status === "completed" || entry.status === "skipped" ? "completed" : "pending";
    return [{ content: entry.title, status, activeForm: entry.active_form }];
  });
}

/** done / total, the current step (in progress, else the next pending, else the last), and its 1-based number. */
export function planProgress(todos: Todo[]): { done: number; total: number; current: string; step: number; allDone: boolean } {
  const done = todos.filter((t) => t.status === "completed").length;
  const total = todos.length;
  const active = todos.findIndex((t) => t.status === "in_progress");
  const pending = todos.findIndex((t) => t.status === "pending");
  const idx = active >= 0 ? active : pending >= 0 ? pending : Math.max(0, total - 1);
  const allDone = total > 0 && done >= total;
  return { done, total, current: todos[idx]?.content ?? "", step: allDone ? total : idx + 1, allDone };
}

// ── what a group says and shows ──

/** The groups holding the tool call `call` (its tool_use_id), outer first, down to the one it sits in: what opens to
 *  bring it into view (a background task's "Jump to the step that started it", iOS ChatView.jumpToCall). Empty: it is in the open; null:
 *  not among these nodes. */
export function groupsHolding(nodes: WorkNode[], call: string): string[] | null {
  for (const n of nodes) {
    if (n.kind === "step") {
      if (n.step.kind === "tool" && n.step.use.tool_use_id === call) return [];
      continue;
    }
    const inner = groupsHolding(n.children, call);
    if (inner) return [n.id, ...inner];
  }
  return null;
}

/** Every nonempty stretch can be inspected, even a single running tool call. */
export const groupOpens = (g: WorkGroupNode): boolean => g.children.length > 0;

/** A Persona conversation ("assistant") or an Agent's: what a finished exchange adds under its answer differs (its
 *  changed files), never how its work is drawn. */
export type WorkLineStyle = "agents" | "assistant";
export const workLineStyle = (assistantView?: boolean, assistantSession?: boolean): WorkLineStyle => (assistantView || assistantSession ? "assistant" : "agents");

export interface WorkLineState {
  turnLive: boolean;
  remembered: boolean;
  liveOverride?: boolean;
}

/** A stretch never opens by itself. A manual choice while it runs lasts while it runs; once done it is the reader's
 *  remembered choice, closed by default. */
export const workLineOpen = (s: WorkLineState): boolean => s.turnLive ? (s.liveOverride ?? false) : s.remembered;

/** How a duration reads where it ticks (cindy's status bar options): from `0s`, and `1m 0s` rather than `1m`. */
export const TICKING_DURATION = { minimumSeconds: 0, alwaysShowRemainder: true } as const;

/** cindy's formatSessionDuration: under an hour `6s`, `1m 5s`, `2m` (the same in every language's catalog; at least a
 *  second); from an hour the language's form, always with the minutes ("1h 0m", "1 小时 0 分", "1時間0分"; "1d 2h 5m").
 *  `opts` are cindy's formatDuration options, so the running bar (TICKING_DURATION) and the summaries share one format. */
export function workDuration(msValue: number, opts: { minimumSeconds?: number; alwaysShowRemainder?: boolean } = {}): string {
  const total = Math.max(opts.minimumSeconds ?? 1, Math.round((Number.isFinite(msValue) ? msValue : 0) / 1000));
  if (total < 60) return t("lib-work-groups.dur_seconds", { s: total });
  if (total >= 3600) {
    const d = Math.floor(total / 86_400);
    const h = Math.floor((total % 86_400) / 3600);
    const m = Math.floor((total % 3600) / 60);
    return d > 0 ? t("lib-work-groups.dur_days_hours_minutes", { d, h, m }) : t("lib-work-groups.dur_hours_minutes", { h, m });
  }
  const m = Math.floor(total / 60);
  const s = total % 60;
  return s || opts.alwaysShowRemainder ? t("lib-work-groups.dur_minutes_seconds", { m, s }) : t("lib-work-groups.dur_minutes", { m });
}

/** A tool call's own elapsed time, from its call timestamp to its result's (#8163). Both are transcript timestamps
 *  rather than provider-recorded timing; null when either is missing or unparseable, or the result predates the call. */
export function toolDurationMs(s: ToolStep): number | null {
  const reported = s.result?.outcome?.duration_ms;
  if (typeof reported === "number" && Number.isFinite(reported) && reported >= 0) return reported;
  const start = tsMs(s.use.ts);
  const end = tsMs(s.result?.ts);
  return start !== null && end !== null && end >= start ? end - start : null;
}

/** Only reliable spans of at least 30 seconds warrant an action-row duration; unfinished calls do not guess. */
export function toolDurationLabel(s: ToolStep): string | null {
  const ms = toolDurationMs(s);
  if (ms === null) return null;
  if (s.result?.outcome?.duration_ms === ms) {
    return ms < 1000 ? t("lib-work-groups.dur_milliseconds", { ms }) : workDuration(ms);
  }
  return ms >= 30_000 ? workDuration(ms) : null;
}

/** A stretch's row in words: what it did by kind ("Ran 3 commands · Edited 2 files"); without tool calls, its
 *  background task's notice, that it thought, or "Work details". Live, "Working…" leads it. Never how long it took. */
export function groupTitle(g: WorkGroupNode): string {
  const words = actionSummary(g).text || quietSummary(g);
  if (!g.live) return words || t("lib-work-groups.work");
  return words ? `${t("lib-work-groups.working")} · ${words}` : t("lib-work-groups.working");
}

/** A stretch without tool calls: its first notice in one line (a background task's, never a wake's: that is no news to
 *  the reader), else that it thought. */
function quietSummary(g: WorkGroupNode): string {
  const steps = g.children.flatMap((c) => (c.kind === "step" ? [c.step] : []));
  const notice = steps.find((s): s is Extract<Step, { kind: "notice" }> => s.kind === "notice" && !isWake(s));
  if (notice) return (notice.item.task ? wakeSummary(notice.item).text : notice.item.text?.split("\n")[0]?.trim()) ?? "";
  return steps.some((s) => s.kind === "thinking") ? t("lib-work-groups.thought") : "";
}

/** Action kinds in first-seen order; file actions count distinct known paths, other kinds count calls.
 * Unknown tools keep their existing localized label. Only the first three kinds enter the compact header. */
export function actionSummary(g: WorkGroupNode): { text: string; icon: string } {
  const kinds = new Map<string, { count: number; paths: Set<string>; icon: string; key?: string; label?: string }>();
  for (const child of g.children) {
    if (child.kind !== "step" || child.step.kind !== "tool") continue;
    const use = child.step.use;
    const tool = itemFacets(use).tool;
    const name = tool?.name ?? use.tool_name ?? "";
    const kind = tool?.kind === "read" ? "read" : tool?.kind === "edit" || tool?.kind === "write" ? "edit"
      : tool?.kind === "execute" ? "run" : tool?.kind === "search" || tool?.kind === "web_search" ? "search" : name;
    let entry = kinds.get(kind);
    if (!entry) {
      const known = ["read", "edit", "run", "search"].includes(kind);
      entry = { count: 0, paths: new Set(), icon: known ? kind === "edit" ? "write" : kind : "tool", ...(known ? { key: `lib-work-groups.actions_${kind}` } : { label: toolLabel(name) }) };
      kinds.set(kind, entry);
    }
    const path = tool?.locations?.[0]?.path ?? use.tool_input?.file_path;
    if ((kind === "read" || kind === "edit") && typeof path === "string" && path) {
      if (entry.paths.has(path)) continue;
      entry.paths.add(path);
    }
    entry.count++;
  }
  const shown = [...kinds.values()].slice(0, 3);
  return { text: shown.map((k) => k.key ? t(k.key, { count: k.count }) : k.label).join(" · "), icon: shown[0]?.icon ?? "layers" };
}
