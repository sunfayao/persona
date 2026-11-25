// The assistant's conversation titles, and sessions newest first. Pure functions, tested without a DOM.
import type { Session } from "../../lib/types";
import { t } from "../../i18n";

/** What a row calls it: an untitled one (empty, or Claude Code's placeholder) is "New chat". The main conversation's title: it has none of its own (the hub refuses renaming it), whatever its first message was (#1313). */
export const MAIN_TITLE = "Persona";

export function conversationTitle(s: Pick<Session, "title">, isMain = false): string {
  if (isMain) return MAIN_TITLE;
  const title = readableTitle(s.title?.trim() ?? "");
  return !title || title === "Claude Code" || title === "assistant" ? t("assistant-conversations.untitled") : title;
}

/** A chat started from picked context is titled by its first message, a `<persona-context …>` tag (docs/protocol/protocol.md
 *  "Tags in messages"): what was typed after the tag, else the tag's own title, entities decoded; empty when it has
 *  neither. Any other title as it is. */
const CONTEXT_BLOCK = /^\s*<persona-context\b[^>]*>[\s\S]*?<\/persona-context>/;
const CONTEXT_OPEN = /^<persona-context\b([^>]*)/;
const decodeAttr = (s: string) => s.replace(/&quot;/g, '"').replace(/&#39;|&apos;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");

function readableTitle(title: string): string {
  const open = CONTEXT_OPEN.exec(title);
  if (!open) return title;
  let rest = title;
  for (let m = CONTEXT_BLOCK.exec(rest); m; m = CONTEXT_BLOCK.exec(rest)) rest = rest.slice(m[0].length);
  const typed = rest === title ? "" : rest.trim().split("\n")[0].trim();
  return typed || decodeAttr(/\btitle="([^"]*)"/.exec(open[1])?.[1] ?? "").trim();
}

function time(s: Session): number {
  const at = s.updated_at ? Date.parse(s.updated_at) : NaN;
  return Number.isFinite(at) && at > 86_400_000 ? at : 0;
}

/** Newest first, a fixed order for equal times. */
export function newestFirst<T extends Session>(list: T[]): T[] {
  return list.slice().sort((a, b) => time(b) - time(a) || a.session_id.localeCompare(b.session_id));
}
