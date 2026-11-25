// Where a conversation's page is: the assistant's conversations all open as the main conversation (`/`), an agent's is
// /s/<machine>/<sid>.
import type { Route } from "./router";
import type { AssistantInfo, Session } from "./types";

/** A conversation's page: the assistant's is the main conversation (the only one the web app shows), an agent's is
 *  /s/<machine>/<sid>. */
export function conversationRoute(mid: string, sid: string, sessions: Record<string, Session[]>, assistant: AssistantInfo | null): Route {
  const s = (sessions[mid] ?? []).find((x) => x.session_id === sid);
  const own = assistant?.machine_id === mid && assistant.conversations.some((c) => c.session_id === sid);
  return s?.assistant || own ? { name: "persona" } : { name: "session", mid, sid };
}
