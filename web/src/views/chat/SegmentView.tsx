import { type CSSProperties, Fragment, lazy, memo, type ReactNode, Suspense, useContext, useLayoutEffect, useMemo, useRef, useState } from "react";
import { clock } from "../../lib/format";
import type { stripCloseout as stripCloseoutText } from "../../lib/closeout-text";
import { useCloseoutReader } from "./closeout-chunks";
import { getSession, getState, useStore } from "../../lib/store";
import { withChildSessions } from "../../lib/child-cards";
import { agentStatus, isNestedSubagentStep, nestedSubagentStatus, nestingKey, subagentFor, withNestedSubagents } from "../../lib/tasks";
import { turnImages } from "../../lib/turn-images";
import { TurnImages } from "./TurnImages";
import { exchangeSteps, isLoopTick, isOwnMessage, type Context, type Segment, type UserMessage } from "../../lib/turns";
import { usageKey } from "../../lib/turn-usage";
import type { Item } from "../../lib/types";
import { ENGINE_SWITCH_KIND } from "../../lib/engine-switch";
import { planCardStep, workBlocks, workTime, type ToolStep, type WorkNode } from "../../lib/work-groups";
import { NO_WORKFLOW_CALLS } from "../../lib/workflows";
import { channelOf, useChannels } from "../../lib/channels";
import { splitQuotes } from "../../lib/drafts";
import { EASE } from "../../ui/atoms";
import { ChannelMark, OriginMark } from "../../ui/ChannelMark";
import { openerFrom, type ExternalOrigin } from "../../lib/conversation-origins";
import { Markdown } from "../../ui/Markdown";
import { CardConversation, CardItem } from "../../ui/AssistantCard";
import { cardReply } from "../../lib/assistant-blocks";
import { InjectedHint, LoopTick, MergedBubble, PeerMessage } from "./Messages";
import { ModelChangeDivider } from "./ModelChangeDivider";
import { EngineSwitchDivider } from "./EngineSwitchDivider";
import { SourceMessage } from "./SourceMessage";
import { ContextGroup } from "./ContextGroup";
import { ForwardBlock, ForwardCards } from "./ForwardCard";
import { AttachedPhotos } from "./AttachedPhotos";
import { useFullText } from "./FullText";
import { FileCards, ReplyFiles } from "./FileCards";
import { ReplyPorts } from "./PortChips";
import { MessageActions } from "./MessageActions";
import { composerEditText } from "../../lib/skill-mentions";
import { inlineMentionAt, sessionMentionRanges } from "../../lib/inline-mentions";
import { clickLink, linkHref } from "../../lib/links";
import { useIsLatestMessage } from "./latest-message-time";
import { QuoteStack } from "./Quote";
import { PathText } from "../../ui/PathLinks";
import { Thumb } from "./Steps";
import { useFoldScroll } from "./fold-scroll";
import { useChatContext } from "./chat-context";
import { useFreshWords } from "./fresh-words";
import { Icon } from "../../ui/icons";
import { StepView, WorkGroup, type WorkProps } from "./WorkGroup";
import { TurnChanges, turnChangesHeight } from "./TurnChanges";
import { turnChanges } from "../../lib/turn-changes";
import type { Step } from "../../lib/turns";
import { useCallSeeker } from "./jump";
import { t } from "../../i18n";
import { useMessageFeedback } from "../../lib/message-send";

// a finished reply's Sources row (#9132): its chunk loads only for a turn that looked something up on the web
const TurnSources = lazy(() => import("./TurnSources").then((m) => ({ default: m.TurnSources })));

/** A message longer than this many lines folds to ten of them (FOLDED), with "Expand" under it (the reference client's UserMessage). */
const BUBBLE_MAX_LINES = 14;
/** Before it is measured: about this many characters to a line of the 488 px bubble. */
const CHARS_PER_LINE = 30;
/** A folded message: ten lines high, scrolling inside (the chat scrolls on at its top and end). */
const FOLDED = "max-h-[10lh] overflow-y-auto";
/** Characters to a line of a bubble narrower than any drawn (a phone's holds about 17 wide ones): a message within
 *  BUBBLE_MAX_LINES even there is short in any bubble, and is not measured. */
const NARROWEST_CHARS = 10;

