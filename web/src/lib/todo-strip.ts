// The to-do strip at the top of an Agent's chat (views/chat/TodoStrip.tsx): where the to-do its session is linked to
// stands, worked out from what the hub already sends (the session's `todo` / `completed_todo`, the to-do list's row),
// and the one most useful next thing. Pure and wording-free: the view says it in the owner's language.
import { briefCriteria } from "./brief";
import { checklist, reviewState, type CheckMark } from "./supervision";
import { todoStageIndex } from "./todo-stage";
import { openMergePRs, type OwnerNeed, type OwnerWait } from "./todo-triage";
import { heldDelivery, isCancelled, openAsk, type Todo, type TodoAgent } from "./todos";

/** One "done when" criterion: its place in the to-do's list (1-based, the hub's order), its words, its latest mark. */
export interface StripCriterion {
  n: number;
  text: string;
  mark: CheckMark;
}

/** What the work waits on that is not the owner: a review of it, a merge, a deploy (or the check after it). */
export type StripWait = "review" | "merge" | "deploy";

/** The single most useful next thing, first that holds: what the owner is asked for; a review, merge or deploy it waits
 *  on; the first criterion not met; every criterion met; nobody on it; nothing to say. */
export type StripNext =
  | { kind: "owner"; need: OwnerWait; text?: string }
  | { kind: "wait"; on: StripWait }
  | { kind: "criterion"; criterion: StripCriterion }
  | { kind: "all_met" }
  | { kind: "unattended" }
  | { kind: "none" };

export type TodoStripModel =
  /** The hub sent no row for the session's to-do (another assistant's, or not read yet): its title and a link. */
  | { form: "missing"; id: string; title: string }
  /** Closed: done, or cancelled. Its one action opens it. */
  | { form: "finished"; id: string; title: string; how: "done" | "cancelled"; todo?: Todo }
  | {
      form: "open";
      id: string;
      title: string;
      /** The sidebar badge counts it (useOwnerNeeds): the one rule of "Needs you". */
      needsYou: boolean;
      /** Its stage when the hub sent one this build knows, for a to-do that uses stages. */
      stage?: string;
      criteria: StripCriterion[];
      next: StripNext;
      /** The other Agents on it now, this one left out. */
      others: TodoAgent[];
      todo: Todo;
    };

type Link = { id: string; title: string };

/** Its criteria in effect: the supervision's list with each one's latest mark, else its brief's, none checked yet. */
export function stripCriteria(todo: Todo): StripCriterion[] {
  const checked = checklist(todo.supervision);
  if (checked.length) return checked.map((c, i) => ({ n: i + 1, text: c.criterion, mark: c.mark }));
  return (todo.brief ? briefCriteria(todo.brief) : []).map((text, i) => ({ n: i + 1, text, mark: "open" as const }));
}

/** A criterion still to do: neither met nor left unchecked by an acceptance that stands. */
export function unmet(c: StripCriterion): boolean {
  return c.mark === "open" || c.mark === "missing";
}

/** What its work waits on that is not the owner, if anything. */
function waitsOn(todo: Todo, now: number): StripWait | undefined {
  const sup = todo.supervision;
  if (reviewState(todo) === "pending_review" || sup?.auto_review?.state === "running") return "review";
  const held = heldDelivery(todo);
  if (openMergePRs(todo, now).length || held?.state === "pr" || (sup?.state === "parked" && sup.parked?.evidence?.kind === "merge_review")) return "merge";
  if (held) return "deploy";
  return undefined;
}

/** The words of what the owner is asked, when the hub wrote some: a decision's question, else the ask's verdict line,
 *  else the first of the row's asks. */
function ownerText(todo: Todo): string | undefined {
  const ask = openAsk(todo);
  const text = ask?.question?.trim() || ask?.verdict?.trim() || todo.view?.asks?.find((a) => a.text?.trim())?.text?.trim();
  return text || undefined;
}

/**
 * The strip of the session `self`, linked to `link` (its `todo`, or `completed` for its `completed_todo`), given the
 * to-do list's row for it (undefined when the hub sent none) and why the sidebar badge counts it (undefined when it
 * does not).
 */
export function todoStrip(
  link: Link,
  todo: Todo | undefined,
  need: OwnerNeed | undefined,
  self: { machine_id: string; session_id: string },
  now: number,
  completed = false,
): TodoStripModel {
  const id = link.id;
  if (!todo) return completed ? { form: "finished", id, title: link.title, how: "done" } : { form: "missing", id, title: link.title };
  const title = todo.title?.trim() || link.title;
  // a start that failed has done_at too, yet it is the owner's to retry: it stays open (todos.ts todoStatus)
  if (isCancelled(todo)) return { form: "finished", id, title, how: "cancelled", todo };
  if (todo.done_at && !todo.start_error) return { form: "finished", id, title, how: "done", todo };

  const criteria = stripCriteria(todo);
  const first = criteria.find(unmet);
  const wait = waitsOn(todo, now);
  const next: StripNext =
    need && need !== "unattended" ? { kind: "owner", need, text: ownerText(todo) }
    : wait ? { kind: "wait", on: wait }
    : first ? { kind: "criterion", criterion: first }
    : criteria.length ? { kind: "all_met" }
    : need === "unattended" ? { kind: "unattended" }
    : { kind: "none" };
  const stage = todo.stage_kind !== "general" && todoStageIndex(todo.stage) >= 0 ? todo.stage : undefined;
  const others = (todo.agents ?? []).filter(
    (a) => !a.released_at && !a.dismissed_at && !(a.machine_id === self.machine_id && a.session_id === self.session_id),
  );
  return { form: "open", id, title, needsYou: Boolean(need), stage, criteria, next, others, todo };
}
