import { useId, useState, type MouseEvent } from "react";
import { t } from "../../i18n";
import { sessionAgentName } from "../../lib/agent-kind";
import { ago, baseName, sessionTitle, stateOf } from "../../lib/format";
import { href, onLinkClick } from "../../lib/router";
import type { Session } from "../../lib/types";
import { SessionMark } from "../../ui/AgentMark";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { SESSION_LIST_METRICS } from "../../ui/list-metrics";
import { StatusMark } from "../../ui/StatusMark";

const copy = (key: string) => t(`work-session-browser.${key}`);

/** One session of the History list: its state, title, last activity and its computer, project and Agent. A plain
 *  click goes to `onSelect` when given; a modified click keeps the session's own link. Its parent is the
 *  `@container/work-sessions` that decides when the details fit on one line. */
export function WorkSessionRow({ session, name, active, onSelect, now }: {
  session: Session; name: string; active: boolean; onSelect?: (session: Session) => void; now: number;
}) {
  const [expanded, setExpanded] = useState(false);
  const summaryId = useId();
  const to = { name: "session" as const, mid: session.machine_id, sid: session.session_id };
  const select = (event: MouseEvent<HTMLAnchorElement>) => {
    if (!onSelect) { onLinkClick(event, to); return; }
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onSelect(session);
  };
  const project = session.space_name || baseName(session.repo_root || session.cwd) || copy("unknown_project");
  const preview = session.activity || session.last_text;
  return <div className={cx("flex min-w-0 items-center rounded-control ps-2 pe-2 hover:bg-hover", active && "bg-accent-tint")}>
    <a href={href(to)} onClick={select} aria-current={active ? "true" : undefined}
      className="flex min-h-11 min-w-0 flex-1 items-start gap-2 rounded-control py-1.5 focus-visible:outline-accent">
    <span className="flex shrink-0 items-center gap-1 pt-0.5">
      <StatusMark state={stateOf(session)} size="sm" />
      <SessionMark s={session} decorative size={SESSION_LIST_METRICS.icon} />
    </span>
    <span className="min-w-0 flex-1">
      <span className="flex min-w-0 items-baseline gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-ink">{sessionTitle(session)}</span>
        {session.updated_at && <time className="shrink-0 text-xs text-ink-3" dateTime={session.updated_at}>{ago(session.updated_at, now, "narrow")}</time>}
      </span>
      {preview && <span className={cx("text-xs text-ink-2", expanded ? "block break-words" : "hidden truncate @sm/work-sessions:block")}>{preview}</span>}
      <span id={summaryId} className={cx("flex min-w-0 items-baseline gap-x-1 text-xs text-ink-3",
        expanded ? "flex-wrap break-words" : "overflow-hidden @sm/work-sessions:flex-wrap")}>
        {([["device", name], ["project", project], ["harness", sessionAgentName(session)]] as const).map(([dimension, label], i) =>
          <span key={dimension} className={cx("inline-flex min-w-0 gap-1", expanded && "max-w-full")}>
            {i > 0 && <span aria-hidden="true">·</span>}
            <span className={expanded ? "min-w-0 break-words" : "min-w-0 truncate"} title={label}>
              <span className="sr-only">{copy(dimension)}: </span>{label}
            </span>
          </span>
        )}
      </span>
    </span>
    </a>
    <button type="button" aria-label={copy("details")} aria-controls={summaryId} aria-expanded={expanded}
      className="min-h-8 shrink-0 rounded-control px-2 text-ink-3 hover:bg-surface [@media(pointer:coarse)]:min-h-11 @sm/work-sessions:hidden"
      onClick={() => setExpanded((open) => !open)}>
      <Icon name="chevron" size={12} className={expanded ? "rotate-180" : ""} />
    </button>
  </div>;
}
