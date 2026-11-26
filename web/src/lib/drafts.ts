import { credentialOwner, onCredentialChange } from "./api";
import { conversationDeletionScope, conversationKeyDeleted, onConversationDeleted } from "./conversation-deleted";
import { canonicalStorageKey, storedKeyAlias, storedValue, storeValue, removeStoredValue } from "./compatibility";
// Changes to a composer's draft from outside it: the message menu's "Quote" / "Edit", a queued message pulled back, an
// edited image put in for the next message (iOS: ChatView.bindMenu, AppModel.editQueued, ImageViewer's attach).
// The composer for that draft key listens; with none mounted, text goes to the stored draft (it is read on mount).
// "Quote" waits as a bar above the input (not as `> ` lines in it) and goes out as the same `> ` block in front of the
// message, so what the agent reads is unchanged; a bubble draws a leading `> ` block folded (splitQuote). A comment on
// picked words or on lines of code (lib/comments.ts, #2437) is one more entry of that list: the message quote is
// at most one (a new one replaces it), comments pile up, each its own block after it.
import { useSyncExternalStore } from "react";
import { clampComment, commentBlock, isCommentHead, parseCommentBlock, type QuoteComment } from "./comments";
import { uuid } from "./format";
import { composerDraft, joinComposerDrafts, splitComposerDraft } from "./skill-mentions";
import { shiftInlineMentions } from "./inline-mentions";
import type { EditedImage } from "./image-edit";

export interface DraftImage {
  name: string;
  mime: string;
  data: string; // base64
}

export interface StoredDraftImage extends DraftImage {
  id?: string;
  edited?: EditedImage;
  editedSource?: { mime: string; data: string };
}

/** What "Quote" holds above the input: the quoted words, who said them, when; for a task's card, which task. */
export interface DraftQuote {
  text: string;
  from?: string;
  ts?: string;
  task?: QuotedTask;
  /** Set on a comment (the note, where it sits); the quote then carries the picked words or the code in `text`. */
  comment?: QuoteComment;
  /** Which entry of the list (set when it joins it). */
  id?: string;
}

/** The task a quoted card is about: its title and todo id, sent on the quote's first line so the assistant knows. */
export interface QuotedTask {
  title: string;
  id?: string;
}

/** The first line of a task's quote: "task", the title in corner brackets, then "(todo id: <id>)" in full-width parentheses (TASK_LINE). */
export function taskQuoteLine(t: QuotedTask): string {
  // i18n-ignore: written into the message and read back by TASK_LINE (and by the apps); not interface text
  return `任务「${t.title}」${t.id ? `（todo id: ${t.id}）` : ""}`;
}

// i18n-ignore: the shape taskQuoteLine writes
const TASK_LINE = /^任务「(.*)」(?:（todo id: ([^）\s]+)）)?$/;

/** A quote's words as sent (splitQuote's `quote`) back to what was quoted: a task's first line becomes its task. */
export function parseQuote(text: string): DraftQuote {
  const [first, ...rest] = text.split("\n");
  const m = TASK_LINE.exec(first.trim());
  if (!m) return { text };
  return { text: rest.join("\n").trim(), task: { title: m[1], ...(m[2] ? { id: m[2] } : {}) } };
}

export type DraftEvent =
  | ({ type: "quote" } & DraftQuote)
  /** `edit`: it starts editing the last message (#4503); any other replace ends an edit. */
  | { type: "replace"; text: string; edit?: boolean }
  | { type: "append"; text: string }
  /** The words of a send that failed after its box moved on (another conversation, or closed): back in front. */
  | { type: "unsent"; text: string }
  /** `edited`: the picture came out of the image editor; its source, edits and history, so the composer's tile opens
   *  it where the editor stopped (#4991). */
  | { type: "image"; image: DraftImage; edited?: EditedImage };

/** "> " in front of every line, then a blank line, then what was already typed (ChatView.swift bindMenu). */
export function withQuote(draft: string, text: string): string {
  const quoted = text.split("\n").map((l) => `> ${l}`).join("\n");
  return `${quoted}\n\n${draft}`;
}

/** One quote's words as they go into the message (a task's first line, then the words). */
function quoteBody(q: DraftQuote): string {
  const words = q.text.trim();
  return q.task ? [taskQuoteLine(q.task), words].filter(Boolean).join("\n\n") : words;
}

