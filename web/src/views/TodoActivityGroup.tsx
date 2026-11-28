import { ago, clock, duration } from "../lib/format";
import { t } from "../i18n";
import type { ActivityGroup, BackgroundRun, RunOutcome } from "../lib/todo-activity";
import { cx } from "../ui/atoms";

// The classes a node names (proto.LifecycleActivityClasses); a newer node's class is counted but not named.
const CLASSES = ["ci", "pr_create", "pr_merge", "release", "deploy", "test", "lint", "build", "install", "push", "commit", "sync", "diff", "simulator", "waiting", "search_code", "read_files"];

function countText(outcome: RunOutcome, count: number): string {
  switch (outcome) {
    case "finished": return t("todos.lifecycle_count_finished", { count });
    case "failed": return t("todos.lifecycle_count_failed", { count });
    case "cancelled": return t("todos.lifecycle_count_cancelled", { count });
    case "running": return t("todos.lifecycle_count_running", { count });
  }
}

function runText(run: BackgroundRun): string {
  switch (run.outcome) {
    case "finished": return t("todos.lifecycle_run_finished");
    case "failed": return t("todos.lifecycle_run_failed");
    case "running": return t("todos.lifecycle_run_running");
    case "cancelled":
      if (run.ended === "killed") return t("todos.lifecycle_run_killed");
      if (run.ended === "stopped") return t("todos.lifecycle_run_stopped");
      return t("todos.lifecycle_run_cancelled");
  }
}

const className = (c: string) => (CLASSES.includes(c) ? t(`todos.lifecycle_class_${c}`) : undefined);

/** One session's background work of one turn: a row that opens to its runs. A failed or cancelled run is in the row's summary. */
export function ActivityGroupRow({ group, actor, open, onToggle }: { group: ActivityGroup; actor?: string; open: boolean; onToggle: () => void }) {
  const outcomes = (["failed", "cancelled", "running", "finished"] as const).filter((o) => group.counts[o] > 0);
  const classes = group.classes.flatMap(([c, n]) => { const name = className(c); return name ? [`${name} × ${n}`] : []; });
  const from = clock(group.from), to = clock(group.to);
  return (
    <li data-testid="todo-activity-group" className="text-label text-ink-2">
      <button type="button" aria-expanded={open} onClick={onToggle} className="focus-ring-inset flex w-full flex-wrap items-baseline gap-x-2 rounded-control text-left hover:bg-hover">
        <span>{t("todos.lifecycle_background_group", { count: group.runs.length })}</span>
        {actor && <span className="text-ink-3">{actor}</span>}
        <time dateTime={group.to} className="text-ink-3">{ago(group.to)}</time>
        {from !== to && <span className="text-ink-3">{from}–{to}</span>}
        {outcomes.map((o) => <span key={o} className={cx(o === "failed" ? "text-red" : o === "finished" ? "text-ink-3" : "text-ink-2")}>{countText(o, group.counts[o])}</span>)}
        {classes.length > 0 && <span className="text-ink-3">{classes.join(" · ")}</span>}
        {group.observed && <span className="text-ink-3">{t("todos.lifecycle_observed")}</span>}
      </button>
      {open && (
        <ol className="mt-1 flex flex-col gap-1 border-l border-line pl-3">
          {group.runs.map((run) => (
            <li key={run.key} className="flex flex-wrap items-baseline gap-x-2" data-testid="todo-activity-run">
              <span className={run.outcome === "failed" ? "text-red" : run.outcome === "finished" ? "text-ink-3" : undefined}>{runText(run)}</span>
              {run.activity && className(run.activity) && <span className="text-ink-3">{className(run.activity)}</span>}
              {run.durationMs !== undefined && <span className="text-ink-3">{duration(run.durationMs)}</span>}
              <time dateTime={run.startedAt} className="text-ink-3">{clock(run.startedAt)}</time>
            </li>
          ))}
        </ol>
      )}
    </li>
  );
}