/** How many lines a message takes in its bubble, by its text (the bubble measures itself once drawn). */
export function bubbleLines(text: string, perLine = CHARS_PER_LINE): number {
  return text.split("\n").reduce((n, l) => n + Math.max(1, Math.ceil([...l].length / perLine)), 0);
}

/** ModelChangeDivider's height as drawn (Chrome, 2026-11-12): its 8 px above and below, the chip's 4 px padding, 1 px
 *  border and 16.5 px line (11 px text, the inherited 1.5). The exchange's 14 px gap comes on top. */
export const MODEL_DIVIDER_HEIGHT = 42.5;
/** EngineSwitchDivider's height folded: the model change divider's row (the same 8 px above and below, and a chip of the
 *  same padding, border and 16.5 px line; its 12 px icons are shorter than the line). The handoff opened adds its own
 *  once drawn. */
export const ENGINE_DIVIDER_HEIGHT = MODEL_DIVIDER_HEIGHT;

/** A finished exchange's height in px before it is first drawn (content-visibility), by
 *  its text the way bubbleLines counts it: its bubble (folded past BUBBLE_MAX_LINES), its words at 2.5 times the
 *  bubble's characters to a line (the column is wider, and Markdown's short and blank lines take less than a line), a
 *  row for each other block, and the model change divider when it has one (`modelChange`), the engine switch divider
 *  folded likewise (`engineSwitch`). Once drawn it keeps its own height. */
export function segmentHeight(user: UserMessage | undefined, nodes: WorkNode[], modelChange = false, engineSwitch = false): number {
  // gap: the exchange's blocks stand 14 px apart (`gap-3.5`, cindy's msg-stream-items)
  const line = 24, gap = 14, row = 28;
  const lines = user ? bubbleLines(user.text) : 0;
  let h = user ? (lines > BUBBLE_MAX_LINES ? 10 : lines) * line + 20 + (user.images.length ? 102 : 0) : 0;
  for (const n of nodes) h += n.kind === "step" && n.step.kind === "text" ? bubbleLines(n.step.item.text ?? "", CHARS_PER_LINE * 2.5) * line : row;
  if (modelChange) h += MODEL_DIVIDER_HEIGHT;
  if (engineSwitch) h += ENGINE_DIVIDER_HEIGHT;
  return Math.round(h + gap * Math.max(0, nodes.length + (user ? 1 : 0) + (modelChange ? 1 : 0) + (engineSwitch ? 1 : 0) - 1));
}

/** `sendState`: a message not in the transcript yet (a sending bubble, the outbox's), marked for the sync scenarios
 *  (spec/fixtures/sync); the transcript's own bubbles have none. `starting`: the first message of a conversation still
 *  starting (the new-conversation page, #1697): drawn as it will be once sent, without actions (there is nothing yet to
 *  act on) and without "Sending…" (the page says it is preparing). */
/** `onDelete`: a message only on this device (a failed send): its menu ends in Delete, and it has no Rewind (it is not in
 *  the transcript to go back to). */
