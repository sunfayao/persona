// A to-do run by a supervisor Agent (internally a task leader) and the workers it hands out, as the to-do's page shows
// it: the supervisor pinned first under "Linked agent sessions" with its workers as a tree beneath it, a worker's way
// back to the to-do it works for, and the "Supervision" log. Everything is read from the to-dos the store already holds
// (GET /api/v1/todos and its pushes); nothing here asks the hub.
//
// The hub's shape: the root to-do has an agent row with `source: "leader"`, its supervisor. Each worker is a to-do of its
// own whose agent row has `source: "worker"` and `via` the root's id, and whose ledger (`supervision.supervisor`) names
// the supervisor as "<machine>/<session>". The supervisor's verdicts on a worker are that worker's reviews; once it has
// checked everything, the root waits for the main conversation's review (`pending_review` since `pending_since`), whose
// verdicts are the root's own reviews.
import { t } from "../i18n";
import { startOfDay } from "./feed";
import { stateName } from "./state-labels";
import { progressLine, reviewState, reviewVerdictLabel, type TaskReview } from "./supervision";
import { cancellationReason } from "./todo-details";
import { isCancelled, type Todo, type TodoAgent } from "./todos";

const ref = (a: Pick<TodoAgent, "machine_id" | "session_id">) => `${a.machine_id}/${a.session_id}`;
const ms = (iso: string | null | undefined) => {
  const v = iso ? Date.parse(iso) : NaN;
  return Number.isFinite(v) ? v : undefined;
};
/** Still on its to-do: neither released nor dismissed. */
const onIt = (a: TodoAgent) => !a.released_at && !a.dismissed_at;

/** The to-do's supervisor: its leader row still on it, the latest linked when there are several. */
export function supervisorOf(todo: Pick<Todo, "agents">): TodoAgent | undefined {
  let best: TodoAgent | undefined;
  for (const a of todo.agents ?? []) if (a.source === "leader" && onIt(a) && (!best || (ms(a.linked_at) ?? 0) >= (ms(best.linked_at) ?? 0))) best = a;
  return best;
}

/** The worker to-dos of a supervised root, oldest first: a worker row names the root (`via`), or the ledger names one of
 *  its supervisors, current or earlier. The root itself never is one. */
export function workersOf(root: Todo, todos: readonly Todo[]): Todo[] {
  const leaders = new Set((root.agents ?? []).filter((a) => a.source === "leader").map(ref));
  return todos
    .filter((w) => w.id !== root.id && ((w.agents ?? []).some((a) => a.source === "worker" && a.via === root.id) || leaders.has(w.supervision?.supervisor ?? "")))
    .sort((a, b) => (ms(a.created_at) ?? 0) - (ms(b.created_at) ?? 0) || a.id.localeCompare(b.id));
}

/** The root a worker works for: the to-do its worker row names, else the one whose supervisor its ledger names. */
function rootOf(todo: Todo, todos: readonly Todo[]): Todo | undefined {
  const via = (todo.agents ?? []).find((a) => a.source === "worker" && a.via && a.via !== todo.id)?.via;
  const named = via ? todos.find((x) => x.id === via) : undefined;
  if (named) return named;
  const supervisor = todo.supervision?.supervisor;
  return supervisor ? todos.find((x) => x.id !== todo.id && (x.agents ?? []).some((a) => a.source === "leader" && ref(a) === supervisor)) : undefined;
}

/**
 * How a to-do takes part in supervision: `root`, a supervisor ran or runs it (a leader row, on it now or before), with
 * its workers; `worker`, a supervisor handed it out (a worker row, or a root in the list names it), with that root when
 * the list has it; undefined for every other to-do.
 */
export type SupervisionRole = { kind: "root"; workers: Todo[] } | { kind: "worker"; root?: Todo };

export function supervisionRole(todo: Todo, todos: readonly Todo[]): SupervisionRole | undefined {
  if ((todo.agents ?? []).some((a) => a.source === "leader")) return { kind: "root", workers: workersOf(todo, todos) };
  const root = rootOf(todo, todos);
  if (root || (todo.agents ?? []).some((a) => a.source === "worker")) return { kind: "worker", root };
  return undefined;
}

/** A worker's own Agent: its worker row (the one on it now, else the latest), else the Agent on it now, else its last. */
export function workerAgent(todo: Pick<Todo, "agents">): TodoAgent | undefined {
  const all = [...(todo.agents ?? [])].reverse();
  const workers = all.filter((a) => a.source === "worker");
  return workers.find(onIt) ?? workers[0] ?? all.find(onIt) ?? all[0];
}

/**
 * Where a worker stands for its supervisor:
 *   working     open, nothing to judge yet;
 *   review      it waits for the supervisor's review: the hub says so, or its Agent reported done since the last one;
 *   rework      sent back (the ledger's round);
 *   needs_user  passed up for a decision that is not the supervisor's;
 *   parked      put off until something happens;
 *   accepted    the supervisor accepted it (also reopened, while that acceptance stands);
 *   done        closed without an acceptance;
 *   cancelled   closed by cancellation;
 *   failed      its Agent did not start.
 */
export type WorkerStep = "working" | "review" | "rework" | "needs_user" | "parked" | "accepted" | "done" | "cancelled" | "failed";

export function workerStep(w: Todo): WorkerStep {
  if (isCancelled(w)) return "cancelled";
  if (w.start_error) return "failed";
  const review = reviewState(w);
  if (w.done_at) return review === "accepted" || w.done_by === "review" ? "accepted" : "done";
  if (review === "pending_review") return "review";
  if (review === "rework" || review === "needs_user" || review === "parked" || review === "accepted") return review;
  return reportedDone(w) ? "review" : "working";
}

