// The to-do strip's details (TodoStrip.tsx), loaded on its first open: the stage bar, the "done when" criteria with
// their marks, the other Agents on the to-do, its latest decisions, and the steering actions. Each
// action only prepares a message (lib/todo-steer.ts) in a composer, for the owner to edit and send there: this Agent's
// own, or the main conversation's to hand supervision of the to-do over to the assistant.
import { useEffect, useId, useState } from "react";
import { t } from "../../i18n";
import { errorText } from "../../lib/api";
import { sendDraft, storedDraft } from "../../lib/drafts";
import { sessionTitle } from "../../lib/format";
import { navigate } from "../../lib/router";
import { continueMessage, nextCriterionMessage, reportMessage, supervisionMessage, type SteerTodo } from "../../lib/todo-steer";
import { unmet, type StripCriterion, type TodoStripModel } from "../../lib/todo-strip";
import { fetchTodoDecisions, isLegacySpacesRouteError, type TodoDecision } from "../../lib/todos";
import type { Session } from "../../lib/types";
import { Button, cx, toast } from "../../ui/atoms";
import { personaTarget } from "../link/handoff";
import { CriterionMark } from "../TodoCriterionMark";
import { TodoStageBar } from "../TodoStageBar";

/** How many of its decisions in effect the details list, newest first. */
const DECISIONS_SHOWN = 3;

type OpenStrip = Extract<TodoStripModel, { form: "open" }>;

export function TodoStripDetail({ model, session, canSteer, onSteered }: { model: OpenStrip; session: Session; canSteer: boolean; onSteered: () => void }) {
  const { todo } = model;
  const facts: SteerTodo = { id: model.id, title: model.title, stage: model.stage, criteria: model.criteria };
  // into this Agent's composer, in place of what is there: the composer offers Undo for words it replaces
  const prepare = (text: string) => {
    sendDraft(`${session.machine_id}/${session.session_id}`, { type: "replace", text });
    onSteered();
  };
  // into the main conversation's composer, after what the owner left there, and the page moves to it
  const askMain = () => {
    const target = personaTarget();
    const text = supervisionMessage(facts, { title: sessionTitle(session), machine_id: session.machine_id, session_id: session.session_id });
    if (!storedDraft(target.key).includes(text)) sendDraft(target.key, { type: "append", text });
    target.go();
    toast(t("chat-todostripdetail.main_prepared"), "ok");
  };
  const criteriaId = useId();
  return (
    <div data-todo-strip-detail className="flex flex-col gap-3 pt-1 text-meta">
      <TodoStageBar stage={todo.stage} stageKind={todo.stage_kind} prURL={todo.pr_url} />
      {model.criteria.length > 0 && (
        <section aria-labelledby={criteriaId} className="rounded-control bg-inset p-3 shadow-hairline">
          <h3 id={criteriaId} className="text-label font-semibold text-ink-2">{t("chat-todostripdetail.criteria")}</h3>
          <ul className="mt-1.5 flex flex-col gap-1">
            {model.criteria.map((c) => (
              <Criterion key={c.n} c={c} onNext={canSteer && unmet(c) ? () => prepare(nextCriterionMessage(facts, c)) : undefined} />
            ))}
          </ul>
        </section>
      )}
      {model.others.length > 0 && (
        <section className="flex flex-col gap-1">
          <h3 className="text-label font-semibold text-ink-2">{t("chat-todostripdetail.others")}</h3>
          <ul className="flex flex-wrap gap-1.5">
            {model.others.map((a) => (
              <li key={`${a.machine_id}/${a.session_id}`}>
                <Button tone="secondary" size="sm" icon="agent" onClick={() => navigate({ name: "session", mid: a.machine_id, sid: a.session_id })}>
                  <span className="max-w-[16rem] truncate">{a.title?.trim() || t("chat-todostripdetail.agent_untitled")}</span>
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      <Decisions todoId={model.id} />
      <div className="flex flex-wrap items-center gap-1.5">
        {canSteer && (
          <>
            <Button tone="secondary" size="sm" icon="play" aria-label={t("chat-todostripdetail.continue_label")} onClick={() => prepare(continueMessage(facts))}>
              {t("chat-todostripdetail.continue")}
            </Button>
            <Button tone="secondary" size="sm" icon="list" aria-label={t("chat-todostripdetail.report_label")} onClick={() => prepare(reportMessage(facts))}>
              {t("chat-todostripdetail.report")}
            </Button>
          </>
        )}
        <Button tone="quiet" size="sm" icon="spark" aria-label={t("chat-todostripdetail.ask_main_label")} onClick={askMain}>
          {t("chat-todostripdetail.ask_main")}
        </Button>
        <Button tone="quiet" size="sm" icon="arrowUpRight" onClick={() => navigate({ name: "todos", id: model.id })}>
          {t("chat-todostrip.open")}
        </Button>
      </div>
    </div>
  );
}

function Criterion({ c, onNext }: { c: StripCriterion; onNext?: () => void }) {
  return (
    <li data-criterion={c.n} data-mark={c.mark} className="flex min-h-7 items-start gap-2">
      <CriterionMark mark={c.mark} className="mt-[3px]" />
      <span className={cx("min-w-0 flex-1 leading-[1.5] [overflow-wrap:anywhere]", c.mark === "met" ? "text-ink-2" : "text-ink")}>{c.text}</span>
      {onNext && (
        <Button tone="quiet" size="sm" aria-label={t("chat-todostripdetail.do_next_label", { text: c.text })} onClick={onNext}>
          {t("chat-todostripdetail.do_next")}
        </Button>
      )}
    </li>
  );
}

/** Its decisions in effect, the newest few; a failed read says so with Retry, never an empty space. */
function Decisions({ todoId }: { todoId: string }) {
  const [read, setRead] = useState<{ list?: TodoDecision[]; error?: string }>({});
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    fetchTodoDecisions(todoId).then(
      (list) => live && setRead({ list }),
      // a hub without decisions has none to show
      (e) => live && setRead(isLegacySpacesRouteError(e) ? { list: [] } : { error: errorText(e) }),
    );
    return () => {
      live = false;
    };
  }, [todoId, attempt]);
  if (read.error) {
    return (
      <p role="alert" className="flex flex-wrap items-center gap-2 text-label text-red">
        {t("chat-todostripdetail.decisions_failed")}
        <Button tone="quiet" size="sm" icon="retry" onClick={() => { setRead({}); setAttempt(attempt + 1); }}>
          {t("chat-todostripdetail.retry")}
        </Button>
      </p>
    );
  }
  const shown = (read.list ?? []).filter((d) => !d.superseded_by).slice(-DECISIONS_SHOWN).reverse();
  if (!shown.length) return null;
  return (
    <section data-todo-strip-decisions className="flex flex-col gap-1">
      <h3 className="text-label font-semibold text-ink-2">{t("chat-todostripdetail.decisions")}</h3>
      <ul className="flex flex-col gap-1">
        {shown.map((d) => (
          <li key={d.id} className="leading-[1.5] [overflow-wrap:anywhere]">
            {d.question && <span className="text-ink-2">{d.question} </span>}
            <span className="text-ink">{d.choice}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
