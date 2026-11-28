import { t } from "../i18n";
import { type PlanTask, type TaskPlan } from "./feed";
import type { Todo } from "./todos";

/** The plan task a to-do runs as: its plan's (`plan_id`), else the plan that took it over through todo_id (a to-do
 *  such a plan runs keeps no `plan_id`, #8770); undefined while no plan the page has holds it. A failed start's carries
 *  why (`error_code`, `error_args`) and the launchers it may start with instead (`retry_with`). */
export function planTaskOf(todo: Todo, plans: Record<string, TaskPlan>): PlanTask | undefined {
  const own = todo.plan_id ? plans[todo.plan_id]?.tasks.find((t) => t.todo_id === todo.id) : undefined;
  if (own) return own;
  for (const p of Object.values(plans)) {
    if (p.state === "dismissed") continue;
    const task = p.tasks.find((t) => t.todo_id === todo.id);
    if (task) return task;
  }
  return undefined;
}

/** A failed start's coded refusal, as its plan task carries it (`error_code`, `error_args`). */
export interface StartFailureCode {
  error_code?: string;
  error_args?: Record<string, string>;
}

/** The coded refusal in a short line, read before the text (lib/start-failure.ts startFailureRowLine): a launcher the
 *  new terminal tab could not run (its long sentence, the shells and the file, stays for the details), or a checkout
 *  folder an earlier start left where the new one goes; undefined for any other code. */
export function startFailureCodeLine(coded: StartFailureCode | undefined): string | undefined {
  const args = coded?.error_args ?? {};
  switch (coded?.error_code) {
    case "launcher_missing":
      return args.where === "terminal" && args.launcher ? t("lib-start-retry.start_code_launcher_terminal", { launcher: args.launcher }) : undefined;
    case "worktree_path_taken":
      return t("lib-start-retry.start_code_worktree_path_taken");
  }
  return undefined;
}