export function UserBubble({ msg, mid, sid, pending, sendState, starting, onDelete }: { msg: UserMessage; mid: string; sid: string; pending?: boolean; sendState?: "sending" | "sent" | "failed"; starting?: boolean; onDelete?: () => void }) {
  const own = isOwnMessage(msg);
  const feedback = useMessageFeedback(mid, sid, msg.client_msg_id ?? msg.id?.replace(/^(pending:|outbox:)/, "") ?? "");
  // the chat's last message keeps its time on screen under the bubble (#5285); a hover shows the others' beside it
  const pinned = useIsLatestMessage(msg.ts) && !pending && !starting;
  // the message an extension opened the conversation with (#3758)
  const { opening } = useChatContext();
  const openedBy = opening && opening.id === msg.id ? opening : undefined;
  // forwards (#3899) are cards at the top of the bubble (several at once, #3838: ForwardCards); the other contexts fold in
  // the row above it
  const forwards = msg.contexts.filter((c) => c.kind === "forward");
  const contexts = msg.contexts.filter((c) => c.kind !== "forward" && !inlineMentionAt(c, msg.text));
  return (
    <div className="ml-auto flex max-w-[min(100%,488px)] min-w-0 flex-col items-end gap-1.5 pl-4 sm:pl-8" style={pending ? { animation: `msg-launch 260ms ${EASE} both` } : undefined} data-user-bubble data-send-state={sendState} data-items={pending || starting ? undefined : [msg.id, ...msg.images.map((i) => i.id)].join(" ")}>
      {msg.images.length > 0 && (
        <div className="flex flex-wrap justify-end gap-1.5">
          {msg.images.map((im) => (
            <Thumb key={im.id} item={im} mid={mid} sid={sid} size={96} />
          ))}
        </div>
      )}
      {msg.photos && <AttachedPhotos paths={msg.photos} mid={mid} sid={sid} />}
      {contexts.length > 0 && <ContextGroup contexts={contexts} />}
      {msg.files && <FileCards files={msg.files} mid={mid} />}
      {(msg.text || forwards.length > 0) &&
        (starting ? (
          <BubbleText text={msg.text} blocks={msg.contexts} mid={mid} sid={sid} own={own} />
        ) : pending ? (
          <BubbleText text={msg.text} blocks={msg.contexts} mid={mid} sid={sid} own={own} pending />
        ) : (
          <MessageActions text={msg.text} editText={composerEditText(msg.contexts, msg.text)} isUser ts={msg.ts} side messageId={onDelete ? undefined : msg.id} onDelete={onDelete}
            images={msg.images} photos={msg.photos} className="max-w-full">
            <BubbleText text={msg.text} blocks={msg.contexts} ts={msg.ts} mid={mid} sid={sid} own={own} />
          </MessageActions>
        ))}
      {pinned && <span className="text-[11px] text-ink-3 tabular-nums" data-latest-time>{clock(msg.ts)}</span>}
      {/* a message waiting its turn says so under it; one on its way (Sending, Processing, delivery unknown) is said once,
          by the chat's status line under the last message (ChatView's data-message-feedback), not here as well */}
      {msg.queued && !pending && !starting && feedback !== "" && <span className="text-[11px] text-ink-3 whitespace-nowrap">{t("chat-segmentview.queued")}</span>}
      {!pending && !starting && <ChannelSource clientMsgId={msg.client_msg_id} openedBy={openedBy} />}
    </div>
  );
}

/** The bubble's words, folded to ten lines past fourteen; the fold scrolls (the wheel, a trackpad, a finger), with a
 *  fade at its bottom while there is more (fold-scroll.ts). `pending` (still being sent): dimmed, the same fold. A
 *  leading `> ` block ("Quote") is a folded quote card at its top instead of `>` lines (Quote.tsx). */