/** The quotes waiting as the blocks that lead the message: the message quote first, then the comments numbered in order. */
export function quoteBlocks(quotes: DraftQuote | readonly DraftQuote[] | null): string[] {
  const list = quotes === null ? [] : "text" in quotes ? [quotes] : quotes;
  let n = 0;
  const bodies = [...list.filter((q) => !q.comment), ...list.filter((q) => q.comment)].map((q) => (q.comment ? commentBlock(q, ++n) : quoteBody(q)));
  return bodies.filter(Boolean).map((b) => withQuote("", b).trimEnd());
}

/** The message as sent with its quotes: their blocks, a blank line, then what was typed (just the blocks when nothing was). */
export function withQuoteBlock(quotes: DraftQuote | readonly DraftQuote[] | null, text: string): string {
  const blocks = quoteBlocks(quotes);
  if (!blocks.length) return text;
  const lead = blocks.join("\n\n");
  return text.trim() ? `${lead}\n\n${text}` : lead;
}

/** A message that starts with a `> ` block (sent with "Quote", or typed): the quoted words and what follows. */
export function splitQuote(text: string): { quote: string; rest: string } | null {
  const lines = text.replace(/^\s*\n/, "").split("\n");
  let n = 0;
  while (n < lines.length && /^ {0,3}>/.test(lines[n])) n++;
  if (!n) return null;
  const quote = lines.slice(0, n).map((l) => l.replace(/^ {0,3}> ?/, "")).join("\n").trim();
  if (!quote) return null;
  return { quote, rest: lines.slice(n).join("\n").trim() };
}

/** A message's leading quotes: the first `> ` block, and after it the comment blocks (each starts `Comment N` /
 *  `Code comment N`, lib/comments.ts); what follows them is `rest`. */
export function splitQuotes(text: string): { quotes: string[]; rest: string } | null {
  const first = splitQuote(text);
  if (!first) return null;
  const quotes = [first.quote];
  let rest = first.rest;
  for (let next = splitQuote(rest); next && isCommentHead(next.quote.split("\n")[0]); next = splitQuote(rest)) {
    quotes.push(next.quote);
    rest = next.rest;
  }
  return { quotes, rest };
}

/** The quotes of a sent message's blocks back to what was quoted (an edit puts them back above the input). */
export function parseQuotes(blocks: readonly string[]): DraftQuote[] {
  return blocks.map((b) => (isCommentHead(b.split("\n")[0]) ? parseCommentBlock(b) : null) ?? parseQuote(b)).map((q) => ({ ...q, id: uuid() }));
}

const DRAFT_PREFIX = "draft:";
const QUOTE_PREFIX = "quote:";
const quoteKey = (key: string) => `${QUOTE_PREFIX}${key}`;

/** How many conversations' unsent words and quotes one browser keeps; the oldest unmounted ones are dropped first. */
export const MAX_STORED_DRAFTS = 50;

function validQuote(q: unknown): q is DraftQuote {
  const x = q as DraftQuote | null;
  return Boolean(x && typeof x === "object" && typeof x.text === "string" && (x.text || x.task?.title || x.comment));
}

/** The quotes kept for a composer's draft key (read on mount, like the draft). An older build kept just one object. */
export function storedQuotes(key: string): DraftQuote[] {
  if (conversationKeyDeleted(key)) return [];
  try {
    const v: unknown = JSON.parse(localStorage.getItem(quoteKey(key)) ?? "null");
    return (Array.isArray(v) ? v : [v]).filter(validQuote).map((q) => (q.id ? q : { ...q, id: uuid() }));
  } catch {
    return [];
  }
}

export function storeQuotes(key: string, list: readonly DraftQuote[]) {
  if (conversationKeyDeleted(key)) list = [];
  const stored = quoteKey(key);
  if (list.length) safeDraftWrite(stored, () => localStorage.setItem(stored, JSON.stringify(list)));
  else safeDraftRemove(stored);
}

/** What a composer's quote list is right now, and which comment card is being written in. */
export interface QuoteState {
  list: readonly DraftQuote[];
  /** The id of the card whose note is open for writing. */
  editing: string | null;
  /** That card is new: Esc takes it back instead of leaving the note as it was. */
  fresh: boolean;
}

