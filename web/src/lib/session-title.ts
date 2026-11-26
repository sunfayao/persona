import type { Session } from "./types";

type StoredTitle = Pick<Session, "title" | "title_manual" | "title_source">;
const contextStart = /^<persona-context\s+kind="/;
const contextBlock = /^<persona-context\b[^>]*>[\s\S]*?<\/persona-context>/;
const decode = (s: string) => s.replace(/&quot;/g, '"').replace(/&#39;|&apos;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");

/** A native thread can be named after a clipped first message with context attachments.
 * Only unwrap the leading transport blocks. The quoted body is never a display name. */
export function readableSessionTitle(session: StoredTitle): string {
  const title = session.title?.trim() ?? "";
  if (session.title_manual || session.title_source === "manual") return title;
  if (title === "<persona-context kind=") return "";
  if (!contextStart.test(title)) return title;
  let rest = title;
  let contextName = "";
  while (contextStart.test(rest)) {
    const headEnd = rest.indexOf(">");
    const head = headEnd < 0 ? rest : rest.slice(0, headEnd);
    const attribute = /\stitle="([^"]*)"/.exec(head);
    if (!contextName && attribute) contextName = decode(attribute[1]).trim();
    const block = contextBlock.exec(rest);
    if (!block) return contextName;
    rest = rest.slice(block[0].length).trimStart();
  }
  return rest.split("\n", 1)[0].trim() || contextName;
}