function BubbleText({ text: full, blocks, ts, mid, sid, own, pending }: { text: string; blocks: Context[]; ts?: string; mid: string; sid: string; own: boolean; pending?: boolean }) {
  const forwards = blocks.filter((c) => c.kind === "forward");
  const fill = own ? "bg-user-bubble" : "bg-neutral-bubble";
  // a reply sent from a card (in an assistant conversation): its words, without the hub's tag, marked "From a card"
  const fromCard = useContext(CardConversation) ? cardReply(full) : undefined;
  const whole = fromCard ? fromCard.text : full;
  const split = useMemo(() => splitQuotes(whole), [whole]);
  const text = split ? split.rest : whole;
  const mentions = useMemo(() => sessionMentionRanges(blocks, text, full.length - text.length), [blocks, text, full]);
  const cwd = getSession(mid, sid)?.cwd;
  const ref = useRef<HTMLDivElement>(null);
  const [long, setLong] = useState(() => bubbleLines(text) > BUBBLE_MAX_LINES);
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || open) return;
    // short however narrow: no layout read (opening a long chat read every bubble's, each a forced layout)
    if (bubbleLines(text, NARROWEST_CHARS) <= BUBBLE_MAX_LINES) {
      setLong(false);
      return;
    }
    // its full height in lines (a fold's scrollHeight is all of it)
    const line = parseFloat(getComputedStyle(el).lineHeight) || 24;
    setLong(el.scrollHeight / line > BUBBLE_MAX_LINES + 0.5);
  }, [text, open]);
  const folded = long && !open;
  const fold = useFoldScroll(ref, folded, [text]);
  const words = text && (
    <div
      ref={ref}
      {...fold}
      className={`text-[length:var(--chat-body)] leading-[1.6] whitespace-pre-wrap [overflow-wrap:anywhere] ${folded ? FOLDED : ""}`}
      data-folded={folded || undefined}
      data-bubble-text
    >
      {mentions.map(({ start, end, url }, i) => (
        <Fragment key={start}>
          <PathText text={text.slice(mentions[i - 1]?.end ?? 0, start)} mid={mid} cwd={cwd} />
          <a href={linkHref(url)} onClick={(event) => clickLink(event, url)} data-session-mention
            className="rounded-chip bg-accent-tint px-1 font-medium text-accent-ink [box-decoration-break:clone] hover:underline">
            {text.slice(start, end)}
          </a>
        </Fragment>
      ))}
      <PathText text={text.slice(mentions.at(-1)?.end ?? 0)} mid={mid} cwd={cwd} />
    </div>
  );
  return (
    <div className="flex max-w-full flex-col items-end gap-1" data-disclosure-anchor data-from-card={fromCard ? fromCard.view : undefined}>
      {fromCard && (
        <span className="flex items-center gap-1 text-[11px] font-medium whitespace-nowrap text-accent">
          <Icon name="check" size={11} />
          {t("ui-assistantcard.from_card")}
        </span>
      )}
      {split || forwards?.length ? (
        <div className={`flex max-w-full flex-col gap-1.5 rounded-[16px] ${fill} px-4 py-2.5 font-sans text-ink ${pending ? "opacity-60" : ""}`} title={clock(ts)} data-hit-target>
          {forwards.length > 1 ? <ForwardCards blocks={blocks} count={forwards.length} /> : forwards.map((c, i) => <ForwardBlock key={i} c={c} />)}
          {split && <QuoteStack blocks={split.quotes} mid={mid} sid={sid} />}
          {words}
        </div>
      ) : (
        <div className={`max-w-full rounded-[16px] ${fill} px-4 py-2.5 font-sans text-ink ${pending ? "opacity-60" : ""}`} title={clock(ts)} data-hit-target>
          {words}
        </div>
      )}
      {long && (
        <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} className="rounded-[6px] px-1.5 py-0.5 text-[12px] font-medium whitespace-nowrap text-ink-3 transition-[background-color,color,scale,transform] duration-150 ease-(--ease-out-strong) hover:bg-hover-2 hover:text-ink-2 active:scale-[0.97] motion-reduce:transition-none">
          {open ? t("chat-segmentview.collapse") : t("chat-segmentview.expand")}
        </button>
      )}
    </div>
  );
}

/** "From WeChat" under a message that came from a channel (its client_msg_id is `<channel>-…`);
 *  else, under the message an extension opened the conversation with, "From Lark /agent" / "From an extension" (#3758),
 *  its mark the opening extension's icon as on the conversation's rows and its words that extension's name while the icon
 *  is drawn (#10358). */
function ChannelSource({ clientMsgId, openedBy }: { clientMsgId?: string; openedBy?: { origin: ExternalOrigin; opened_by?: string; manager?: string } }) {
  const via = channelOf(clientMsgId, useChannels());
  // the extension whose own icon the mark draws, which the words then name (#10358)
  const [name, setName] = useState<string>();
  if (!via && openedBy) {
    return (
      <span data-opened-by={openedBy.origin} className="flex items-center gap-1.5 whitespace-nowrap text-[11px] text-ink-3">
        <OriginMark origin={openedBy.origin} size={14} decorative session={openedBy} onName={setName} />
        {openerFrom(openedBy.origin, name)}
      </span>
    );
  }
  if (!via) return null;
  return (
    <span className="flex items-center gap-1.5 whitespace-nowrap text-[11px] text-ink-3">
      <ChannelMark c={via} size={14} />
      {t("chat-segmentview.from", { name: via.name })}
    </span>
  );
}

/** An answer's prose at the chat's body size with airy editorial leading (1.72). Important: styles.css's `.prose-j`
 *  (14 / 1.7) is outside Tailwind's layers, so it would win over a plain utility. */
const ANSWER = "[&_.prose-j]:text-[length:var(--chat-body)]! [&_.prose-j]:leading-[1.72]!";

