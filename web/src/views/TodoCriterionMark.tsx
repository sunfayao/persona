// A "done when" criterion's mark, as the to-do's review checklist (Todos.tsx ReviewChecklist) and the to-do strip in an
// Agent's chat (chat/TodoStripDetail.tsx) draw it: a shape per state (check, cross, clock, dash) and its word for
// screen readers, so a state never rests on its colour. Imported only by lazy code: it is not on the first screen.
import { t } from "../i18n";
import type { CheckMark } from "../lib/supervision";
import { cx } from "../ui/atoms";
import { Icon } from "../ui/icons";

const MARK: Record<CheckMark, { icon: "check" | "x" | "clock" | "minus"; className: string; label: () => string }> = {
  met: { icon: "check", className: "text-green", label: () => t("assistant-tasks.review_met") },
  missing: { icon: "x", className: "text-red", label: () => t("assistant-tasks.review_missing") },
  open: { icon: "clock", className: "text-ink-3", label: () => t("assistant-tasks.review_open") },
  // added after the acceptance, which still stands (#3156): nothing waits on it
  unchecked: { icon: "minus", className: "text-ink-3", label: () => t("assistant-tasks.review_unchecked") },
};

export function CriterionMark({ mark, className = "" }: { mark: CheckMark; className?: string }) {
  const m = MARK[mark];
  return (
    <span role="img" aria-label={m.label()} title={m.label()} className={cx("shrink-0", m.className, className)}>
      <Icon name={m.icon} size={13} stroke={2.4} />
    </span>
  );
}