const EMPTY: QuoteState = { list: [], editing: null, fresh: false };
const quoteStates = new Map<string, QuoteState>();
const quoteListeners = new Set<() => void>();

/** The composer's quotes (memory, filled from the stored ones the first time a key is asked for). */
export function getQuotes(key: string): QuoteState {
  let st = quoteStates.get(key);
  if (!st) {
    const list = storedQuotes(key);
    st = list.length ? { list, editing: null, fresh: false } : EMPTY;
    quoteStates.set(key, st);
  }
  return st;
}

function putQuotes(key: string, st: QuoteState) {
  if (conversationKeyDeleted(key)) st = EMPTY;
  quoteStates.set(key, st.list.length ? st : EMPTY);
  storeQuotes(key, st.list);
  quoteListeners.forEach((l) => l());
}

export function useQuotes(key: string): QuoteState {
  return useSyncExternalStore(
    (l) => {
      quoteListeners.add(l);
      return () => quoteListeners.delete(l);
    },
    () => getQuotes(key),
    () => getQuotes(key),
  );
}

/** A quote joins the list: a message quote takes the place of the last one (first in the list), a comment comes last.
 *  Returns its id. A comment starts open for writing (`write`). */
export function addQuote(key: string, q: DraftQuote, write = Boolean(q.comment)): string {
  const st = getQuotes(key);
  const added: DraftQuote = { ...q, id: uuid() };
  const list = q.comment ? [...st.list, added] : [added, ...st.list.filter((x) => x.comment)];
  putQuotes(key, { list, editing: write ? added.id! : st.editing && list.some((x) => x.id === st.editing) ? st.editing : null, fresh: write });
  return added.id!;
}

/** The whole list replaced (a send that cleared it, a failed one that puts it back, an edit that returns the quotes). */
export function setQuotes(key: string, list: readonly DraftQuote[]) {
  putQuotes(key, { list, editing: null, fresh: false });
}

export function removeQuote(key: string, id: string) {
  const st = getQuotes(key);
  const list = st.list.filter((q) => q.id !== id);
  putQuotes(key, { list, editing: st.editing === id ? null : st.editing, fresh: st.editing === id ? false : st.fresh });
}

/** The note of a comment card; `done` closes it for writing. */
export function setNote(key: string, id: string, body: string, done = false) {
  const st = getQuotes(key);
  const list = st.list.map((q) => (q.id === id && q.comment ? { ...q, comment: { ...q.comment, body: clampComment(body) } } : q));
  putQuotes(key, done ? { list, editing: null, fresh: false } : { ...st, list });
}

/** Opens a card's note for writing again (a click on "Comment"). */
export function editNote(key: string, id: string) {
  const st = getQuotes(key);
  if (st.editing !== id) putQuotes(key, { ...st, editing: id, fresh: false });
}

/** Esc in a note: a new card goes, an older one keeps its last note (the caller restores it). */
export function cancelNote(key: string, id: string, previous: string) {
  const st = getQuotes(key);
  if (st.fresh && st.editing === id) removeQuote(key, id);
  else setNote(key, id, previous, true);
}

/** A pulled-back queued message goes under what is typed, on its own line (AppModel.editQueued). */
export function withAppended(draft: string, text: string): string {
  return draft ? `${draft}\n${text}` : text;
}

/** The draft after a send failed: the composer emptied itself when the message went out, so the text comes back;
 * what was typed while it was on its way stays, under it on its own line. */
export function withUnsent(draft: string, unsent: string): string {
  if (!unsent) return draft;
  return draft ? `${unsent}\n${draft}` : unsent;
}

/** The draft after a text event (an image changes the attachments and a quote the bar above, not the text). */
export function applyDraft(draft: string, e: DraftEvent): string {
  switch (e.type) {
    case "replace":
      return e.text;
    case "append":
      return withAppended(draft, e.text);
    case "unsent":
      return withUnsent(draft, e.text);
    default:
      return draft;
  }
}

/** Replacing asks first only when something else is typed ("Edit" must not silently drop a draft). */
export function replaceNeedsConfirm(draft: string, text: string): boolean {
  return draft.trim() !== "" && draft.trim() !== text.trim();
}

type Listener = (e: DraftEvent) => void;
const listeners = new Map<string, Set<Listener>>();

