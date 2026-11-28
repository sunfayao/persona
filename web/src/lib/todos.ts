import { t } from "../i18n";
import { acceptanceStands, decisionDismissed, linkedParkInteraction, linkedParkOwnerAsk, parkInteractionID, reviewState, type Supervision } from "./supervision";
import type { LineageStep, TaskBrief } from "./brief";
import type { RuleSuggestion } from "./rule-suggestion";
import type { TodoSummary } from "./todo-view-wire";
import { api, ApiError, enc } from "./api";
import { parseAPIError, retryable, unrecoverableError } from "./api-errors";
import { stateOf } from "./format";
import { foldedState } from "./child-sessions";
import { foldRows } from "./folded-rows";
import type { AgentState, Interaction, Question, Session } from "./types";
import type { Route } from "./router";
import { byTime } from "./times";
import { stateName, waitName } from "./state-labels";
import { usageLimitAgent } from "./usage-limit";

/** How an Agent came onto the to-do; `leader`, `worker` and `review` belong to a to-do a supervisor Agent runs
 *  (lib/todo-supervisor.ts): its supervisor, a worker on the worker's own to-do, an independent reviewer. */
type TodoAgentSource = "manual" | "space" | "spawned" | "chat" | "leader" | "worker" | "review";

export interface TodoAgent {
  machine_id: string;
  session_id: string;
  linked_at: string;
  released_at?: string | null;
  dismissed_at?: string | null;
  title?: string;
  cwd?: string;
  agent?: string;
  start_error?: string;
  source?: TodoAgentSource;
  via?: string;
}

export interface TodoSpace {
  machine_id: string;
  space: string;
  linked_at: string;
}

type TodoResponsibleKind = "session" | "person";

/** A responsible party that is not a Persona-started agent (#12712). */
export interface TodoResponsible {
  kind: TodoResponsibleKind;
  name: string;
  machine_id?: string;
  session_id?: string;
}

/** The to-do's responsible party, when the hub sent a whole one (valid kind and non-empty name). */
export function responsibleOf(todo: Pick<Todo, "responsible"> | undefined): TodoResponsible | undefined {
  const r = todo?.responsible;
  if (!r) return undefined;
  if (r.kind !== "session" && r.kind !== "person") return undefined;
  const name = typeof r.name === "string" ? r.name.trim() : "";
  if (!name) return undefined;
  const machine_id = typeof r.machine_id === "string" && r.machine_id.trim() ? r.machine_id.trim() : undefined;
  const session_id = typeof r.session_id === "string" && r.session_id.trim() ? r.session_id.trim() : undefined;
  return {
    kind: r.kind,
    name,
    ...(machine_id ? { machine_id } : {}),
    ...(session_id ? { session_id } : {}),
  };
}

/** Private image metadata on a to-do; bytes are read separately from `url`. */
export interface TodoImage {
  id: string;
  mime: string;
  width: number;
  height: number;
  url: string;
  name?: string;
  size?: number;
}

/** What the hub last read of a PR on GitHub (pr_status.go). */
export interface PRFacts { url: string; state: "open" | "merged" | "closed"; draft: boolean; review: "required" | "approved" | "changes_requested" | "pending"; checks: "failure" | "pending" | "success" | "none"; conflicts: boolean; checked_at: string }