/** What the agent said, without a bubble, the chat's body size (15, 16 on a phone) / 1.6. `fade`: it just came in, its
 *  words fade in once. Still streaming, it has the same right-click / long-press menu (copy what is there so far, quote
 *  it); only a finished turn's last reply has the bar under it (`bar`, #5314).
 *  Memo'd: the chat context changes with every streamed item, so each SegmentView re-renders; a finished answer's
 *  props stay the same and it is skipped (#3024). */
export const Answer = memo(function Answer({ item, streaming, mid, sid, fade, stripCloseout, bar, cut, usage, usageMs, ttftMs }: {
  item?: Item;
  streaming?: string;
  mid?: string;
  sid?: string;
  fade?: boolean;
  /** The card above the transcript shows its closeout (#3612): drawn without the raw block. */
  stripCloseout?: typeof stripCloseoutText;
  /** The bar under it (ReplyBar): a finished turn's last reply, never narration in a work summary. */
  bar?: boolean;
  /** Its turn ended in a model error or an interruption right after these words (Segment.cut): cindy's line above them. */
  cut?: boolean;
  /** With `bar`: what its exchange used (lib/turn-usage.ts usageKey, a string so the memo holds), its work time, and streaming TTFT. */
  usage?: string;
  usageMs?: number;
  ttftMs?: number;
}) {
  // a long answer the hub cut for the page: its start, and "View full text" under it
  const live = streaming !== undefined;
  const full = useFullText(live ? undefined : item, mid, sid);
  const text = streaming ?? (stripCloseout ? stripCloseout(full.text, !item?.full_size || (Boolean(mid && sid) && !full.more)) : full.text);
  if (!text) return null;
  const cwd = mid && sid ? getSession(mid, sid)?.cwd : undefined;
  return (
    <MessageActions text={text} ts={item?.ts} itemId={item?.id} bar={bar} usage={bar ? usage : undefined} usageMs={bar ? usageMs : undefined} ttftMs={bar ? ttftMs : undefined}
      translationSource={item?.full_size ? {
        key: JSON.stringify([mid, sid, item.id, item.text, item.full_size, item.source_fingerprint, Boolean(stripCloseout)]),
        load: async () => {
          const complete = await full.getText();
          return stripCloseout ? stripCloseout(complete) : complete;
        },
      } : undefined}>
      {cut && <p className="mb-1 text-[13px] leading-[1.384615] text-ink-2" data-cut-short>{t("chat-messageactions.cut_short")}</p>}
      <div className={ANSWER} style={fade && !live ? { animation: `answer-settle 280ms ${EASE} both` } : undefined} data-answer={live ? "streaming" : true} data-items={item?.id}>
        {/* the cards in it are this item's blocks (ui/AssistantCard.tsx): `<item id>/<n>` */}
        <CardItem.Provider value={item?.id ? { id: item.id, text } : null}>
          <Markdown text={text} mid={mid} cwd={cwd} streaming={live || undefined} fade={fade || undefined} lineBreaks />
        </CardItem.Provider>
      </div>
      {full.more}
      {!live && mid && sid && <ReplyFiles text={text} mid={mid} cwd={cwd} />}
      {!live && mid && sid && <ReplyPorts text={text} mid={mid} sid={sid} cwd={cwd} />}
    </MessageActions>
  );
});

/** A piece of the agent's words in its exchange. It fades in once when it just came in: it reads its own id
 *  (fresh-words.ts), so a new reply renders these words, not every exchange. `latest`: the exchange's latest words (the
 *  closeout reader, the cut-short line and, with `done`, the reply bar: cindy never draws the bar while the turn runs). */
const Words = memo(function Words({ item, streaming, mid, sid, latest, noCloseout, done, cut, usage, usageMs, ttftMs }: { item?: Item; streaming?: string; mid: string; sid: string; latest?: boolean; noCloseout?: boolean; done?: boolean; cut?: boolean; usage?: string; usageMs?: number; ttftMs?: number }) {
  const persisted = streaming === undefined ? item?.id : undefined;
  const fresh = useFreshWords(mid, sid, persisted);
  const bar = Boolean(done && persisted);
  const stripCloseout = useCloseoutReader(Boolean(noCloseout));
  if (stripCloseout && item?.text?.trim() && !item.full_size && !stripCloseout(item.text)) return null;
  return <div data-latest-text={latest || undefined} data-words><Answer item={streaming === undefined ? item : undefined} streaming={streaming} mid={mid} sid={sid} fade={persisted ? fresh : undefined} stripCloseout={stripCloseout} bar={bar || undefined} cut={cut} usage={bar ? usage : undefined} usageMs={bar ? usageMs : undefined} ttftMs={bar ? ttftMs : undefined} /></div>;
});