// The sidebar's rows mark a conversation with something left unsent while its box is not on screen (#5568): they hear
// of the words a box keeps (`draft:<key>`, read when the box mounts, written through storeDraft), of its quotes, and
// of a box mounting or going away for a key (onDraftEvent).
const rowListeners = new Set<() => void>();
const tellRows = () => rowListeners.forEach((l) => l());
const subscribeRows = (l: () => void) => {
  rowListeners.add(l);
  quoteListeners.add(l);
  return () => {
    rowListeners.delete(l);
    quoteListeners.delete(l);
  };
};

/** A composer listens for its draft key; returns the unsubscribe. */
export function onDraftEvent(key: string, l: Listener): () => void {
  if (!listeners.has(key)) listeners.set(key, new Set());
  listeners.get(key)!.add(l);
  tellRows();
  return () => {
    const ls = listeners.get(key);
    ls?.delete(l);
    // the last composer of a key takes its set along: readers only ask for its size, so a missing key reads the same
    if (ls?.size === 0) listeners.delete(key);
    tellRows();
  };
}

/** Tests only: how many draft keys have a listener set. */
export function draftListenerKeysForTests() {
  return listeners.size;
}

export function storeDraft(key: string, text: string) {
  if (conversationKeyDeleted(key)) text = "";
  const stored = `${DRAFT_PREFIX}${key}`;
  const id = canonicalDraftId(key);
  if (text) {
    if (safeDraftWrite(stored, () => storeValue(stored, text))) memoryDrafts.delete(id);
    else setMemoryDraft(id, key, text);
  } else {
    safeDraftRemove(stored);
    if (safeRawDraftValue(key) !== null && !conversationKeyDeleted(key)) setMemoryDraft(id, key, "");
    else memoryDrafts.delete(id);
  }
  tellRows();
}

// Storage pressure: a full or blocked localStorage must never break typing or throw out of the save timer. A failed
// write drops the oldest other conversations' drafts and quotes first — the mounted composers' keys are protected —
// and is retried; what still cannot be written falls back to a bounded in-memory map for the current session/owner.
const KEY_PREFIXES = [DRAFT_PREFIX, QUOTE_PREFIX] as const;

/** Maximum unmounted conversations and base64 character budget retained in memory for attached draft images. */
export const MAX_STORED_DRAFT_IMAGE_CONVERSATIONS = 20;
export const MAX_STORED_DRAFT_IMAGE_CHARS = 64 * 1024 * 1024;
export const MAX_SESSION_DRAFT_IMAGE_CHARS = 2 * 1024 * 1024;

const DRAFT_IMAGES_PREFIX = "draft-images:";
const DRAFT_IMAGES_EPOCH_KEY = "draft-images:epoch";

interface MemoryDraftEntry {
  scope: string;
  text: string;
  diskSnapshot: string | null;
}

interface DraftImageEntry {
  scope: string;
  chars: number;
  images: StoredDraftImage[];
}

const memoryDrafts = new Map<string, MemoryDraftEntry>();
const draftImageStore = new Map<string, DraftImageEntry>();
let imageStoreEpoch: string | null = null;
let imageStoreEpochPersisted = false;

function draftOwnerScope(): string {
  let owner = "";
  try {
    if (typeof localStorage !== "undefined") owner = credentialOwner();
  } catch {
    // Storage can be absent or blocked in non-DOM tests or restricted frames.
  }
  return `${owner}:${conversationDeletionScope()}`;
}

function canonicalDraftId(key: string): string {
  const canonical = canonicalStorageKey(`${DRAFT_PREFIX}${key}`);
  return canonical.startsWith(DRAFT_PREFIX) ? canonical.slice(DRAFT_PREFIX.length) : key;
}

function isConversationMounted(id: string): boolean {
  for (const [key, set] of listeners) {
    if (set.size > 0 && canonicalDraftId(key) === id) return true;
  }
  return false;
}

function safeRawDraftValue(key: string): string | null {
  if (!draftStorage()) return null;
  try {
    return storedValue(`${DRAFT_PREFIX}${key}`);
  } catch {
    return null;
  }
}