export interface Todo {
  id: string;
  title: string;
  notes?: string;
  /** The manager that owns this to-do on a multi-manager hub. */
  manager?: string;
  /** When it is due (#1683): a moment, or a day ("YYYY-MM-DD",
   * due by its end in the user's time zone); at most one. Unset from hubs without them. */
  due_at?: string | null;
  due_date?: string | null;
  /** When the phone is pushed about it, and when that was done (a new time is pushed again). */
  remind_at?: string | null;
  reminded_at?: string | null;
  /** "high" or "low"; unset or "" is normal. */
  priority?: string;
  /** The slug of the knowledge-base project it belongs to (#2581); unset for none. */
  project?: string;
  /** The to-do whose request this one is a piece of, and the pieces it waits for (docs/assistant/todos.md "Requests in
   *  pieces"); unset for a to-do that is no piece and from hubs without them. */
  part_of?: string;
  after?: string[];
  /** Unmet prerequisite to-do IDs for a strict piece; an empty list means none, omission keeps legacy behavior. */
  prerequisite_blockers?: string[];
  done_at?: string | null;
  created_at: string;
  updated_at: string;
  agents?: TodoAgent[];
  spaces?: TodoSpace[];
  /** Stored pictures; their private bytes are not embedded in list responses (#11084). */
  images?: TodoImage[];
  /** The assistant conversation that opened it as a task, and the plan; unset for a todo made by hand. */
  origin?: string;
  plan_id?: string;
  /** The Inbox item this task was explicitly dispatched for. */
  inbox_item?: string;
  /** "concern" records a question the owner wants answered; unset for other to-dos and older hubs. */
  kind?: string;
  /** "auto" when the hub marked the task done because its work was judged finished, with why. */
  done_by?: string;
  done_reason?: string;
  /** Cancellation is closed but not completed; the hub derives both fields for current and legacy rows (#5732). */
  cancelled_at?: string | null;
  cancelled_reason?: string;
  /** When the user last reopened it after it was done. */
  reopened_at?: string | null;
  /** The last read receipt recorded by the hub. A to-do seen
   *  since it got there is filed under Done (lib/todo-triage.ts). Unset when never seen, and from hubs before it. */
  seen_at?: string | null;
  /** Opened from the main conversation while "notify tasks the main chat dispatched only in the main chat" was on (#1731). */
  quiet?: boolean;
  /** The task's agent did not start, and why: the hub ended it there (done_at is set too, #1077). */
  start_error?: string;
  /** The main conversation supervises it (#3124): what done means and where its review stands (lib/supervision.ts). */
  supervision?: Supervision;
  /** How far the work got (`plan` / `develop` / `review` / `merge` / `deploy` / `verify`, #2776); unset on hubs that
   * don't send it, and unknown values draw nothing (lib/todo-stage.ts). */
  stage?: string;
  /** "code" uses the six stages; "general" hides them. Unset or unknown kinds keep the existing stage behavior. */
  stage_kind?: string;
  /** Closed with nothing left to deploy (#11590): the bar ends with Done after `stage` instead of marking it current.
   *  Unset while open, for work that went out or is held short of live, and from hubs before it. */
  stage_done?: boolean;
  /** The pull or merge request the work is in, when the hub knows one. */
  pr_url?: string;
  /** Fresh, observational pull request facts; these do not change the task lifecycle. */
  pr_status?: PRFacts;
  /** Facts of its earlier PRs, newest first (#7935): work spanning several PRs waits for merge while any is open. */
  other_prs?: PRFacts[];
  /** How far its finished work went out; short of live, where it is held and who can unhold it. */
  delivery?: TodoDelivery;
  /** The hub's latest automatic completion check of its work, while still current (#3180
   *  "Automatic completion"); unset before one, after a reopen or a new turn, and from hubs without it. */
  check?: TodoCheck;
  /** The chat thread the todo is bound to (#5279); unset while it has none. */
  thread?: TodoThread;
  /** Optional responsible party that is not a Persona-started agent (#12712); linked agents take precedence when present. */
  responsible?: TodoResponsible;
  /** What it waits on from the user (#2056); unset while nothing does,
   *  and from hubs before it. */
  ask?: TodoAsk;
  /** Current task-review decision, including a receipt-scoped dismissal; absent on older hubs. */
  decision_review?: { review_at: string; dismissal_id?: string };
  /** Nobody is working on it (#3618): every agent of this open
   *  to-do is gone and nothing brings one back. `reason`: `error` (closed after a model error), `offline` (its computer
   *  is not connected) or `closed`; `since` is when the hub first saw it. Unset otherwise, and from hubs before it. */
  orphaned?: { reason?: "error" | "offline" | "closed" | (string & {}); since?: string };
  /** The brief its work is held to and the to-dos that worked for it, oldest first (#3822, lib/brief.ts,
   *  docs/assistant/unified-entry.md "Briefs"); unset for a to-do that is no task, and from hubs before briefs. */
  brief?: TaskBrief;
  lineage?: LineageStep[];
  /** The owner's latest restore of it, and until when they put it off (Someday; a far date means no date), as the hub
   *  keeps them; unset when never, and from hubs before them. */
  restored_at?: string | null;
  someday_until?: string | null;
  /** The hub's view of the row (#10226): read only behind
   *  Developing Persona's switches for now (features/todos/model). Unset from managers without `todo_view`. */
  view?: TodoSummary;
}

/** Whether this closed to-do was cancelled, including the legacy supervision-only representation. */
export function isCancelled(todo: Pick<Todo, "cancelled_at" | "done_at" | "done_by" | "supervision">): boolean {
  return Boolean(todo.cancelled_at) || todo.done_by === "cancelled" || (todo.done_by === "review" && reviewState(todo) === "cancelled");
}

/** "Nobody is working on this" and why ("closed after an error"), for an open to-do the hub found orphaned; undefined
 *  for every other. A reason this page does not know leaves the lead alone. */
export function orphanedLine(todo: Pick<Todo, "orphaned" | "done_at">): { lead: string; reason: string } | undefined {
  if (!todo.orphaned || todo.done_at) return undefined;
  const reason = todo.orphaned.reason;
  return { lead: t("lib-todos.orphaned"), reason: reason === "error" || reason === "offline" || reason === "closed" ? t(`lib-todos.orphaned_${reason}`) : "" };
}

/**
 * The ask a to-do waits on, as GET /todos carries it: a deployed to-do's "Please verify" (no outcome), or a supervised
 * task's conclusion — `done`, `cannot` (cannot be done) or `decide` (needs the user's decision, with the option the
 * supervisor recommends when it has one). `verdict` is the line its Lark card says, in the request's language;
 * `asked_at` is the round, which POST /todos/{id}/accept takes back (an earlier round's is 409).
 */