export const SegmentView = memo(function SegmentView({
  seg,
  live,
  mid,
  sid,
  streaming,
  batchedWords,
  expandFold,
  trace,
  offscreen,
  busy,
}: {
  seg: Segment;
  live: boolean;
  mid: string;
  sid: string;
  streaming?: string;
  /** The live words are an agent's as Claude shows them: a batch of lines at a time, and still while a tool runs. The
   *  live indicator stays at the end, under them; the headless assistant's words stream as they are written and stand
   *  for it. */
  batchedWords?: boolean;
  expandFold: (f: Item) => Promise<boolean | void>;
  /** An opened supervision round shows its original input instead of the injected hint. */
  trace?: boolean;
  /** A finished exchange before the last two: off screen it is not drawn (the `seg-cv` class), and the chat watches its
   *  height through this (ChatView `watchSegment`, returning the unwatch) to keep the view still when it changes. */
  offscreen?: (section: HTMLElement) => () => void;
  /** The chat's last exchange while the chat is still at work (working, a message of yours on its way or being
   *  answered): drawn finished for a moment when a send drops the live line, its words get no reply bar yet, so the
   *  bar does not come and go (#5314). */
  busy?: boolean;
}) {
  const { subagents, tasks, scope, workflowCalls = NO_WORKFLOW_CALLS, lineStyle, closeoutSegment, children } = useChatContext();
  const own = useMemo(() => exchangeSteps(seg), [seg]);
  // the subagents its subagents launched, each a card of its own by when it started (#5030); a trace shows the calls only.
  // Keyed by what places them, not the list: that changes with every subagent's tokens and steps.
  const nesting = trace ? "" : nestingKey(subagents);
  // the child sessions that started during this exchange, each a card among its steps (#5120, lib/child-cards.ts)
  const placed = trace ? undefined : children?.get(seg.id);
  const steps = useMemo(
    () => withChildSessions(nesting ? withNestedSubagents(own, subagents) : own, placed),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [own, nesting, placed],
  );
  const turnStart = seg.user?.ts ?? seg.start;
  // one plan card per exchange, where its latest list was written (cindy); the other updates are rows
  const plan = useMemo(() => planCardStep(steps), [steps]);
  const p = useMemo<WorkProps>(() => {
    const agent = (s: ToolStep) =>
      isNestedSubagentStep(s) ? nestedSubagentStatus(subagentFor(subagents, s.use)) : agentStatus(s.result, subagentFor(subagents, s.use), live, s.use.tool_use_id ? tasks.notes.get(s.use.tool_use_id) : undefined);
    return { mid, sid, scope, expandFold, agentStatus: agent, segment: seg.id, workflowCalls, trace, turnLive: live, plan };
  }, [mid, sid, scope, expandFold, subagents, tasks, live, seg.id, workflowCalls, trace, plan]);
  const last = steps.at(-1);
  // live words that are not yet the last persisted words: written after everything here
  const newWords = streaming !== undefined && (last?.kind !== "text" || last.item.text?.trimEnd() !== streaming.trimEnd());
  const nodes = useMemo(
    () => workBlocks(steps, { live, turnStart, streaming: newWords }),
    [steps, live, turnStart, newWords],
  );
  useCallSeeker(scope, nodes);
  // the pictures the agent looked at this turn, before its last words
  const images = useMemo(() => turnImages(seg, getState().machines.find((m) => m.id === mid)?.name), [seg, mid]);
  // One continuous answer: every piece of words stays where it was written, each stretch of work between them a row.
  // Its last words are the answer's (the reply bar once the turn is over, also with a stretch after them: a stopped
  // turn's, a child session's card); words still streaming are drawn after everything, in place of the last persisted
  // words when they are those words.
  const tail = nodes.at(-1);
  const latestWords = newWords ? undefined : lastWords(nodes);
  // Streamed words and the item they land as are one element: the item takes the stream's key, and keeps it after.
  const streamKeys = useRef<{ next: number; current?: string; items: Map<string, string> }>({ next: 0, items: new Map() }).current;
  if (newWords) streamKeys.current ??= `streaming-${streamKeys.next++}`;
  else if (streamKeys.current && latestWords) {
    if (!streamKeys.items.has(latestWords.id)) streamKeys.items.set(latestWords.id, streamKeys.current);
    streamKeys.current = undefined;
  }
  // The card above the transcript shows this exchange's closeout (#3612): its last words are drawn without the raw
  // block, and not at all when the block was all they said.
  const noCloseout = closeoutSegment === seg.id && streaming === undefined;
  // the exchange's one live indicator, at its end: the live tail stretch, else this row (the headless assistant's
  // streaming words stand for it)
  const working = live && (streaming === undefined || batchedWords) && !(tail?.kind === "group" && tail.live);
  const since = useMemo(() => (working ? workTime(steps, turnStart).since : undefined), [working, steps, turnStart]);
  const liveRow = working && <WorkGroup g={{ kind: "group", id: `work-live-${seg.id}`, children: [], live: true, startedAt: since }} p={p} />;
  const imageStrip = images.length > 0 && <TurnImages images={images} mid={mid} sid={sid} cwd={getSession(mid, sid)?.cwd} />;
  const box = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    const el = box.current;
    return offscreen && el ? offscreen(el) : undefined;
  }, [offscreen]);
  const divided = Boolean(seg.modelChange);
  // the files this finished Agent exchange changed (cindy's TurnChangesCard): its own edits, never a Persona
  // conversation's (super rules 0 and 4), never while it runs, never in a supervision trace, and never when the page
  // starts inside it (no message of the user's): edits before the page would be missing from its counts
  const counted = !live && !trace && lineStyle === "agents" && Boolean(seg.user);
  // the session's folder from the store, so the card follows it when it comes in after the transcript
  const cwd = useStore(() => (counted ? getSession(mid, sid)?.cwd : undefined));
  const changes = useMemo(() => (counted ? turnChanges(own, cwd) : null), [counted, own, cwd]);
  // the web pages the finished exchange used, in a Persona conversation and an Agent's alike (#9132)
  const sourced = !live && !trace && Boolean(seg.user) && own.some(webStep);
  // what the finished exchange used (#5426) and how long it worked, said once under its answer
  const usage = !live && !trace ? usageKey(seg.usage) : undefined;
  const usageMs = useMemo(() => (!live && !trace && steps.some((s) => s.kind !== "text") ? workTime(steps, turnStart).ms : undefined), [live, trace, steps, turnStart]);
  const switched = Boolean(seg.engineSwitch);
  const guess = useMemo(() => (offscreen ? ({ "--seg-est": `${segmentHeight(seg.user, nodes, divided, switched) + (changes ? 12 + turnChangesHeight(changes) : 0) + (sourced ? 14 + SOURCES_HEIGHT : 0)}px` } as CSSProperties) : undefined), [offscreen, seg.user, nodes, divided, switched, changes, sourced]);
  // One keyed list, so a paragraph stays the same element when newer words come after it, and streamed words stay the
  // same element when they land: the pictures the agent looked at go before its latest words.
  const words = (key: string, w: ReactNode) => (
    <Fragment key={key}>
      <Suspense fallback={null}>{w}</Suspense>
    </Fragment>
  );
  const drawn: ReactNode[] = [];
  for (const n of nodes) {
    if (n.kind === "group") drawn.push(<WorkGroup key={n.id} g={n} p={p} />);
    else if (n.step.kind !== "text") drawn.push(<StepView key={n.step.id} step={n.step} p={p} live={live} />);
    else if (n.step.item !== latestWords) drawn.push(words(streamKeys.items.get(n.step.id) ?? n.step.id, <Words item={n.step.item} mid={mid} sid={sid} />));
    else {
      if (imageStrip) drawn.push(<Fragment key="turn-images">{imageStrip}</Fragment>);
      drawn.push(words(streamKeys.items.get(n.step.id) ?? n.step.id, <Words item={n.step.item} streaming={streaming} mid={mid} sid={sid} latest noCloseout={noCloseout || undefined}
        done={(!live && !busy && streaming === undefined) || undefined} cut={(!live && seg.cut) || undefined} usage={usage} usageMs={usageMs} ttftMs={!live && !trace ? seg.ttftMs : undefined} />));
    }
  }
  if (newWords) {
    if (imageStrip) drawn.push(<Fragment key="turn-images">{imageStrip}</Fragment>);
    drawn.push(words(streamKeys.current!, <Words streaming={streaming} mid={mid} sid={sid} latest />));
  } else if (!latestWords && imageStrip) drawn.push(<Fragment key="turn-images">{imageStrip}</Fragment>);
  return (
    <section ref={box} className={offscreen ? "seg-cv flex flex-col gap-3.5" : "flex flex-col gap-3.5"} style={guess} data-segment>
      {/* the first message the conversation's new engine got (#5054): the switch divider above it, the user's words below */}
      {seg.engineSwitch && <EngineSwitchDivider sw={seg.engineSwitch} id={seg.id} item={switchItem(seg)} mid={mid} sid={sid} />}
      {trace && seg.user?.rawInput && !seg.user.promptHidden && !seg.user.sourceInTrace ? <RawInjectedInput items={seg.user.rawInput} mid={mid} sid={sid} /> : seg.user && (seg.user.source ? <SourceMessage msg={seg.user} mid={mid} sid={sid} /> : seg.user.injected ? <InjectedHint msg={seg.user} sid={sid} /> : seg.user.parts ? <MergedBubble msg={seg.user} mid={mid} sid={sid} /> : seg.user.from ? <PeerMessage msg={seg.user} /> : isLoopTick(seg.user) ? <LoopTick msg={seg.user} /> : <UserBubble msg={seg.user} mid={mid} sid={sid} />)}
      {/* the reply below came from another model than the one before it (cindy: after the message that started it) */}
      {seg.modelChange && <ModelChangeDivider from={seg.modelChange.from} to={seg.modelChange.to} />}
      {/* the words and the stretches of work between them, in the order they came; each stretch a collapsed row */}
      {drawn}
      {liveRow}
      {sourced && <Suspense key="turn-sources" fallback={null}><TurnSources steps={own} /></Suspense>}
      {changes && <TurnChanges key="turn-changes" changes={changes} mid={mid} sid={sid} cwd={cwd} />}
    </section>
  );
});