function setMemoryDraft(id: string, key: string, text: string) {
  const scope = draftOwnerScope();
  for (const [k, entry] of memoryDrafts) {
    if (entry.scope !== scope) memoryDrafts.delete(k);
  }
  memoryDrafts.delete(id);
  memoryDrafts.set(id, { scope, text, diskSnapshot: safeRawDraftValue(key) });
  if (memoryDrafts.size <= MAX_STORED_DRAFTS) return;
  for (const candidate of memoryDrafts.keys()) {
    if (memoryDrafts.size <= MAX_STORED_DRAFTS) break;
    if (candidate !== id && !isConversationMounted(candidate)) memoryDrafts.delete(candidate);
  }
}

/** Reads the unsent text for `key` from localStorage, falling back to the in-memory copy if storage quota was full. */
export function storedDraft(key: string): string {
  if (conversationKeyDeleted(key)) return "";
  const id = canonicalDraftId(key);
  const disk = safeRawDraftValue(key);
  const mem = memoryDrafts.get(id);
  if (mem) {
    if (mem.scope === draftOwnerScope() && disk === mem.diskSnapshot) return mem.text;
    memoryDrafts.delete(id);
  }
  return disk ?? "";
}

function sessionDraftStorage(): Storage | null {
  try {
    return typeof sessionStorage === "undefined" ? null : sessionStorage;
  } catch {
    return null;
  }
}

function clearSessionDraftImages() {
  const ss = sessionDraftStorage();
  if (!ss) return;
  try {
    for (let i = ss.length - 1; i >= 0; i--) {
      const k = ss.key(i);
      if (k && k.startsWith(DRAFT_IMAGES_PREFIX)) ss.removeItem(k);
    }
  } catch {
    // Blocked sessionStorage: nothing to clear.
  }
}

function syncImageStoreEpoch() {
  if (!imageStoreEpoch || !imageStoreEpochPersisted) return;
  const ls = draftStorage();
  let current: string | null = null;
  try {
    current = ls?.getItem(DRAFT_IMAGES_EPOCH_KEY) ?? null;
  } catch {
    return;
  }
  if (current !== imageStoreEpoch) {
    draftImageStore.clear();
    imageStoreEpoch = null;
    imageStoreEpochPersisted = false;
    clearSessionDraftImages();
  }
}

function ensureImageStoreEpoch() {
  syncImageStoreEpoch();
  const ls = draftStorage();
  if (!imageStoreEpoch) {
    try {
      imageStoreEpoch = ls?.getItem(DRAFT_IMAGES_EPOCH_KEY) || uuid();
    } catch {
      imageStoreEpoch = uuid();
    }
  }
  if (!imageStoreEpochPersisted && ls) {
    try {
      ls.setItem(DRAFT_IMAGES_EPOCH_KEY, imageStoreEpoch);
      imageStoreEpochPersisted = ls.getItem(DRAFT_IMAGES_EPOCH_KEY) === imageStoreEpoch;
    } catch {
      imageStoreEpochPersisted = false;
    }
  }
}

function validStoredDraftImage(x: unknown): x is StoredDraftImage {
  const img = x as StoredDraftImage | null;
  return Boolean(
    img &&
    typeof img === "object" &&
    typeof img.name === "string" &&
    typeof img.mime === "string" &&
    typeof img.data === "string" &&
    img.data.length > 0,
  );
}

function draftImageChars(images: readonly StoredDraftImage[]): number {
  return images.reduce((n, img) => n + img.data.length + (img.editedSource?.data.length ?? 0), 0);
}

function totalDraftImageChars(): number {
  let total = 0;
  draftImageStore.forEach((entry) => { total += entry.chars; });
  return total;
}

function readSessionDraftImages(id: string): StoredDraftImage[] {
  const ss = sessionDraftStorage();
  if (!ss) return [];
  const storageKey = `${DRAFT_IMAGES_PREFIX}${id}`;
  try {
    const raw = ss.getItem(storageKey);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    const envelope = parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as { epoch?: unknown; images?: unknown })
      : { epoch: undefined, images: parsed };
    if (typeof envelope.epoch === "string") {
      const ls = draftStorage();
      if ((ls?.getItem(DRAFT_IMAGES_EPOCH_KEY) ?? null) !== envelope.epoch) {
        ss.removeItem(storageKey);
        return [];
      }
      if (!imageStoreEpoch) {
        imageStoreEpoch = envelope.epoch;
        imageStoreEpochPersisted = true;
      }
    }
    const list = (Array.isArray(envelope.images) ? envelope.images : []).filter(validStoredDraftImage);
    return list.map(({ id: imgId, name, mime, data }) => ({ ...(imgId ? { id: imgId } : {}), name, mime, data }));
  } catch {
    return [];
  }
}