export interface TodoAsk {
  /** An expired ask_user's original form. Answer it on the to-do, not its expired interaction. */
  questions?: Question[];
  outcome?: "done" | "cannot" | "decide" | string;
  verdict: string;
  recommend?: string;
  asked_at: string;
  /** An owner-only terminal action on this task. Older decision cards can still name it as an option. */
  resume_terminal?: { machine_id: string; session_id: string };
  terminal_option?: string;
  /** A decision's own fields (#3804), absent from an older hub and from an ask that is no decision: what is to be
   *  decided in one sentence, why only the owner can, the options (2 to 4, the recommended one first) with what each
   *  leads to, and what waiting costs. */
  question?: string;
  why_owner?: "irreversible" | "visible_to_others" | "priority" | "acceptance" | "only_owner_knows" | string;
  options?: AskOption[];
  cost_of_waiting?: string;
  /** Where the task came from and where it stands, as one line the hub wrote in the request's language; `from` and
   *  `stands` are its parts. */
  background?: string;
  from?: { conversation?: string; title?: string; main?: boolean; at?: string; message?: string; by_assistant?: boolean; started_by?: string };
  stands?: string;
  /** `default` "taken": the recommendation is taken at `deadline` when nobody answers; "never": it never is. */
  deadline?: string;
  default?: "taken" | "never" | string;
  /** The fixed answers supported alongside the decision's options. */
  responses?: string[];
  /** The fixed answer already given while the decision still waits (`explain_first`: Persona is to ask again with the
   *  background; the options stay answerable meanwhile). */
  response?: string;
  /** `true` while a decision that can wait is held for the next briefing: nothing was pushed for it (#4930, #5803;
   *  views/assistant/BriefingMark). Absent otherwise, and from an older hub. */
  for_briefing?: boolean;
  /** The rule the hub suggests once the owner gave this go-ahead for this directory three times (#4929,
   *  lib/rule-suggestion.ts). Absent otherwise. */
  suggestion?: RuleSuggestion;
}

/** One option of a decision: its label, which is the answer, and what choosing it leads to. */
export interface AskOption {
  label: string;
  leads_to?: string;
}

/** The fixed answers of a decision the web app knows (`TodoAsk.responses`); views/assistant/DecisionRow lists them. */
export type AskResponse = "decide_yourself" | "not_what_i_asked" | "explain_first";

/** The options of a decision with at least two labelled choices.
 *  None for any other ask. */
export function askOptions(ask: TodoAsk): AskOption[] {
  if (ask.outcome !== "decide" || !Array.isArray(ask.options)) return [];
  const list = ask.options.filter((o) => o && typeof o.label === "string" && o.label.trim());
  return list.length >= 2 ? list : [];
}

/** The ask a to-do waits on, when the hub supplied its verdict and round. */
export function openAsk(todo: Pick<Todo, "ask">): TodoAsk | undefined {
  const ask = todo.ask;
  return ask?.verdict && ask.asked_at ? ask : undefined;
}

/** The tone of the ask's verdict. */
export function askTone(ask: TodoAsk): "attention" | "red" | "green" {
  return ask.outcome === "decide" ? "attention" : ask.outcome === "cannot" ? "red" : "green";
}

/** A chat thread a todo is bound to: the channel, the chat and the thread in it name it; the title is only a label. */
export interface TodoThread {
  channel: string;
  chat_id: string;
  thread_id: string;
  title: string;
}

export interface TodoDelivery {
  state: "live" | "pr" | "merged" | "off" | "none" | string;
  gate?: string;
  /** Who can unhold it. `supervisor` (#13191): an owner hold an Agent reported on a supervised task that its supervisor
   *  has not reviewed yet; it is not the owner's until the supervisor opens a decision. Only `user` is the owner's. */
  unblock?: "user" | "agent" | "other" | "supervisor" | string;
  at?: string;
}

/** A completion check: `verdict` is `done`, `not_done`, `waiting`, `needs_user`, `undelivered` or `unsure` (others may
 *  come); `reason` is the judge's line in the user's language; `work_since` the start of the work it judged. */
interface TodoCheck {
  verdict: string;
  reason?: string;
  at: string;
  work_since?: string;
}

/** Why a stopped task is still open, from its completion check (#3180): the reason ("" when the judge gave none) while
 *  the task is neither working nor done and its check, newer than its latest reopen, found it not done; else undefined
 *  (no check, or one that says done or unsure). `state` is the row's task state. */
export function stoppedReason(todo: Pick<Todo, "check" | "done_at" | "reopened_at"> | undefined, state: string | undefined): string | undefined {
  const check = todo?.check;
  if (!todo || !check?.verdict || todo.done_at) return undefined;
  if (state === "working" || state === "done" || state === "accepted" || state === "cancelled") return undefined;
  if (check.verdict === "done" || check.verdict === "unsure") return undefined;
  if (todo.reopened_at && !(Date.parse(check.at) >= Date.parse(todo.reopened_at))) return undefined;
  return check.reason?.trim() ?? "";
}

