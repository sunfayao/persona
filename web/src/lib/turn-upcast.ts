// Single compatibility boundary: produces ItemFacets from a legacy Item row.
//
// This is the one file that knows how to read old-format transcripts (before the
// unified turn model landed on nodes). Every facet consumer calls `itemFacets(item)`
// and renders entirely from the facets object — never from the raw legacy fields.
//
// When the node emits facets directly, upcast is a no-op that passes through.
// When facets are absent, upcast synthesises the best it can from kind, text,
// tool_name, outcome, narration, source_kind, and injected/TaskNote fields.
// Unknown facets (new enums from future nodes) are passed through untouched.
//
// Rule: no agent identity switches, no i18n, no UI decisions — purely a shape mapper.

import type { Item, LoopInfo, Session, Interaction, ScreenDialog } from "./types";
import type { ItemFacets, ToolFacet, PlanFacet, SubagentFacet, TaskFacet, InteractionFacet, ArtifactFacet, NoticeFacet, ProgressFacet, InputFacet, ToolStatus, SubagentStatus, TaskStatus, InteractionStatus, NoticeSeverity, NoticeCode, ArtifactOp, ArtifactKind, TaskKind, SubagentRelation, ContextMeter } from "./turn-model-wire";
import { describeTool } from "./tool-registry";

// ── Public entry ────────────────────────────────────────────────────────────

/** Return facets for an item, synthesising them from legacy fields if missing. */
export function itemFacets(item: Item): ItemFacets {
  if (item.facets) {
    if (!item.facets.plan && item.kind === "tool_use" && item.facets.tool?.kind === "plan") {
      return { ...item.facets, plan: synthesizePlan(item) };
    }
    return item.facets;
  }
  return synthesizeFacets(item);
}

/** Select the active timer for the chat header and background panel. */
export function sessionTimer(session: Session | undefined): LoopInfo | undefined {
  const timers = session?.facets?.timers;
  if (!timers) return session?.loop;
  const task = timers.findLast((entry) => entry.status === "scheduled" || entry.status === "fired");
  if (!task) return undefined;
  return {
    kind: task.timer?.cron ? "cron" : "wakeup",
    prompt: task.timer?.prompt,
    reason: task.title,
    interval_seconds: task.timer?.interval_s,
    cron: task.timer?.cron,
    next_at: task.timer?.next_at,
    ticks: session?.loop?.ticks,
  };
}

/** Legacy CLI sentinels that mark an answer-text item as commentary or quiet
 *  heartbeat — it should not be treated as a visible reply in the transcript. */
export function isTransparentAnswer(item: Item): boolean {
  return item.facets?.progress?.phase === "commentary" || Boolean(item.narration || item.codex_heartbeat_quiet);
}

// ── Synthesis ───────────────────────────────────────────────────────────────

function synthesizeFacets(item: Item): ItemFacets {
  const facets: ItemFacets = {};

  // Native ref is optional; only set if we can tell.
  // facets.native is deliberately omitted — provenance is diagnostics, not render.

  // Input facet: "typed" unless we see markers for queued/relay/wake.
  facets.input = synthesizeInput(item);

  // Tool facet: only for tool_use and tool_result.
  if (item.kind === "tool_use" || item.kind === "tool_result") {
    facets.tool = synthesizeTool(item);
  }

  // Plan facet: only for todo_write / task_create tool calls.
  if (item.kind === "tool_use" && item.tool_name) {
    const desc = describeTool(item.tool_name);
    if (desc.kind === "plan") facets.plan = synthesizePlan(item);
  }

  // Task facet: from item.task or from background workflow markers.
  if (item.task) facets.task = synthesizeTaskFromNote(item.task, item.outcome);

  // Subagent facet: detected from narration+fork markers or known patterns.
  if (item.kind === "notice" && looksLikeSubagentNotice(item)) {
    facets.subagent = synthesizeSubagent(item);
  }

  // Interaction facet: when the item's text matches known question/permission markers.
  if (item.kind === "notice" && looksLikeInteractionNotice(item)) {
    facets.interaction = synthesizeInteraction(item);
  }

  // Artifact facet: when text names an artifact creation/update.
  if (item.kind === "notice" && looksLikeArtifactNotice(item)) {
    facets.artifact = synthesizeArtifact(item);
  }

  // Notice facet: always when kind is "notice".
  if (item.kind === "notice") {
    facets.notice = synthesizeNotice(item);
  }

  // Progress facet: for thinking items and narration markers.
  if (item.kind === "thinking") {
    facets.progress = synthesizeProgress(item);
  } else if (item.narration && item.kind === "text") {
    facets.progress = { phase: "commentary" };
  }

  return facets;
}