function writeSessionDraftImages(id: string, images: readonly StoredDraftImage[]) {
  const ss = sessionDraftStorage();
  if (!ss) return;
  const storageKey = `${DRAFT_IMAGES_PREFIX}${id}`;
  if (!images.length) {
    try { ss.removeItem(storageKey); } catch { /* Blocked sessionStorage. */ }
    return;
  }
  const chars = images.reduce((n, img) => n + img.data.length, 0);
  if (chars > MAX_SESSION_DRAFT_IMAGE_CHARS) {
    try { ss.removeItem(storageKey); } catch { /* Blocked sessionStorage. */ }
    return;
  }
  const payload = JSON.stringify({
    epoch: imageStoreEpochPersisted ? imageStoreEpoch : null,
    images: images.map(({ id: imgId, name, mime, data }) => ({ ...(imgId ? { id: imgId } : {}), name, mime, data })),
  });
  try {
    ss.setItem(storageKey, payload);
    return;
  } catch {
    // Evict older unmounted session image entries and retry once.
  }
  try {
    const victims: string[] = [];
    for (let i = 0; i < ss.length; i++) {
      const k = ss.key(i);
      if (!k || !k.startsWith(DRAFT_IMAGES_PREFIX) || k === DRAFT_IMAGES_EPOCH_KEY || k === storageKey) continue;
      const cid = k.slice(DRAFT_IMAGES_PREFIX.length);
      if (!isConversationMounted(cid)) victims.push(k);
    }
    for (const victim of victims) {
      ss.removeItem(victim);
      try {
        ss.setItem(storageKey, payload);
        return;
      } catch {
        // Continue freeing older entries.
      }
    }
  } catch {
    // In-memory LRU still preserves attached images across conversation switches.
  }
}

export function storeDraftImages(key: string, images: readonly StoredDraftImage[]) {
  if (conversationKeyDeleted(key)) images = [];
  syncImageStoreEpoch();
  const id = canonicalDraftId(key);
  const valid = images.filter(validStoredDraftImage).map((img) => ({
    ...(img.id ? { id: img.id } : {}),
    name: img.name,
    mime: img.mime,
    data: img.data,
    ...(img.edited ? { edited: img.edited } : {}),
    ...(img.editedSource ? { editedSource: img.editedSource } : {}),
  }));
  if (!valid.length) {
    const hadMem = draftImageStore.delete(id);
    const ss = sessionDraftStorage();
    let hadSession = false;
    if (ss) {
      try {
        const sk = `${DRAFT_IMAGES_PREFIX}${id}`;
        hadSession = ss.getItem(sk) !== null;
        if (hadSession) ss.removeItem(sk);
      } catch {
        // Blocked sessionStorage.
      }
    }
    if (hadMem || hadSession) tellRows();
    return;
  }
  ensureImageStoreEpoch();
  const scope = draftOwnerScope();
  for (const [k, entry] of draftImageStore) {
    if (entry.scope !== scope) {
      draftImageStore.delete(k);
      writeSessionDraftImages(k, []);
    }
  }
  draftImageStore.delete(id);
  draftImageStore.set(id, { scope, chars: draftImageChars(valid), images: valid });
  while (draftImageStore.size > MAX_STORED_DRAFT_IMAGE_CONVERSATIONS || totalDraftImageChars() > MAX_STORED_DRAFT_IMAGE_CHARS) {
    let evicted = false;
    for (const candidate of draftImageStore.keys()) {
      if (candidate === id || isConversationMounted(candidate)) continue;
      draftImageStore.delete(candidate);
      writeSessionDraftImages(candidate, []);
      evicted = true;
      break;
    }
    if (!evicted) break;
  }
  writeSessionDraftImages(id, valid);
  tellRows();
}