/** An open task whose work is finished but held short of live (stopped at a PR, merged, off by default); else null. */
export function heldDelivery(todo: Todo): TodoDelivery | null {
  const d = todo.delivery;
  if (todo.done_at || !d) return null;
  return d.state === "pr" || d.state === "merged" || d.state === "off" ? d : null;
}

/** Its finished work waits for its supervisor to read an owner hold its Agent reported (`unblock` supervisor, #13191):
 *  the supervisor's to turn into a decision, park or send on, never the owner's Needs you. */
export function heldForSupervisor(todo: Todo): boolean {
  return heldDelivery(todo)?.unblock === "supervisor";
}

export type TodoStatus = "done" | "needs_you" | "interrupted" | "usage_limit" | "unread" | "working" | "waiting" | "follow_up" | "not_started" | "failed";

/** Where a status sorts, as the task board orders its sections (lib/task-board.ts): what the user must act on first —
 *  Needs you, Awaiting review (rank 2, see todoRank), Failed to start — then Working, Finished, Stopped, Done (#4896). */
export const TODO_STATUS_RANK: Record<TodoStatus, number> = {
  needs_you: 0,
  interrupted: 1,
  usage_limit: 1.5, // after Interrupted, before Awaiting review (todoRank returns 2)
  failed: 3,
  working: 4,
  waiting: 4.5, // under way like Working, its agent between rounds (#3274)
  not_started: 5,
  unread: 6,
  follow_up: 7,
  done: 8,
};

/** A status's tone. `attention` is a waiting to-do's that waits on its owner (lib/todo-wait-reason.ts todoLook, #11090):
 *  no status has it of its own. */
export type TodoTone = "red" | "green" | "amber" | "idle" | "off" | "attention";

/** The text color of a status's tone (TODO_STATUS_INFO, todoLook), as the Tasks page and the overview draw it. */
export const TODO_TONE_CLASS: Record<TodoTone, string> = {
  red: "text-red",
  green: "text-green",
  amber: "text-orange",
  idle: "text-ink-3",
  off: "text-ink-3",
  attention: "text-attention",
};

/** A task that waits for the user's review while nothing of it runs or asks, so it is the user's rather than just stopped
 *  or finished-unseen (#4896): a supervised one whose work was judged finished (#3124), or an open one whose result
 *  waits on them — to verify, to acknowledge, or to decide (its `ask`, #2056). */
export function awaitsReview(todo: Todo, status: TodoStatus): boolean {
  const reviewRequired = reviewState(todo) === "pending_review" || !!openAsk(todo) || (todo.kind !== "concern" && todo.delivery?.state === "live");
  return !todo.done_at && !acceptanceStands(todo) && !waitsForOwner(todo) && reviewRequired && (status === "follow_up" || status === "unread" || status === "not_started");
}

/** A task's place in the order: its status's rank, Awaiting review right after Needs you, Cancelled after Done. */
export function todoRank(todo: Todo, status: TodoStatus): number {
  if (isCancelled(todo)) return 9;
  return awaitsReview(todo, status) ? 2 : TODO_STATUS_RANK[shownStatus(todo, status)];
}

/** Each status's word and tone. Waiting's are the word for a wait nothing names, neutral: where the to-do and its Agents
 *  are at hand, lib/todo-wait-reason.ts todoLook names what it waits on (#11090). */
export const TODO_STATUS_INFO: Record<TodoStatus, { label: string; tone: TodoTone }> = {
  done: { label: stateName("done"), tone: "off" },
  needs_you: { label: stateName("needs_you"), tone: "amber" },
  interrupted: { label: stateName("interrupted"), tone: "red" },
  usage_limit: { label: t("lib-usage-limit.short"), tone: "amber" },
  unread: { label: stateName("unread"), tone: "green" },
  working: { label: stateName("working"), tone: "amber" },
  waiting: { label: waitName("unknown"), tone: "idle" },
  follow_up: { label: stateName("follow_up"), tone: "idle" },
  not_started: { label: stateName("not_started"), tone: "off" },
  failed: { label: stateName("failed"), tone: "red" },
};

export type SessionLookup = ((mid: string, sid: string) => Session | undefined) & {
  /** Exact pending-card lookup, present when the caller supplied its current interaction list. */
  interaction?: (id: string) => Interaction | undefined;
};

/** The hub closed it because its task's work was judged finished; reopening undoes it. */
export function isAutoDone(todo: Todo): boolean {
  return Boolean(todo.done_at) && todo.done_by === "auto";
}

/** Its supervisor closed it after reviewing the work (done_by review, #3124). */
function isReviewDone(todo: Todo): boolean {
  return Boolean(todo.done_at) && todo.done_by === "review";
}

/** Whether a finished row strikes its title through: every cancellation representation used for Cancelled filing
 *  (#11677). A task that succeeded — accepted, deployed, ticked by the owner or auto-completed — keeps a plain muted
 *  title; the filled check and the muted tone already say it is closed. A row still open (a failed start waiting on
 *  the owner) is never struck (#3713). */
