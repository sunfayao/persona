import { useFeed } from "../../lib/feed";
import { openTask } from "../../lib/handoff";
import { startFailureRowLine } from "../../lib/start-failure";
import { planTaskOf, startFailureCodeLine } from "../../lib/start-retry";
import type { Todo } from "../../lib/todos";
import { Icon } from "../../ui/icons";
import { whyNotStarted } from "./Handoff";
import { waitsLabel } from "./Tasks";

export function FailedTaskRow({ rowKey, todo, countsOpenAttr }: { rowKey: string; todo: Todo; countsOpenAttr: { "data-counts-open"?: string } }) {
  const why = useFeed((s) => whyNotStarted(todo, s.plans));
  const task = useFeed((s) => planTaskOf(todo, s.plans));
  return (
    <div data-needs-row={rowKey} {...countsOpenAttr} data-failed-task={todo.id} className="flex flex-col gap-1.5 rounded-control bg-danger-wash px-3 py-2 text-sm">
      <div className="flex min-w-0 items-center gap-2">
        <Icon name="info" className="shrink-0 text-red" />
        <button type="button" onClick={() => openTask(todo.id)} className="min-w-0 flex-1 truncate text-left hover:underline">{todo.title}</button>
        <span className="shrink-0 text-xs text-ink">{waitsLabel(todo, "failed")}</span>
      </div>
      {/* in plain words, by its code first (#10365); the hub's raw error is the task's details' (#8237) */}
      {why && <div className="truncate pl-6 text-xs text-ink">{startFailureCodeLine(task) ?? startFailureRowLine(why)}</div>}
    </div>
  );
}