export function storedDraftImages(key: string): StoredDraftImage[] {
  if (conversationKeyDeleted(key)) return [];
  syncImageStoreEpoch();
  const id = canonicalDraftId(key);
  const scope = draftOwnerScope();
  const mem = draftImageStore.get(id);
  if (mem) {
    if (mem.scope !== scope) {
      draftImageStore.delete(id);
      writeSessionDraftImages(id, []);
      return [];
    }
    draftImageStore.delete(id);
    draftImageStore.set(id, mem);
    return mem.images.map((img) => ({ ...img }));
  }
  const restored = readSessionDraftImages(id);
  if (restored.length) {
    draftImageStore.set(id, { scope, chars: draftImageChars(restored), images: restored });
  }
  return restored.map((img) => ({ ...img }));
}

export function hasDraftImages(key: string): boolean {
  if (conversationKeyDeleted(key)) return false;
  syncImageStoreEpoch();
  const id = canonicalDraftId(key);
  const mem = draftImageStore.get(id);
  if (mem) {
    if (mem.scope !== draftOwnerScope()) {
      draftImageStore.delete(id);
      writeSessionDraftImages(id, []);
      return false;
    }
    return mem.images.length > 0;
  }
  return readSessionDraftImages(id).length > 0;
}

function draftStorage(): Storage | null {
  try {
    return localStorage;
  } catch {
    return null;
  }
}

/** Storage keys of every mounted composer's draft and quote list: eviction never touches the conversation on screen. */
function protectedDraftKeys(): Set<string> {
  const keep = new Set<string>();
  listeners.forEach((_set, key) => KEY_PREFIXES.forEach((prefix) => keep.add(prefix + key)));
  return keep;
}

/** The conversation id a stored draft/quote key belongs to; a legacy alias key shares its canonical key's id. */
function storedConversationId(stored: string): string | null {
  const key = canonicalStorageKey(stored);
  const prefix = KEY_PREFIXES.find((p) => key.startsWith(p));
  return prefix ? key.slice(prefix.length) : null;
}

/** Conversation ids holding a stored draft or quote, oldest first in storage order; protected ids are skipped. A
 *  key and its legacy alias (compatibility.ts) are one conversation, so it is never counted twice. */
function evictableConversations(keep: ReadonlySet<string>): string[] {
  const store = draftStorage();
  const ids: string[] = [];
  if (!store) return ids;
  try {
    for (let i = 0; i < store.length; i++) {
      const stored = store.key(i);
      if (!stored || keep.has(stored)) continue;
      // the mounted conversation's canonical key is protected even when this entry is its legacy alias
      const alias = storedKeyAlias(stored);
      if (alias && keep.has(alias)) continue;
      const id = storedConversationId(stored);
      if (id !== null && !ids.includes(id)) ids.push(id);
    }
  } catch {
    // storage became unreadable: return what was gathered
  }
  return ids;
}

function dropConversation(store: Storage, id: string) {
  // the legacy alias copy leaves with its canonical key, so no orphaned entry stays behind
  for (const prefix of KEY_PREFIXES) {
    const key = prefix + id;
    for (const k of [key, storedKeyAlias(key)]) {
      if (!k) continue;
      try {
        store.removeItem(k);
      } catch {
        // blocked storage: nothing to do
      }
    }
  }
  const mem = memoryDrafts.get(id);
  if (mem) mem.diskSnapshot = safeRawDraftValue(id);
}

/** Keep the retained drafts of unmounted conversations within MAX_STORED_DRAFTS, oldest first. */
function enforceDraftBound(store: Storage, targetKey: string) {
  const ids = evictableConversations(protectedDraftKeys());
  const targetId = storedConversationId(targetKey);
  const victims = ids.filter((id) => id !== targetId);
  // The target counts toward the bound even though it cannot be its own victim.
  for (let i = 0; i < ids.length - MAX_STORED_DRAFTS; i++) dropConversation(store, victims[i]);
}

/** Write a draft or quote entry, evicting older unmounted conversations' entries and retrying when storage is full.
 *  The target key itself and every mounted composer's keys survive; a write that still fails stays quiet. Other draft
 *  writers (page-level prompt boxes) route their raw localStorage writes through this so a full quota never throws. */