// ── Input facet ─────────────────────────────────────────────────────────────

function synthesizeInput(item: Item): InputFacet | undefined {
  if (item.role !== "user") return undefined;
  if (item.queued) return { via: "queued" };
  if (item.wake) return { via: "wake" };
  if (item.source?.kind === "supervision") return { via: "relay" };
  return { via: "typed" };
}

// ── Tool facet ──────────────────────────────────────────────────────────────

function synthesizeTool(item: Item): ToolFacet {
  const rawName = item.tool_name ?? "";
  const desc = describeTool(rawName);

  let status: ToolStatus | undefined;
  if (item.kind === "tool_result") {
    const raw = item.outcome?.status;
    if (raw === "background") status = "background";
    else if (raw === "timed_out") status = "interrupted";
    else if (raw === "cancelled") status = "interrupted";
    else if (item.is_error) status = "failed";
    else if (raw === "denied") status = "denied";
    else if (raw === "completed" || raw === undefined) status = "completed";
    else status = raw as ToolStatus;
  } else if (item.kind === "tool_use") {
    status = "running";
  }

  const result: ToolFacet = {
    kind: desc.kind,
    name: desc.isCommand ? "Bash" : rawName,
    title: desc.title,
  };
  if (status) result.status = status;
  return result;
}

// ── Plan facet ──────────────────────────────────────────────────────────────

function synthesizePlan(item: Item): PlanFacet | undefined {
  const input = item.tool_input ?? {};
  if (!Array.isArray(input.todos)) return undefined;
  return {
    mode: "replace",
    entries: input.todos.flatMap((entry: unknown) => {
      if (!entry || typeof entry !== "object") return [];
      const todo = entry as Record<string, unknown>;
      const title = typeof todo.content === "string" ? todo.content : typeof todo.activeForm === "string" ? todo.activeForm : "";
      if (!title) return [];
      const status = typeof todo.status === "string" ? todo.status : "pending";
      return [{ title, status, active_form: typeof todo.activeForm === "string" ? todo.activeForm : undefined }];
    }),
  };
}

// ── Task facet from legacy TaskNote ─────────────────────────────────────────

function synthesizeTaskFromNote(
  note: NonNullable<Item["task"]>,
  outcome?: Item["outcome"],
): TaskFacet {
  const kind = note.kind as TaskKind;
  const status = (note.status ?? "running") as TaskStatus;
  const result: TaskFacet = { id: note.task_id ?? note.tool_use_id ?? "", kind, status };
  if (note.title) result.title = note.title;
  if (typeof outcome?.exit_code === "number") result.exit_code = outcome.exit_code;
  if (note.output_file) result.output_file = note.output_file;
  return result;
}

// ── Subagent detection heuristics ───────────────────────────────────────────

function looksLikeSubagentNotice(item: Item): boolean {
  const text = item.text ?? "";
  return (
    /(?:(?:subagent|agent|worker|child).*?(?:spawn|started|launched|running|finished|completed)|已启动|已完成|已创建)/i.test(text) ||
    /^Agent \w+ (starting|running|completed|failed)/.test(text)
  );
}