export function strikesThrough(todo: Todo, open: boolean): boolean {
  return !open && isCancelled(todo);
}

/** An open task its supervisor accepted, reopened by the hub when its agent got more work, with nothing of it running
 *  or asking now: it reads as accepted, not "Follow up" (#3156).
 *  An open concern still waits for its report, so neither an accepted verdict nor its done styling applies (#3582). */
export function acceptedAtRest(todo: Todo, status: TodoStatus): boolean {
  return todo.kind !== "concern" && !todo.done_at && (!waitsForOwner(todo) || decisionDismissed(todo)) && acceptanceStands(todo) && (status === "follow_up" || status === "unread" || status === "not_started" || status === "usage_limit");
}

/** An undismissed decision only the owner can move reads Waiting for you, consistently with triage and the snapshot (#5732). */
export function waitsForOwner(todo: Todo): boolean {
  const outcome = openAsk(todo)?.outcome;
  return !todo.done_at && !decisionDismissed(todo) && (reviewState(todo) === "needs_user" || outcome === "decide" || outcome === "cannot");
}

/** The status a row draws its tone and mark from: Done for an accepted task at rest, else its own. */
export function shownStatus(todo: Todo, status: TodoStatus): TodoStatus {
  if (acceptedAtRest(todo, status)) return "done";
  if (waitsForOwner(todo)) return "needs_you";
  return awaitsReview(todo, status) ? "unread" : status;
}

/** The status's label for this todo: "Auto-completed" for one the hub closed, "Accepted" for one its supervisor did
 *  (or accepted and nothing runs again). */
export function todoStatusLabel(todo: Todo, status: TodoStatus): string {
  if (isCancelled(todo)) return t("lib-todos.review_cancelled");
  if (!todo.done_at && todo.supervision?.state === "user_check") return t("assistant-tasks.review_user_check");
  if (acceptedAtRest(todo, status)) return t("lib-todos.review_done");
  if (waitsForOwner(todo)) return t("work-entry.pending");
  if (awaitsReview(todo, status)) return t("assistant-tasks.review_pending");
  if (status === "done" && isReviewDone(todo)) return todo.supervision?.state === "cancelled" ? t("lib-todos.review_cancelled") : t("lib-todos.review_done");
  if (status === "follow_up" && todo.supervision?.state === "parked" && todo.supervision.parked?.handoff && !openAsk(todo)) {
    return TODO_STATUS_INFO.waiting.label;
  }
  return status === "done" && isAutoDone(todo) ? t("lib-todos.auto_done") : TODO_STATUS_INFO[status].label;
}

export function withInteractionLookup(base: SessionLookup, interactions: readonly Interaction[]): SessionLookup {
  const lookup: SessionLookup = (mid: string, sid: string) => base(mid, sid);
  // Resolve only when a park carries an explicit ID; ordinary status reads do not scan the pending cards.
  lookup.interaction = (id) => interactions.find((ix) => ix.id === id);
  return lookup;
}

export function makeSessionLookup(sessions: Session[] | Record<string, Session[]>, interactions?: readonly Interaction[]): SessionLookup {
  let base: SessionLookup;
  if (Array.isArray(sessions)) {
    const map = new Map<string, Session>();
    for (const s of sessions) map.set(`${s.machine_id}/${s.session_id}`, s);
    base = (mid, sid) => map.get(`${mid}/${sid}`);
  } else {
    base = (mid, sid) => (sessions[mid] ?? []).find((s) => s.session_id === sid);
  }
  return interactions ? withInteractionLookup(base, interactions) : base;
}

/**
 * Todo status rules (the first that holds wins):
 * 0. Didn't start: a task agent opened from a conversation never started and the hub ended it (start_error, #1077)
 * 1. Done: the user marked it done (done_at != null)
 * 2. Needs you: an attached agent needs you (a question, an approval)
 * 2.5 Interrupted: an attached agent's turn ended on a model API error (turn_error, #1516), unfinished, waiting for you to continue it.
 *     A subscription usage limit is not this (usage_limit, below).
 * 2.6 Usage limit: an attached agent is stopped on a subscription usage limit that has not reset yet
 * 3. Unread: an attached agent finished and you have not seen it
 * 4. Working: an attached agent is working or starting
 * 4.5 Waiting: an attached agent ended its turn but something brings it back (agentWaits, #3274)
 * 5. Follow-up: every attached agent is stopped (seen, not running, failed to start)
 * 6. Not started: no attached agent
 */
