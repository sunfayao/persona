// The to-do strip (lib/todo-strip.ts): pinned under an Agent chat's header while its session is linked to a to-do, one
// quiet line saying which to-do, where it stands (its stage) and the one most useful next thing. A click opens its
// details and the steering actions (TodoStripDetail, a chunk of its own), which push the chat down rather than cover
// it. Nothing at all for a session with no to-do. Amber only when the to-do needs the owner, by the sidebar badge's rule.
import { lazy, Suspense, useId, useMemo, useState } from "react";
import { t } from "../../i18n";
import { navigate } from "../../lib/router";
import { stateName } from "../../lib/state-labels";
import { useStore } from "../../lib/store";
import { useOwnerNeeds, useTodoMinute } from "../../lib/todo-owner-needs";
import { todoStrip, type StripNext, type TodoStripModel } from "../../lib/todo-strip";
import type { Session } from "../../lib/types";
import { Button, Collapse, cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { TodoStageBar } from "../TodoStageBar";

const TodoStripDetail = lazy(() => import("./TodoStripDetail").then((m) => ({ default: m.TodoStripDetail })));

/** The next thing in words: what the owner is asked, what the work waits on, or the first criterion not met. */
export function nextWords(next: StripNext): string {
  switch (next.kind) {
    case "owner":
      switch (next.need) {
        case "failed_start":
          return t("chat-todostrip.owner_failed_start");
        case "agent_asks":
          return t("chat-todostrip.owner_agent_asks");
        case "delivery_ok":
          return t("chat-todostrip.owner_delivery_ok");
      }
      return next.text ? t("chat-todostrip.owner_decides_text", { text: next.text }) : t("chat-todostrip.owner_decides");
    case "wait":
      return next.on === "review" ? t("chat-todostrip.wait_review") : next.on === "merge" ? t("chat-todostrip.wait_merge") : t("chat-todostrip.wait_deploy");
    case "criterion":
      return t("chat-todostrip.next_criterion", { text: next.criterion.text });
    case "all_met":
      return t("chat-todostrip.all_met");
    case "unattended":
      return t("chat-todostrip.unattended");
  }
  return t("chat-todostrip.in_progress");
}

/** The strip of an Agent chat's session; nothing when it is linked to no to-do. */
export function TodoStrip({ session, canSteer }: { session: Session; canSteer: boolean }) {
  const link = session.todo ?? session.completed_todo;
  if (!link) return null;
  return <LinkedTodoStrip session={session} link={link} completed={!session.todo} canSteer={canSteer} />;
}

function LinkedTodoStrip({ session, link, completed, canSteer }: { session: Session; link: { id: string; title: string }; completed: boolean; canSteer: boolean }) {
  const todo = useStore((s) => s.todos.find((x) => x.id === link.id));
  const need = useOwnerNeeds().get(link.id);
  const minute = useTodoMinute();
  const model = useMemo(
    () => todoStrip(link, todo, need, session, minute, completed),
    [link, todo, need, session, minute, completed],
  );
  return <TodoStripView model={model} session={session} canSteer={canSteer} />;
}

/** The strip drawn from its model (the tests render this). */
export function TodoStripView({ model, session, canSteer }: { model: TodoStripModel; session: Session; canSteer: boolean }) {
  const [open, setOpen] = useState(false);
  const detailId = useId();
  const openTodo = () => navigate({ name: "todos", id: model.id });
  return (
    <section data-todo-strip={model.form} aria-label={t("chat-todostrip.region", { title: model.title })} className="shrink-0 border-b border-line bg-page">
      <div className="mx-auto w-full max-w-[820px] px-5 sm:px-8">
        {model.form === "open" ? (
          <>
            <button
              type="button"
              aria-expanded={open}
              aria-controls={detailId}
              title={open ? t("chat-todostrip.collapse") : t("chat-todostrip.expand")}
              onClick={() => setOpen(!open)}
              className="flex min-h-11 w-full min-w-0 items-center gap-2 rounded-control py-1.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              <Icon name="todo" size={14} className={cx("shrink-0", model.needsYou ? "text-attention" : "text-ink-3")} />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5 sm:flex-row sm:items-center sm:gap-2">
                <span data-todo-strip-title className="min-w-0 truncate text-meta font-medium text-ink sm:max-w-[45%] sm:shrink-0">
                  {model.title}
                </span>
                <span className="flex min-w-0 items-center gap-1.5 text-label">
                  {model.needsYou && (
                    <span data-todo-strip-needs className="inline-flex shrink-0 items-center gap-1 font-medium text-attention">
                      <span aria-hidden className="size-1.5 rounded-full bg-attention ring-2 ring-attention/25" />
                      {stateName("needs_you")}
                    </span>
                  )}
                  <span data-todo-strip-next className="min-w-0 truncate text-ink-2">
                    {nextWords(model.next)}
                  </span>
                </span>
              </span>
              <TodoStageBar compact stage={model.stage} className="max-sm:hidden" />
              <Icon name="chevron" size={14} className={cx("shrink-0 text-ink-3 transition-transform duration-150", open && "rotate-180")} />
            </button>
            <Collapse open={open} lazy>
              <div id={detailId} className="max-h-[min(50vh,420px)] overflow-y-auto pb-3">
                <Suspense fallback={null}>
                  <TodoStripDetail model={model} session={session} canSteer={canSteer} onSteered={() => setOpen(false)} />
                </Suspense>
              </div>
            </Collapse>
          </>
        ) : (
          <div className="flex min-h-11 min-w-0 items-center gap-2 py-1.5">
            {model.form === "finished" ? (
              <Icon name={model.how === "done" ? "check" : "x"} size={14} className={cx("shrink-0", model.how === "done" ? "text-green" : "text-ink-3")} />
            ) : (
              <Icon name="todo" size={14} className="shrink-0 text-ink-3" />
            )}
            <span className={cx("min-w-0 truncate text-meta text-ink-2", model.form === "finished" && model.how === "cancelled" && "line-through")}>{model.title}</span>
            <span className="shrink-0 text-label text-ink-3">
              {model.form === "missing" ? t("chat-todostrip.no_detail") : model.how === "done" ? stateName("done") : t("lib-todos.review_cancelled")}
            </span>
            <span className="flex-1" />
            <Button tone="quiet" size="sm" icon="arrowUpRight" onClick={openTodo}>
              {t("chat-todostrip.open")}
            </Button>
          </div>
        )}
      </div>
    </section>
  );
}
