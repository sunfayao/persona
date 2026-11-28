import { t } from "../../i18n";
import { openTask } from "../../lib/handoff";
import type { RuleCard } from "../../lib/rule-suggestion";
import { latestReview } from "../../lib/supervision";
import { askOptions, openAsk, type Todo } from "../../lib/todos";
import { BriefingMark } from "./BriefingMark";
import { FailedTaskRow } from "./FailedTaskRow";
import RuleSuggestionLine from "./RuleSuggestionLine";

export function TaskDecision({ todo, rowKey, hideTitle = false }: { todo: Todo; rowKey?: string; hideTitle?: boolean }) {
  const ask = openAsk(todo);
  const review = latestReview(todo.supervision);
  const text = ask?.question || ask?.verdict || review?.reason || review?.note;
  const recommend = ask?.recommend || review?.recommend;
  const options = ask ? askOptions(ask) : review?.options ?? [];
  return (
    <div data-needs-row={rowKey} data-decision-row className="flex min-w-0 flex-col gap-1.5 rounded-control bg-field px-3 py-2.5">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
        {!hideTitle && (
          <button type="button" onClick={() => openTask(todo.id)} className="min-w-0 flex-1 truncate text-left text-meta font-medium hover:underline">
            {todo.title}
          </button>
        )}
        {(!ask || ask.outcome === "decide") && <span className="text-label text-ink-2">{t("assistant-tasks.review_needs_user")}</span>}
      </div>
      {text && <p className="whitespace-pre-wrap text-meta select-text [overflow-wrap:anywhere]">{text}</p>}
      {ask?.background && <p className="text-label text-ink-2 [overflow-wrap:anywhere]">{ask.background}</p>}
      {recommend && <p className="text-label text-ink-2">{t("assistant-brief.recommend", { option: recommend })}</p>}
      {(options.length > 0 || Boolean(ask?.questions?.length)) && (
        <ul className="list-inside list-disc text-label text-ink-2 [overflow-wrap:anywhere]">
          {options.map((option, i) => <li key={i} data-decision-option>{option.label}{option.leads_to && t("assistant-decisionrow.leads_to", { leads: option.leads_to })}</li>)}
          {ask?.questions?.map((question, i) => <li key={i}>{question.question}</li>)}
        </ul>
      )}
      {ask?.for_briefing && <BriefingMark />}
    </div>
  );
}

export type NeedsYouPartProps =
  | { decision: Todo; rowKey?: string; hideTitle?: boolean }
  | { failed: Todo; rowKey: string; countsOpenAttr: { "data-counts-open"?: string } }
  | { rule: RuleCard };

export default function NeedsYouParts(props: NeedsYouPartProps) {
  if ("decision" in props) return <TaskDecision todo={props.decision} rowKey={props.rowKey} hideTitle={props.hideTitle} />;
  if ("failed" in props) return <FailedTaskRow todo={props.failed} rowKey={props.rowKey} countsOpenAttr={props.countsOpenAttr} />;
  return <RuleSuggestionLine of={props.rule} />;
}
