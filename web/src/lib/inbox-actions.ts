// Merge inbox rows within one manager, with Undo.
import { api, errorText } from "./api";
import { applyInboxChange, reloadInbox, type InboxItem } from "./inbox";
import { itemHeaders, otherManager } from "./manager-choice";
import { t } from "../i18n";
import { toast } from "../ui/atoms";

/** The managers' rows apart: a request goes to one manager. */
function byManager(items: InboxItem[]): Map<string, InboxItem[]> {
  const m = new Map<string, InboxItem[]>();
  for (const it of items) {
    const k = otherManager(it.manager) ?? "";
    m.set(k, [...(m.get(k) ?? []), it]);
  }
  return m;
}

/** Rows that can be merged: two or more open rows of one manager. */
export function canMerge(items: InboxItem[]): boolean {
  return items.length >= 2 && byManager(items).size === 1;
}

interface MergeAnswer {
  item: InboxItem;
  members: string[];
  undo: { id: string };
}

/** "Merge": the rows become one (the newest row, its sources' later messages follow it); the members leave the list at once the hub
 * answers, and the toast has Undo. Resolves to whether it merged. */
export async function mergeItems(items: InboxItem[]): Promise<boolean> {
  if (!canMerge(items)) return false;
  const manager = otherManager(items[0].manager);
  try {
    const r = await api<MergeAnswer>("/inbox/merge", { method: "POST", json: { items: items.map((it) => ({ id: it.id, version: it.version })) }, headers: itemHeaders(manager) });
    applyInboxChange([r.item], r.members ?? [], manager);
    toast(t("lib-inbox-actions.merged", { count: (r.members?.length ?? 0) + 1 }), "ok", { label: t("lib-inbox-actions.undo"), run: () => void unmergeItems(r.undo.id, manager) });
    return true;
  } catch (e) {
    toast(t("lib-inbox-actions.merge_failed", { error: errorText(e) }), "error");
    void reloadInbox();
    return false;
  }
}

/** Undo of a merge: the members get their rows back; the list is read again for them (the hub lists them now). */
async function unmergeItems(id: string, manager: string | undefined): Promise<void> {
  try {
    const r = await api<{ changed: InboxItem[] }>("/inbox/merge/undo", { method: "POST", json: { id }, headers: itemHeaders(manager) });
    applyInboxChange(r.changed ?? [], [], manager);
    void reloadInbox();
    toast(t("lib-inbox-actions.unmerged"), "ok");
  } catch (e) {
    toast(t("lib-inbox-actions.unmerge_failed", { error: errorText(e) }), "error");
  }
}