export function todoStatus(todo: Todo, lookup?: SessionLookup): TodoStatus {
  if (todo.start_error) return "failed";
  if (todo.done_at) return "done";
  const linkedID = parkInteractionID(todo);
  const ownerAsk = linkedParkOwnerAsk(todo);
  const viewKeepsPark = linkedID && todo.view && !ownerAsk && todo.view.section !== "needs_you" && todo.view.section !== "can_wait";
  const candidate = viewKeepsPark ? undefined : linkedID ? lookup?.interaction?.(linkedID) : undefined;
  const linked = candidate && linkedParkInteraction(todo, [candidate]);
  if (linked) return "needs_you";
  // With no pending-card list, use the manager's view; a supplied list, including an empty one after resolution, wins.
  if (linkedID && !lookup?.interaction && ownerAsk) return "needs_you";
  const active = (todo.agents ?? []).filter((a) => !a.released_at);
  if (active.length === 0) return responsibleOf(todo) ? "follow_up" : "not_started";

  let hasNeedsYou = false;
  let hasInterrupted = false;
  let hasLimit = false;
  let hasUnread = false;
  let hasWorking = false;
  let hasWaiting = false;

  for (const a of active) {
    if (a.start_error) {
      // a failed start counts as stopped
      continue;
    }
    const s = lookup?.(a.machine_id, a.session_id);
    // its child sessions' state folded in (#5120): one waiting on the user makes the task wait, one working works
    const st: AgentState = s ? foldedState(s) : "offline";
    if (st === "needs_you") {
      if (linkedID && !linked && s?.work?.interaction_id === linkedID) {
        // The exact linked card is gone; this is its stale session state, not a new owner question.
        continue;
      }
      hasNeedsYou = true;
      break;
    }
    if (usageLimitAgent(s)) hasLimit = true;
    else if (interruptedAgent(s) && (s?.turn_error_kind === "unrecoverable" || s?.turn_error_code === "compaction_sealed" ||
      unrecoverableError(s?.turn_error))) hasInterrupted = true;
    else if (active.length > 1 && st !== "working" && s && agentWaits(s)) hasWaiting = true;
    else if (interruptedAgent(s)) hasInterrupted = true;
    else if (st === "unread") hasUnread = true;
    else if (s && agentWaits(s)) hasWaiting = true;
    else if (st === "working" || s?.starting) hasWorking = true;
  }

  if (hasNeedsYou) return "needs_you";
  if (hasInterrupted && !hasWorking && !hasWaiting && todo.supervision?.state !== "parked") return "interrupted";
  if (hasLimit) return "usage_limit";
  if (hasUnread) return "unread";
  if (hasWorking) return "working";
  if (hasWaiting) return "waiting";
  return "follow_up";
}

/** The agent ended its turn, yet something brings it back, so its task waits rather than stopped (#3274; the hub's
 *  agentWaits): only its background commands, subagents, Monitors or workflows run and will report back
 *  (background_working), or the hub waits out another agent, a deploy / CI / a time, or a moment's model failure
 *  for it and wakes it (work blocked on anything but the user). */
export function agentWaits(s: Session): boolean {
  const st = stateOf(s);
  if (st === "working") return Boolean(s.background_working);
  const w = s.work;
  return st !== "needs_you" && st !== "unread" && w?.phase === "blocked" && (w.on === "agent" || w.on === "external" || w.on === "transient");
}

/** What a waiting task waits for, in one line: the first waiting agent's own words for it (#3274). */
export function waitingLine(todo: Todo, lookup?: SessionLookup): string | undefined {
  for (const a of todo.agents ?? []) {
    if (a.released_at) continue;
    const s = lookup?.(a.machine_id, a.session_id);
    if (!s || !agentWaits(s)) continue;
    const line = (s.work?.phase === "blocked" ? s.work.line || s.work.wait : undefined)?.trim();
    if (line) return line;
  }
  return undefined;
}

/** The agent stopped on a model API error (the hub's turn_error): cut off, not finished (#1516). */
export function interruptedAgent(s?: Session): boolean {
  if (!s?.turn_error || usageLimitAgent(s)) return false;
  const st = stateOf(s);
  return st === "unread" || st === "idle";
}

/** The hub's own word on a turn that had no output at all (#10187): `no_output` (the model service answered nothing) or
 *  `stopped` (Persona's Stop interrupted it with its conversation). Not a model API error: its turn_error is a sentence
 *  in the user's language, said as it is. */
const SILENT_TURN_KINDS: ReadonlySet<string> = new Set(["no_output", "stopped"]);

/** Why the model service cut the agent off (#1516), in words, and whether "Continue" can help (a login or a bad request is
 *  not fixed by going on); undefined when it was not cut off. A turn with no output says the hub's sentence for it. */
export function interruption(s?: Session): { reason: string; canContinue: boolean; invalid?: boolean } | undefined {
  if (!interruptedAgent(s)) return undefined;
  if (s?.turn_error_kind === "unrecoverable" || s?.turn_error_code === "compaction_sealed" || unrecoverableError(s?.turn_error)) {
    return { reason: t("lib-api-errors.conversation_invalid"), canContinue: false, invalid: true };
  }
  if (SILENT_TURN_KINDS.has(s?.turn_error_kind ?? "")) return { reason: s!.turn_error!.trim(), canContinue: true };
  const info = parseAPIError(s?.turn_error);
  return { reason: info?.reason ?? t("lib-todos.model_request_failed"), canContinue: !info || retryable(info) };
}