/** Its Agent's latest report says done and no review has answered it yet. */
function reportedDone(w: Todo): boolean {
  const p = w.supervision?.progress;
  if (p?.kind !== "done") return false;
  const last = w.supervision?.reviews?.at(-1);
  return !last || !p.at || Date.parse(last.at) < Date.parse(p.at);
}

/** The worker's step in words: the review states as everywhere else (reviewVerdictLabel), the rest as an Agent's. */
export function workerStepLabel(w: Todo): string {
  const step = workerStep(w);
  switch (step) {
    case "review":
      return t("assistant-tasks.review_pending");
    case "rework":
    case "needs_user":
    case "parked":
    case "accepted":
    case "cancelled":
      return reviewVerdictLabel(step, w.supervision?.round);
    case "done":
    case "failed":
    case "working":
      return stateName(step);
  }
}

// ── the log ─────────────────────────────────────────────────

/** How a closed to-do ended. */
type Outcome = "done" | "cancelled" | "failed";

type EntryBody =
  | { kind: "planned" }
  | { kind: "review"; review: TaskReview; root: boolean }
  | { kind: "report" }
  | { kind: "closed"; outcome: Outcome }
  | { kind: "reported" };

/**
 * One line of the Supervision log, about `todo` (a worker, or the root itself), at `at` (ms):
 *   planned   the supervisor handed the worker out (its creation);
 *   review    a verdict: the supervisor's on a worker, or the main conversation's on the root (`root`);
 *   report    the worker's latest report;
 *   closed    it closed some other way than by a verdict (a verdict that closed it is its own line);
 *   reported  the supervisor reported the root done, and it waits for the main conversation's review.
 */
export type SupervisionEntry = { key: string; at: number; todo: Todo } & EntryBody;

/** The Supervision log of a to-do, newest first: for a root, what happened to each worker and how the main conversation
 *  took the result; for a worker, what happened to it. Lines without a readable time are left out. */
export function supervisionLog(todo: Todo, role: SupervisionRole | undefined): SupervisionEntry[] {
  const out: SupervisionEntry[] = [];
  const add = (td: Todo, iso: string | null | undefined, key: string, body: EntryBody) => {
    const at = ms(iso);
    if (at !== undefined) out.push({ ...body, key: `${td.id}:${key}`, at, todo: td });
  };
  const verdicts = (td: Todo, root: boolean) => (td.supervision?.reviews ?? []).forEach((review, i) => add(td, review.at, `review:${i}`, { kind: "review", review, root }));
  const closed = (td: Todo) => {
    if (!td.done_at && !td.cancelled_at) return;
    const outcome: Outcome = isCancelled(td) ? "cancelled" : td.start_error ? "failed" : "done";
    // a verdict that closed it (done_by review) already says so on its own line
    const verdict = outcome === "cancelled" ? "cancelled" : "accepted";
    if (td.done_by === "review" && td.supervision?.reviews?.some((r) => r.verdict === verdict)) return;
    add(td, outcome === "cancelled" ? td.cancelled_at || td.done_at : td.done_at, "closed", { kind: "closed", outcome });
  };
  const worker = (w: Todo) => {
    add(w, w.created_at, "planned", { kind: "planned" });
    verdicts(w, false);
    if (progressLine(w.supervision)) add(w, w.supervision?.progress?.at, "report", { kind: "report" });
    closed(w);
  };
  if (role?.kind === "root") {
    role.workers.forEach(worker);
    if (reviewState(todo) === "pending_review") add(todo, todo.supervision?.pending_since, "reported", { kind: "reported" });
    verdicts(todo, true);
    closed(todo);
  } else if (role?.kind === "worker") worker(todo);
  // newest first; lines of the same moment in the reverse of the order above (sort is stable)
  return out.reverse().sort((a, b) => b.at - a.at);
}

/** A day of the log, in the owner's local calendar: `key` YYYY-MM-DD, `date` its midnight; `today` when `now` is in it. */
export interface SupervisionDay {
  key: string;
  date: Date;
  today: boolean;
  entries: SupervisionEntry[];
}

/** The log (newest first) by day, the newest day first, with the day lines' rule of the chat and the feed (lib/feed.ts
 *  startOfDay, a day keyed by its local date). */
export function supervisionDays(entries: readonly SupervisionEntry[], now = Date.now()): SupervisionDay[] {
  const today = startOfDay(now).getTime();
  const days: SupervisionDay[] = [];
  for (const e of entries) {
    const date = startOfDay(e.at);
    let day = days.at(-1);
    if (!day || day.date.getTime() !== date.getTime()) days.push((day = { key: date.toLocaleDateString("sv"), date, today: date.getTime() === today, entries: [] }));
    day.entries.push(e);
  }
  return days;
}

/** A line of the log in words: what happened (`label`), and what was said with it (`detail`, "" when nothing). */
export function entryWords(e: SupervisionEntry): { label: string; detail: string } {
  switch (e.kind) {
    case "planned":
      return { label: t("todos.supervision_planned"), detail: "" };
    case "review": {
      const verdict = reviewVerdictLabel(e.review.verdict, e.review.round);
      return { label: e.root ? t("todos.supervision_main_review", { verdict }) : verdict, detail: e.review.note?.trim() || e.review.reason?.trim() || "" };
    }
    case "report":
      return { label: t("assistant-tasks.report_latest"), detail: progressLine(e.todo.supervision) ?? "" };
    case "closed":
      if (e.outcome === "cancelled") return { label: t("assistant-tasks.review_cancelled"), detail: cancellationReason(e.todo) };
      return { label: stateName(e.outcome), detail: (e.outcome === "failed" ? e.todo.start_error : e.todo.done_reason)?.trim() ?? "" };
    case "reported":
      return { label: t("todos.supervision_reported"), detail: "" };
  }
}