export function safeDraftWrite(targetKey: string, write: () => void): boolean {
  const store = draftStorage();
  if (!store) return false;
  try {
    write();
    enforceDraftBound(store, targetKey);
    return true;
  } catch {
    // quota exceeded or storage blocked: make room below and retry
  }
  const keep = protectedDraftKeys();
  keep.add(targetKey);
  for (const id of evictableConversations(keep)) {
    dropConversation(store, id);
    try {
      write();
      return true;
    } catch {
      // still full: drop the next oldest conversation
    }
  }
  return false;
}

/** Remove a draft or quote entry without throwing when storage is blocked (drafts keep their alias write path). */
export function safeDraftRemove(storedKey: string) {
  if (!draftStorage()) return;
  try {
    if (storedKey.startsWith(DRAFT_PREFIX)) removeStoredValue(storedKey);
    else localStorage.removeItem(storedKey);
  } catch {
    // the stale entry stays, but typing and sending keep working
  }
}

/** Words of a composer that belongs to no conversation row (the Agents page box): kept under a key the sidebar
 *  never asks useLeftDraft for, so a half-written instruction never marks a row, but a reload still restores it. */
export function storedPageDraft(key: string): string {
  return storedDraft(key);
}

export function storePageDraft(key: string, text: string) {
  if (conversationKeyDeleted(key)) text = "";
  const stored = `draft:${key}`;
  const id = canonicalDraftId(key);
  // route through the quota-safe path so a full localStorage cannot throw inside the composer
  if (text) {
    if (safeDraftWrite(stored, () => storeValue(stored, text))) memoryDrafts.delete(id);
    else setMemoryDraft(id, key, text);
  } else {
    safeDraftRemove(stored);
    if (safeRawDraftValue(key) !== null && !conversationKeyDeleted(key)) setMemoryDraft(id, key, "");
    else memoryDrafts.delete(id);
  }
}

/** What waits unsent for a conversation whose box is not on screen: its stored words, and whether quotes or pictures
 *  wait in its tray. Nothing while its box is mounted (you are in it; typing there does not re-render the list), or for `null`. */
export function useLeftDraft(key: string | null): { text: string; quoted: boolean } {
  const away = () => key !== null && !listeners.get(key)?.size;
  const read = () => (away() ? storedDraft(key!) : "");
  const hasAttachmentOrQuote = () => away() && (getQuotes(key!).list.length > 0 || hasDraftImages(key!));
  const text = useSyncExternalStore(subscribeRows, read, read);
  const quoted = useSyncExternalStore(subscribeRows, hasAttachmentOrQuote, hasAttachmentOrQuote);
  return { text: splitComposerDraft(text).text, quoted };
}

/** Sends an event to the composer of `key`. False when none is mounted and it could not be kept (an image). */
export function sendDraft(key: string, e: DraftEvent): boolean {
  if (conversationKeyDeleted(key)) return false;
  const ls = listeners.get(key);
  if (ls?.size) {
    ls.forEach((l) => l(e));
    return true;
  }
  if (e.type === "image") return false;
  if (e.type === "quote") {
    addQuote(key, { text: e.text, from: e.from, ts: e.ts, task: e.task }, false);
    return true;
  }
  // "Edit" / "Rewind" a message sent with a quote while its box is away: as the composer would, the quotes go back to the bar
  // above the input (any it had are replaced) and only the words into the draft.
  const current = storedDraft(key);
  let next: string;
  if (e.type === "replace") {
    const selected = splitComposerDraft(e.text);
    const split = splitQuotes(selected.text);
    setQuotes(key, split ? parseQuotes(split.quotes) : []);
    next = split ? composerDraft(shiftInlineMentions(selected.refs, -selected.text.lastIndexOf(split.rest)), split.rest) : e.text;
  } else {
    next = e.type === "append" ? joinComposerDrafts(current, e.text) : joinComposerDrafts(e.text, current);
  }
  storeDraft(key, next);
  return true;
}

onConversationDeleted((mid, sid) => {
  const key = `${mid}/${sid}`;
  try { storeDraft(key, ""); } catch { /* Storage can be unavailable. */ }
  try { storeDraftImages(key, []); } catch { /* Storage can be unavailable. */ }
  try { setQuotes(key, []); } catch { /* Its memory copy was already cleared. */ }
});

onCredentialChange(() => {
  memoryDrafts.clear();
  draftImageStore.clear();
  imageStoreEpoch = null;
  imageStoreEpochPersisted = false;
  clearSessionDraftImages();
  tellRows();
});