/** The last piece of words among an exchange's nodes. */
function lastWords(nodes: WorkNode[]): Item | undefined {
  for (let i = nodes.length - 1; i >= 0; i--) {
    const n = nodes[i];
    if (n.kind === "step" && n.step.kind === "text") return n.step.item;
  }
  return undefined;
}

/** The Sources row's height as drawn (TurnSources.tsx: one line of chips), for an exchange's estimate. */
const SOURCES_HEIGHT = 28;

/** A step that may name web pages: a web call, or folded work that carries some (lib/turn-sources.ts reads them). */
function webStep(s: Step): boolean {
  return s.kind === "tool" ? s.use.tool_name === "WebSearch" || s.use.tool_name === "WebFetch" : s.kind === "fold" && Boolean(s.item.fold?.sources?.length);
}

/** The message's transcript item that carried the switch's block (its rest is fetched from it when the hub cut it). */
function switchItem(seg: Segment): Item | undefined {
  return seg.user?.rawInput?.find((i) => i.kind === "text" && i.text?.includes(`kind="${ENGINE_SWITCH_KIND}"`));
}

function RawInjectedInput({ items, mid, sid }: { items: Item[]; mid: string; sid: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1" data-supervision-input>
      <span className="text-[length:var(--chat-secondary)] text-ink-3">{t("chat-lines.supervision_input")}</span>
      {items.map((item) => item.kind === "image" ? <Thumb key={item.id} item={item} mid={mid} sid={sid} /> : <RawInjectedText key={item.id} item={item} mid={mid} sid={sid} />)}
    </div>
  );
}

function RawInjectedText({ item, mid, sid }: { item: Item; mid: string; sid: string }) {
  const full = useFullText(item, mid, sid);
  return (
    <div data-items={item.id}>
      <pre className="rounded-card bg-inset p-3 text-[length:var(--chat-secondary)] whitespace-pre-wrap text-ink-2 [overflow-wrap:anywhere]">{full.text}</pre>
      {full.more}
    </div>
  );
}