function synthesizeSubagent(item: Item): SubagentFacet {
  const text = item.text ?? "";
  let relation: SubagentRelation = "spawn";
  let status: SubagentStatus | undefined;
  if (/result|completed|finished|result from/i.test(text)) relation = "result";
  else if (/cancel|terminate/i.test(text)) relation = "cancel";
  else if (/message to/i.test(text)) relation = "message_to";
  if (/starting/.test(text)) status = "starting";
  else if (/running/.test(text)) status = "running";
  else if (/waiting/.test(text)) status = "waiting";
  else if (/failed|error/i.test(text)) status = "failed";
  else if (/cancel/i.test(text)) status = "cancelled";
  else if (/completed|finished|result/i.test(text)) status = "completed";
  const result: SubagentFacet = { id: item.id, relation };
  if (status) result.status = status;
  return result;
}

// ── Interaction detection heuristics ─────────────────────────────────────────

function looksLikeInteractionNotice(item: Item): boolean {
  const text = item.text ?? "";
  return (
    /^(?:Ask|Question|Permission|请求|询问|批准)/i.test(text) ||
    /\?$/.test(text.trim()) ||
    /^Allow (?:this|continue|all)/i.test(text) ||
    /^[A-Z][a-z]+, (?:do you|would you|please)/.test(text)
  );
}

function synthesizeInteraction(item: Item): InteractionFacet {
  const text = item.text ?? "";
  let kind: InteractionFacet["kind"] = "question";
  let status: InteractionStatus = "pending";
  if (/permission|allow|deny|批准|允许/i.test(text)) kind = "permission";
  else if (/plan|计划/i.test(text)) kind = "plan";
  if (item.is_error) status = "denied";
  return { kind, status };
}

// ── Artifact detection heuristics ───────────────────────────────────────────

function looksLikeArtifactNotice(item: Item): boolean {
  const text = item.text ?? "";
  return /(?:Created|Updated|Deleted) (?:artifact|document|plan|checklist|file)/i.test(text);
}

function synthesizeArtifact(item: Item): ArtifactFacet {
  const text = item.text ?? "";
  let op: ArtifactOp = "created";
  if (/Updated/i.test(text)) op = "updated";
  else if (/Deleted/i.test(text)) op = "deleted";
  let kind: ArtifactKind = "file";
  if (/plan/i.test(text)) kind = "plan";
  else if (/checklist/i.test(text)) kind = "checklist";
  else if (/document/i.test(text)) kind = "document";
  else if (/visualization/i.test(text)) kind = "visualization";
  return { id: item.id, op, kind };
}

// ── Notice facet ────────────────────────────────────────────────────────────

function synthesizeNotice(item: Item): NoticeFacet {
  const text = item.text ?? "";
  let code: NoticeCode = "unknown_event";
  let severity: NoticeSeverity = "info";

  if (/interrupt|cancel|中断/i.test(text)) code = "interrupted";
  else if (/compact|compacted|压缩/i.test(text)) code = "compacted";
  else if (/api error|api 错误|model error/i.test(text)) { code = "api_error"; severity = "error"; }
  else if (/retry|retrying|重试/i.test(text)) { code = "retrying"; severity = "warning"; }
  else if (/usage limit|subscription limit/i.test(text)) { code = "usage_limit"; severity = "warning"; }
  else if (/model fallback|model switched|model_fallback/i.test(text)) code = "model_switched";
  else if (/mode switched|切换模式/i.test(text)) code = "mode_switched";
  else if (/checkpoint/i.test(text)) code = "checkpoint";
  else if (/wake|唤醒/i.test(text)) code = "wake";
  else if (/timer/i.test(text)) code = "timer_fired";
  else if (/server restart|restart/i.test(text)) code = "server_restart";
  else if (/loop/i.test(text)) code = "loop_fired";
  else if (/runtime error|运行时错误/i.test(text)) { code = "runtime_error"; severity = "error"; }

  return { code, severity };
}

// ── Progress facet ──────────────────────────────────────────────────────────

function synthesizeProgress(item: Item): ProgressFacet {
  if (item.kind === "thinking") {
    const text = item.text ?? "";
    let phase: ProgressFacet["phase"] = "thinking";
    if (/summary/i.test(text)) phase = "summary";
    return { phase };
  }
  return { phase: "commentary" };
}