/**
 * The todo list's order, the task board's (#4896): what needs the user (Needs you, Interrupted, Awaiting review) first;
 * then the open ones by due date, earliest (an overdue one) first, those without one after (#1683); then by todoRank —
 * Failed to start, Working, Not started, Finished, Stopped, Done, Cancelled — then newest first. A task that did not start has
 * done_at too, but it is the user's to retry or remove, so it does not sink with the done ones.
 */
export function sortTodos(todos: Todo[], lookup?: SessionLookup): Todo[] {
  return [...todos].sort((a, b) => {
    const rA = todoRank(a, todoStatus(a, lookup));
    const rB = todoRank(b, todoStatus(b, lookup));
    if ((rA <= 2 || rB <= 2) && rA !== rB) return rA - rB;
    if (!a.done_at && !b.done_at) {
      const dA = todoDueBy(a);
      const dB = todoDueBy(b);
      if (dA !== undefined && dB !== undefined && dA !== dB) return dA - dB;
      if ((dA === undefined) !== (dB === undefined)) return dA === undefined ? 1 : -1;
    }
    if (rA !== rB) return rA - rB;

    const tA = a.created_at ? Date.parse(a.created_at) || 0 : 0;
    const tB = b.created_at ? Date.parse(b.created_at) || 0 : 0;
    if (tA !== tB) return tB - tA;
    return a.id.localeCompare(b.id);
  });
}

/** The local midnight starting a todo's due_date ("YYYY-MM-DD"), in ms; undefined without a readable one. */
export function todoDueDay(todo: Todo): number | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(todo.due_date ?? "");
  if (!m) return undefined;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getTime();
}

/** The moment a todo is due by, in ms: its due_at, or the end of its due day; undefined without either. */
export function todoDueBy(todo: Todo): number | undefined {
  if (todo.due_at) {
    const at = Date.parse(todo.due_at);
    if (!Number.isNaN(at)) return at;
  }
  const day = todoDueDay(todo);
  if (day === undefined) return undefined;
  const end = new Date(day);
  end.setDate(end.getDate() + 1);
  return end.getTime() - 1000;
}

/**
 * Finds the most pressing agent of a todo and its summary
 */
export function mostUrgentAgent(
  todo: Todo,
  lookup?: SessionLookup,
): { agent: TodoAgent; session?: Session; status: AgentState | "failed" } | null {
  const active = (todo.agents ?? []).filter((a) => !a.released_at);
  if (active.length === 0) return null;

  const score = (st: AgentState | "failed", s?: Session) => {
    if (interruptedAgent(s)) return 3.5; // after one waiting on the user, before one done
    if (usageLimitAgent(s)) return 3.4;
    switch (st) {
      case "needs_you":
        return 4;
      case "unread":
        return 3;
      case "working":
        return 2;
      case "failed":
        return 1;
      default:
        return 0;
    }
  };

  let best: { agent: TodoAgent; session?: Session; status: AgentState | "failed" } | null = null;
  let bestScore = -1;

  for (const a of active) {
    const s = lookup?.(a.machine_id, a.session_id);
    const st: AgentState | "failed" = a.start_error ? "failed" : s ? foldedState(s) : "offline";
    const sc = score(st, s);
    if (sc > bestScore || !best) {
      bestScore = sc;
      best = { agent: a, session: s, status: st };
    }
  }

  return best;
}

// ── Decision log (#1618) ───────────────

/** One call the user made on a todo: written by hand, or their answer to a question card of an agent on it. */
export interface TodoDecision {
  id: string;
  todo_id: string;
  question: string;
  choice: string;
  reason: string;
  source: "manual" | "card" | "outcome" | "agent" | "thread" | string;
  source_machine: string;
  source_session: string;
  /** The question card (interaction id) it was answered in; empty for one written by hand. */
  source_message: string;
  /** The earlier decision this one replaces. */
  supersedes_id: string;
  /** The newest decision that replaces this one; empty while it is in effect. */
  superseded_by: string;
  created_at: string;
  /** Completion time for an outcome written by Persona, which may be recorded later. */
  phase_completed_at?: string;
}

/** How a to-do's Decisions list is shown (#6447): the first few, the
 *  default each time a to-do opens; every one; or none, folded under its title. */
export type DecisionsView = "first" | "all" | "folded";
/** How many decisions the list shows before Show all. */
export const DECISIONS_FIRST = 5;

/** What `view` shows of `list` (the current decisions, or the full history), in its order. */
export function shownDecisions<T>(list: readonly T[], view: DecisionsView): T[] {
  if (view === "folded") return [];
  return view === "first" ? list.slice(0, DECISIONS_FIRST) : [...list];
}

/** Every decision of a todo, oldest first, superseded ones included (`superseded_by` set). */
export async function fetchTodoDecisions(todoId: string): Promise<TodoDecision[]> {
  const r = await api<{ decisions: TodoDecision[] }>(`/todos/${enc(todoId)}/decisions?all=1`);
  return r.decisions ?? [];
}

