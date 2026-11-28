import { t } from "../../i18n";
import { href, onLinkClick } from "../../lib/router";
import { stateName } from "../../lib/state-labels";
import { useStore } from "../../lib/store";
import { useTodoLookup } from "../../lib/todo-owner-needs";
import { TODO_STATUS_INFO, TODO_TONE_CLASS, shownStatus, todoStatus, todoStatusLabel } from "../../lib/todos";
import type { Session } from "../../lib/types";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { TodoStageBar } from "../TodoStageBar";

/** A linked to-do's title, state and stage beneath the Agent's header. */
export function TodoStrip({ session }: { session: Session }) {
  const link = session.todo ?? session.completed_todo;
  if (!link) return null;
  return <LinkedTodoStrip link={link} completed={!session.todo} />;
}

function LinkedTodoStrip({ link, completed }: { link: { id: string; title: string }; completed: boolean }) {
  const todo = useStore((s) => s.todos.find((x) => x.id === link.id));
  const lookup = useTodoLookup();
  const status = todo && todoStatus(todo, lookup);
  const title = todo?.title.trim() || link.title;
  const tone = todo && status ? TODO_TONE_CLASS[TODO_STATUS_INFO[shownStatus(todo, status)].tone] : undefined;
  const label = todo && status ? todoStatusLabel(todo, status) : completed ? stateName("done") : t("chat-todostrip.no_detail");
  const to = { name: "todos", id: link.id } as const;

  return (
    <section data-todo-strip aria-label={t("chat-todostrip.region", { title })} className="shrink-0 border-b border-line bg-page">
      <div className="mx-auto flex min-h-11 w-full max-w-[820px] items-center gap-2 px-5 sm:px-8">
        <Icon name="todo" size={14} className="shrink-0 text-ink-3" />
        <a href={href(to)} onClick={(e) => onLinkClick(e, to)} title={title}
          className="flex min-h-11 min-w-0 items-center rounded-control text-meta font-medium text-accent-ink hover:underline">
          <span className="truncate">{title}</span>
        </a>
        <span data-todo-strip-status className={cx("shrink-0 rounded-chip px-2 py-0.5 text-label font-medium", tone ?? "text-ink-3")}>
          {label}
        </span>
        {todo && <TodoStageBar compact stage={todo.stage} stageKind={todo.stage_kind} done={todo.stage_done} className="ml-auto max-sm:hidden" />}
      </div>
    </section>
  );
}
