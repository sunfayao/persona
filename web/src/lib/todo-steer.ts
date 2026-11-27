// What the to-do strip (views/chat/TodoStrip.tsx) prepares in a composer for the owner to edit and send: messages to an
// Agent about the to-do its session works on, and one to the main conversation to take its supervision over. They are
// read by a model but written here, on the page, from the to-do's facts the hub sent, so the owner does not retype them:
// English, short, factual, naming the to-do by its title and id and each criterion by its number in the to-do's list.
// They are drafts, never sent from here: the owner sends them through the composer and the outbox.
import { unmet, type StripCriterion } from "./todo-strip";

/** The facts of a to-do a message names. */
export interface SteerTodo {
  id: string;
  title: string;
  /** Its stage (`plan` … `verify`) when it uses stages; none otherwise. */
  stage?: string;
  criteria: StripCriterion[];
}

const MARK_WORD: Record<StripCriterion["mark"], string> = {
  met: "met",
  missing: "not met",
  open: "open",
  unchecked: "not checked",
};

/** Words on one line, as a quoted name: inner quotes and line breaks would break the sentence around it. */
function quoted(text: string): string {
  return `"${text.replace(/\s+/g, " ").replace(/"/g, "'").trim()}"`;
}

function head(todo: SteerTodo): string {
  return `To-do ${quoted(todo.title)} (${todo.id})${todo.stage ? `, stage: ${todo.stage}` : ""}.`;
}

function criteriaLines(todo: SteerTodo, marks: boolean): string {
  return ["Done when:", ...todo.criteria.map((c) => `${c.n}. ${marks ? `[${MARK_WORD[c.mark]}] ` : ""}${c.text.replace(/\s+/g, " ").trim()}`)].join("\n");
}

/** "Continue with the to-do": where the Agent stands against each criterion, then on with the first one not met. */
export function continueMessage(todo: SteerTodo): string {
  if (!todo.criteria.length) return `${head(todo)}\nSay in one line where you stand against this to-do, then continue with its next step.`;
  const first = todo.criteria.find(unmet);
  const ask = first
    ? `Say in one line per criterion where you stand against it, then continue with the first one not yet met: criterion ${first.n}.`
    : "Say in one line per criterion what shows it is met. If one is not, continue with it.";
  return `${head(todo)}\n${criteriaLines(todo, true)}\n${ask}`;
}

/** "Report against the to-do": a short status per criterion and nothing else. */
export function reportMessage(todo: SteerTodo): string {
  if (!todo.criteria.length) return `${head(todo)}\nReport your status on this to-do in a few short lines: done, not done, or blocked and why. Nothing else.`;
  return `${head(todo)}\n${criteriaLines(todo, false)}\nFor each criterion, report only its status in one short line: done, not done, or blocked and why. Nothing else.`;
}

/** "Do this next" on one criterion. */
export function nextCriterionMessage(todo: SteerTodo, c: StripCriterion): string {
  return `${head(todo)}\nWork next on criterion ${c.n}: ${quoted(c.text)}. Say when it is met and what shows it.`;
}

/** "Ask the main conversation": the assistant takes over supervising the to-do and this Agent on it. */
export function supervisionMessage(todo: SteerTodo, agent: { title: string; machine_id: string; session_id: string }): string {
  return `Please take over supervising the to-do ${quoted(todo.title)} (${todo.id}). The agent ${quoted(agent.title)} (session ${agent.session_id} on computer ${agent.machine_id}) works on it. Check where it stands against its criteria and guide the agent until it is done.`;
}