/**
 * Which todo a project hangs on:
 * returns the unfinished Todo that holds the project (machineId, spaceName).
 */
export function findTodoForSpace(todos: Todo[], machineId: string, spaceName: string): Todo | undefined {
  if (!machineId || !spaceName) return undefined;
  for (const t of todos) {
    if (t.done_at) continue;
    if (t.spaces?.some((sp) => sp.machine_id === machineId && sp.space === spaceName)) {
      return t;
    }
  }
  return undefined;
}

/**
 * Navigate to a todo detail view and trigger drawer close callback if provided.
 */
export function openTodoFromTag(
  todoId: string,
  nav: (to: Route) => void,
  onNavigate?: () => void,
) {
  nav({ name: "todos", id: todoId });
  onNavigate?.();
}

/**
 * Check if an error from the spaces (or decisions, #1618) endpoints is a legacy hub 404 (route does not exist).
 * In Persona hub, jsonRouteErrors produces code === "not_found" and error === "Not Found"
 * for unregistered routes. Handlers themselves return specific messages like "todo not found"
 * or a localized "todo does not exist". Only a route-level 404 (code === "not_found" and error === "Not Found", or non-JSON 404)
 * marks the hub as legacy; other 404s are ordinary business failures (#881 review).
 */
export function isLegacySpacesRouteError(e: unknown): boolean {
  if (!(e instanceof ApiError) || e.status !== 404) return false;
  if (!e.isJson) return true;
  return e.body?.code === "not_found" && e.body?.error === "Not Found";
}

/**
 * Check whether todos data supports the spaces field (legacy hub compatibility).
 * An empty list is treated as unknown/unsupported (returns false).
 * Only returns true if at least one todo explicitly contains a spaces array.
 */
export function isSpacesSupported(todos: Todo[]): boolean {
  if (todos.length === 0) return false;
  return todos.some((t) => "spaces" in t && Array.isArray(t.spaces));
}

/** An agent going on ("Needs you" / "Working") that the main conversation's dock shows after its tasks (#1113, iOS DockAgent). */
export interface DockAgent {
  session: Session;
  state: "needs_you" | "working";
}

/**
 * What a conversation's dock shows after its tasks: in the main
 * conversation, every agent going on that no task stands for — the ones opened by hand on the Agents page — waiting
 * for the user first, then the newest in its state; elsewhere nothing. Persona's own conversations are not agents. A
 * task's agents (a task not done, from any conversation) are its pill already; an agent the hub tags with a todo shows
 * only when that todo was made by hand, so a task the todo list has not caught up with stays out too. A child session
 * folded under one of them is no entry of its own: its parent's entry carries its state (#5120).
 */
export function dockAgents(sessions: Session[], todos: Todo[], conversation: string, main: string | undefined): DockAgent[] {
  if (!main || conversation !== main) return [];
  sessions = foldRows(sessions);
  const taskAgents = new Set<string>();
  const manual = new Set<string>();
  for (const t of todos) {
    if (!t.origin) manual.add(t.id);
    else if (!t.done_at) for (const a of t.agents ?? []) if (!a.released_at) taskAgents.add(`${a.machine_id}/${a.session_id}`);
  }
  const out: DockAgent[] = [];
  for (const s of sessions) {
    if (s.assistant) continue;
    const state = stateOf(s);
    if (state !== "needs_you" && state !== "working") continue;
    if (taskAgents.has(`${s.machine_id}/${s.session_id}`)) continue;
    if (s.todo && !manual.has(s.todo.id)) continue;
    out.push({ session: s, state });
  }
  return out.sort(
    (a, b) =>
      (a.state === "needs_you" ? 0 : 1) - (b.state === "needs_you" ? 0 : 1) ||
      byTime(b.session.state_since, a.session.state_since) ||
      `${a.session.machine_id}/${a.session.session_id}`.localeCompare(`${b.session.machine_id}/${b.session.session_id}`),
  );
}

/** What a task's lead agent says or does now (iOS TaskNow). */
export type TaskNow = { kind: "words"; text: string } | { kind: "step"; text: string; since?: string };

/**
 * A working agent's step (its latest tool call, which the hub only sends when it is newer than its words) comes
 * before its words; once it stopped, its words do (a turn cut short on a tool call), and the step only stands in for
 * words it never said. Null when it has said and done nothing yet (#1583).
 */
export function taskNow(lead: Session | undefined, working: boolean): TaskNow | null {
  const words = lead?.last_text?.trim() ?? "";
  const step = lead?.activity?.trim() ?? "";
  if (step && (working || !words)) return { kind: "step", text: step, since: working ? lead?.activity_at : undefined };
  if (words) return { kind: "words", text: words };
  return null;
}

/** What each of an agent's running subagents is for: shown under its own progress, never instead of it. */
export function runningSubagents(s: Session | undefined): string[] {
  return (s?.subagents ?? []).filter((a) => a.running).map((a) => a.description?.trim() || a.type || t("lib-todos.subtask"));
}
