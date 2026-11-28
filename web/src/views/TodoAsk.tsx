import { t } from "../i18n";
import { stateName } from "../lib/state-labels";
import { askTone, openAsk, type Todo, type TodoAsk } from "../lib/todos";
import { cx } from "../ui/atoms";
import { Icon, type IconName } from "../ui/icons";

const TONE: Record<ReturnType<typeof askTone>, string> = { attention: "text-attention", red: "text-red", green: "text-green" };
const ICON: Record<ReturnType<typeof askTone>, IconName> = { attention: "hand", red: "info", green: "check" };

export function TodoAskLine({ todo, className }: { todo: Todo; className?: string }) {
  const ask = openAsk(todo);
  return ask ? <AskLine ask={ask} className={className} /> : null;
}

function AskLine({ ask, className }: { ask: TodoAsk; className?: string }) {
  const tone = askTone(ask);
  const recommend = ask.recommend ? t("assistant-brief.recommend", { option: ask.recommend }) : "";
  return (
    <div data-testid="todo-ask" data-ask={ask.outcome || "deployed"} className={cx("flex min-w-0 items-start gap-2 text-[12px]", className)}>
      {/* the verdict starts with its own mark (✅ ⛔ 🤔, as its Lark card says it): no icon beside it */}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={cx("truncate font-medium", TONE[tone])} title={ask.verdict}>
          {ask.verdict}
        </span>
        {recommend && (
          <span className="truncate text-ink-2" title={recommend}>
            {recommend}
          </span>
        )}
        {/* held for the briefing (#5803): why it did not ring; what that means on hover, written out on the to-do's page */}
        {ask.for_briefing === true && (
          <span data-for-briefing="mark" title={t("todos.ask_held_hint")} className="flex min-w-0 items-center gap-1 text-ink-2">
            <Icon name="clock" size={11} className="shrink-0" />
            <span className="truncate">{t("todos.ask_held")}</span>
          </span>
        )}
      </span>
    </div>
  );
}

/** The ask on a to-do's page, to read: what it waits on in full. */
export function TodoAskPanel({ todo }: { todo: Todo }) {
  const ask = openAsk(todo);
  return ask ? <AskPanel ask={ask} /> : null;
}

function AskPanel({ ask }: { ask: TodoAsk }) {
  const tone = askTone(ask);
  const decide = ask.outcome === "decide";
  return (
    <section
      aria-label={stateName("needs_you")}
      data-testid="todo-ask-panel"
      data-ask={ask.outcome || "deployed"}
      className={cx("mt-4 min-w-0 rounded-control bg-inset p-3.5 shadow-hairline", decide && "ring-1 ring-attention/40")}
    >
      <div className="flex items-center gap-2">
        <Icon name={ICON[tone]} size={13} stroke={2.2} className={TONE[tone]} />
        <span className="text-[12.5px] font-semibold text-ink-2">{stateName("needs_you")}</span>
      </div>
      <p className={cx("mt-2 text-[13.5px] font-medium leading-[1.55] select-text [overflow-wrap:anywhere]", TONE[tone])}>{ask.verdict}</p>
      {ask.recommend && <p className="mt-1 text-[13px] leading-[1.55] text-ink select-text [overflow-wrap:anywhere]">{t("assistant-brief.recommend", { option: ask.recommend })}</p>}
    </section>
  );
}
