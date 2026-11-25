import { t } from "../i18n";
import type { Injected, Item, MessageSource } from "./types";

const LEADING_CONTEXT = /^\s*<persona-context\b([^>]*)>([\s\S]*?)<\/persona-context>\s*/;
// The hub's canonical relay template, in English; the LEGACY ones are the Chinese it wrote before (old transcripts).
const RELAY_TITLE = /\btitle="From the conversation “(.*?)”"/u;
const RELAY_BODY = /^The user did not send this message: another Persona conversation \(([^\s()]+)\) sent it with (conversation_send|conversation_new), hop ([0-9]+) of at most ([0-9]+)\.\nDo what it says\. To tell that conversation the result, send it back to ([^\s;]+) with conversation_send; do not reply just to acknowledge, so the two conversations do not message back and forth\.$/u;
const LEGACY_RELAY_TITLE = /\btitle="来自对话「(.*?)」"/u; // i18n-ignore: canonical relay template
const LEGACY_RELAY_BODY = /^这条消息不是用户本人发的，是 Persona 的另一段对话（([^\s（）]+)）用 (conversation_send|conversation_new) 发来的，第 ([0-9]+) 跳（最多 ([0-9]+) 跳）。\n按里面说的做。要把结果告诉那段对话就用 conversation_send 发回 ([^\s；]+)；只是确认收到就不要回，免得两段对话来回发。$/u; // i18n-ignore: canonical relay template

function* leadingContexts(raw: string): Generator<{ attrs: string; text: string }> {
  let text = raw;
  for (let match = LEADING_CONTEXT.exec(text); match; match = LEADING_CONTEXT.exec(text)) {
    yield { attrs: match[1], text: match[2].trim() };
    text = text.slice(match[0].length);
  }
}

function sourceMetadata(raw: string): { found: boolean; source?: MessageSource } {
  for (const context of leadingContexts(raw)) {
    if (!/\bkind="message-source"/.test(context.attrs)) continue;
    try {
      const data: unknown = JSON.parse(context.text);
      if (!data || typeof data !== "object" || Array.isArray(data)) return { found: true };
      const meta = data as Record<string, unknown>;
      if (meta.author === "user" || typeof meta.kind !== "string" || !meta.kind.trim()) return { found: true };
      return { found: true, source: {
        kind: meta.kind,
        ...(typeof meta.conversation_id === "string" ? { conversation_id: meta.conversation_id } : {}),
        ...(typeof meta.title === "string" ? { title: meta.title } : {}),
      } };
    } catch { return { found: true }; }
  }
  return { found: false };
}

/** The first leading metadata block protects user quotations and malformed explicit metadata from guessing. */
export function userSourceSentinel(text: string): boolean {
  const metadata = sourceMetadata(text);
  return metadata.found && !metadata.source;
}

const unescape = (value: string) => value.replace(/&quot;/g, '"').replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");

/** Legacy identity requires the complete leading machine template, including matching sender/reply ids. */
function legacyConversationSource(raw: string): MessageSource | undefined {
  for (const context of leadingContexts(raw)) {
    let title = RELAY_TITLE.exec(context.attrs);
    let body = RELAY_BODY.exec(context.text);
    if (!title || !body) {
      title = LEGACY_RELAY_TITLE.exec(context.attrs);
      body = LEGACY_RELAY_BODY.exec(context.text);
    }
    if (/\bkind="context"/.test(context.attrs) && title && body && body[1] === body[5]) return { kind: "conversation", conversation_id: body[1], title: unescape(title[1]) };
  }
}

/** Only the verified relay control envelope is omitted; selected/knowledge contexts remain inspectable. */
export function isRelayContext(context: { kind: string; title?: string; text: string }): boolean {
  const text = context.text.trim();
  const title = context.title ?? "";
  const current = /^From the conversation “.*”$/u.test(title) ? RELAY_BODY.exec(text) : null;
  const body = current ?? (/^来自对话「.*」$/u.test(title) ? LEGACY_RELAY_BODY.exec(text) : null); // i18n-ignore: canonical relay title
  return context.kind === "context" && Boolean(body && body[1] === body[5]);
}

/** Explicit provenance wins, then durable metadata, injected compatibility, and complete legacy relay templates. */
export function sourceOf(item: Item, injected: Injected | null): MessageSource | undefined {
  if (item.source && typeof item.source.kind === "string" && item.source.kind.trim()) return item.source;
  if (item.client_msg_id || item.from) return undefined;
  const metadata = sourceMetadata(item.text ?? "");
  if (metadata.found) return metadata.source;
  if (injected?.state === "supervise") return { kind: "supervision", title: injected.title };
  if (injected?.kind === "followup") return {
    kind: injected.state === "daily" || injected.state === "interval" ? "patrol" : "followup",
    title: injected.title,
  };
  return legacyConversationSource(item.text ?? "");
}

export function sourceLabel(source: MessageSource): string {
  return source.title?.trim() || t(`chat-messages.source_${["conversation", "patrol", "followup", "supervision", "scheduled_task"].includes(source.kind) ? source.kind : "other"}`);
}

/** Equal titles alone must not merge different originating conversations. */
export function sourceKey(source: MessageSource): string {
  return JSON.stringify([source.kind, source.conversation_id ?? "", source.title ?? ""]);
}
