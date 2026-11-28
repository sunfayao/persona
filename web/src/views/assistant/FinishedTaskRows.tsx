// The Finished tray's rows (#10922): drawn when the tray opens, in the chunk the Parked tray's rows load in
// (ParkedTaskRow.tsx re-exports them, so they add no lazy entry of their own), so neither the rows' list nor the tray's empty line is part of the first screen (startup-preload's FIRST_SCREEN_BUDGET).
import type { FinishedItem } from "../../lib/assistant-entries";
import { t } from "../../i18n";
import { ProgressTaskRow } from "./Tasks";

/** How a task ended, instead of a card in the chat (#8237): what finished in the last 24 hours, newest first, seen or not;
 *  one nobody opened yet says Just finished (#10922). Its line when nothing is left, the tray kept open past the last. */
export function FinishedTaskRows({ items, conversation }: { items: readonly FinishedItem[]; conversation: string }) {
  if (!items.length) return <p className="px-3 py-3 text-sm text-ink-2">{t("assistant-finishedtaskrows.empty")}</p>;
  return <>{items.map((item) => <ProgressTaskRow key={item.key} card={item.card} conversation={conversation} countsOpen={false} fresh={item.fresh} />)}</>;
}