// ── Interaction → InteractionFacet (from the Interaction type, not Item) ───

/** Upcast a live Interaction card to its InteractionFacet. */
export function interactionToFacet(ix: Interaction): InteractionFacet {
  const kind = ix.kind as InteractionFacet["kind"];
  const result: InteractionFacet = { kind, status: "pending" };
  if (ix.questions?.length) {
    result.questions = ix.questions.map((q) => ({
      question: q.question,
      header: q.header,
      multi_select: Boolean(q.multi_select),
      options: q.options?.map((o) => ({ label: o.label, description: (o as any).detail })) ?? [],
    }));
  }
  if (ix.plan) result.plan = ix.plan;
  return result;
}

// ── Session → SessionFacets ─────────────────────────────────────────────────

import type { SessionFacets } from "./turn-model-wire";

/** Upcast a Session to SessionFacets (partial — legacy Session lacks most facets). */
export function sessionFacets(session: Session): SessionFacets {
  const result: SessionFacets = {};
  if (session.plan) {
    result.plan = { entries: [], at: session.updated_at ?? "" };
  }
  if (session.context_tokens !== undefined || session.context_window !== undefined) {
    const ctx: ContextMeter = { used: session.context_tokens ?? 0, source: "rule" };
    if (session.context_window !== undefined) ctx.window = session.context_window;
    if (session.context_restart_recommended) ctx.auto_compact_at = ctx.used;
    result.context = ctx;
  }
  return result;
}

/** Normalize shell arguments recorded by older nodes. */
export function commandText(input: Record<string, unknown>): string {
  if (Array.isArray(input.command)) return input.command.join(" ");
  if (typeof input.command === "string") return input.command.trim();
  return typeof input.cmd === "string" ? input.cmd.trim() : "";
}

/** Older terminals identify the question policy in the revision. */
export function screenQuestionDismissible(dialog: ScreenDialog | undefined): boolean {
  return dialog?.question_dismissible ?? !(dialog?.rev?.startsWith("trae-question:") ?? false);
}

export function interruptNotice(item: Item): boolean {
  if (item.kind !== "notice" || item.task) return false;
  return item.facets?.notice ? item.facets.notice.code === "interrupted" : (item.text ?? "").includes("中断"); // i18n-ignore: legacy notice
}

export function foregroundWork(session: Session | undefined): boolean {
  if (!session?.background_working) return true;
  return session.facets?.turn?.open ?? session.agent !== "agy";
}

export function sendQueuedNow(session: Session, assistant: boolean, owned = 0): boolean {
  return session.capabilities?.queue_send_now
    ?? ((assistant || session.source_kind !== "runtime")
      && (session.agent === "claude" || session.agent === "agy" || (assistant && owned > 0)));
}

export function composerSendQueuedNow(session: Session, assistant: boolean): boolean {
  return session.capabilities?.queue_send_now
    ?? (assistant || (session.agent === "claude" && session.source_kind !== "runtime"));
}

export function editQueue(session: Session, assistant: boolean): boolean {
  return session.capabilities?.queue_edit
    ?? ((assistant || session.source_kind !== "runtime") && session.agent !== "agy");
}

export function takeoverAction(session: Session): boolean {
  const capability = session.capabilities?.takeover;
  if (capability) return !capability.released_desktop;
  return session.agent === "codex" || session.source_kind === "codex_history" || session.source_kind === "codex_app_server";
}

export function runtimeQueue(session: Session): boolean {
  return session.source_kind === "runtime";
}

export function archiveClosesSession(session: Session): boolean {
  return session.agent === "agy";
}

export function busySendWithQueue(agent: string | undefined): boolean {
  return agent === "agy";
}

export function nativePermissionReason(agent: string | undefined): boolean {
  return Boolean(agent && agent !== "claude");
}

export function engineHandoffSupported(agent: string): boolean {
  return agent === "claude" || agent === "codex";
}
