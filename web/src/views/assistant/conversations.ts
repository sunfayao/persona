// The assistant's conversation titles, and sessions newest first. Pure functions, tested without a DOM.
import { readableSessionTitle } from "../../lib/session-title";
import type { Session } from "../../lib/types";
import { t } from "../../i18n";

/** What a row calls it: an untitled one (empty, or Claude Code's placeholder) is "New chat". The main conversation's title: it has none of its own (the hub refuses renaming it), whatever its first message was (#1313). */
const MAIN_TITLE = "Persona";

export function conversationTitle(s: Pick<Session, "title" | "title_manual" | "title_source">, isMain = false): string {
  if (isMain) return MAIN_TITLE;
  const title = readableSessionTitle(s);
  return !title || title === "Claude Code" || title === "assistant" ? t("assistant-conversations.untitled") : title;
}
