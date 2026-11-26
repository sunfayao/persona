import { storeValue } from "../lib/compatibility";
// The Persona tab: the headless assistant on the hub's machine. `/` (and the old `/persona`) is the main
// conversation, the only one this page shows. With no main conversation yet, the first message on `/` starts it.
import { lazy, Suspense, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { MentionSource } from "./chat/ComposerPanel";
import { startConversation } from "../lib/assistant-start";
import { feedCacheScope, mainConversationMachine, readFeedCache } from "../lib/feed-cache";
import { startOfDay } from "../lib/feed";
import { uuid } from "../lib/format";
import { codedFailure, helpTopic, passesBySelf, refreshReadiness, type CodedFailure } from "../lib/readiness";
import { navigate } from "../lib/router";
import { hubHas, refreshAssistant, useStore } from "../lib/store";
import { effectiveLauncher, NEW_CONVERSATION, pickedEffort, pickedFast, pickedLauncher, pickedModel, pickEffort, pickFast, pickLauncher, usePickedLauncher } from "../lib/assistant-chat";
import { ASSISTANT_START_SERVICE_TIER_FEATURE } from "../lib/assistant-models";
import { noteSent, outboxMessage } from "../lib/outbox";
import { CLEAR_MAIN_FEATURE, isClearCommand, mainCommands, useMainCleared } from "../lib/main-clear";
import { Button, cx, EASE, IconButton, Loading } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { ChatView, Header } from "./chat/ChatView";
import { MainRuntimeSlot } from "./assistant/MainRuntimeSlot";
import { UserBubble } from "./chat/SegmentView";
import { Composer, type Outgoing } from "./chat/Composer";
import { AssistantModelPill } from "./chat/ModelPill";
import { PersonaAvatar, usePersona } from "./assistant/Persona";
import { ChatOnlyNote, FailureCard, ReadinessCard, useReadinessGate } from "./assistant/Readiness";
import { DayLine } from "./assistant/Tasks";
import { ShellHeaderSlots, ShellSwitch } from "./ShellSwitch";
import { StartSessionButton } from "./StartSessionButton";
import { t } from "../i18n";
import { useAssistantTitle, useShownOffline } from "./ManagerParts";
import { chosenManager, noteConversation, ownerHeaders, shownManager, useShownManager } from "../lib/manager-choice";

/** Every computer's tokens a day under the things to ask (#9506): a lazy chunk, not on the first screen. */
const TokenCalendar = lazy(() => import("./assistant/TokenCalendar"));

/** The example questions of a hub without readiness (its `suggestions` replace them). */
const SUGGESTIONS = [t("personaassistant.suggestion_meetings"), t("personaassistant.suggestion_agents"), t("personaassistant.suggestion_lark"), t("personaassistant.suggestion_projects")];

const starting = new Set<string>();

/** How long a start may take before the page says it is still at it (#1697). */
export const SLOW_START_MS = 8000;
/** The input box's move between the page's middle and the bottom, and the conversation coming in (#1697). */
const MOVE_MS = 220;
const NO_SESSIONS: never[] = [];
const noop = () => {};

// @ in a new conversation: other sessions as context; no chat yet, so no files it touched (typed / and ~ paths still browse)

export function PersonaAssistant({ at, onToggleSidebar }: { at?: string; onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const assistantError = useStore((s) => s.assistantError);
  const loaded = useStore((s) => s.loaded);
  // the shown manager offline (with several, ManagerParts): its history stays, nothing goes
  const offline = useShownOffline();
  const scope = feedCacheScope();
  const manager = chosenManager() ?? "";
  const cached = useMemo(() => {
    if (assistant) return null;
    const page = readFeedCache(scope);
    return page && (page.manager ?? "") === manager ? page : null;
  }, [scope, manager, assistant]);
  const main = assistant?.main ?? cached?.sessionID;
  // this tab cleared the main conversation's context (/clear, lib/main-clear.ts): until the next one starts, its page
  // keeps the conversation's layout
  const cleared = useMainCleared(manager);
  const machine = mainConversationMachine(assistant) ?? cached?.machineID;
  if (main) {
    if (!machine) {
      // an offline manager's read fails as expected: its page says so (Starter) rather than offering Retry
      if (assistantError && !offline) return <AssistantLoadFailed error={assistantError} onToggleSidebar={onToggleSidebar} />;
      return <Centered><Loading /></Centered>;
    }
    // the main conversation is the phone's assistant page: "Assistant | Agents" in its top bar (ChatView's `topCenter`, narrow only)
    // keyed by the conversation, not its computer: the cached first view's computer giving way to the hub's answer is the
    // same chat, and a new key would take the typing person's words, pictures and focus with the old page (#11237)
    return (
      <ChatView
        key={`${scope}/${manager}/${main}`}
        mid={machine}
        sid={main}
        assistant
        starting={starting.has(main)}
        onToggleSidebar={onToggleSidebar}
        topCenter={<ShellSwitch page="assistant" />}
        headerActions={<StartSessionButton compact />}
        mainCache={scope ? { scope, manager } : undefined}
        refreshError={assistantError ?? undefined}
        onRefresh={() => void refreshAssistant().catch(() => {})}
        offline={offline?.composer}
        landAt={at}
      />
    );
  }
  if (!loaded && !assistant && !assistantError) return <Centered><Loading /></Centered>;
  if (assistantError && !assistant && !offline) {
    return <AssistantLoadFailed error={assistantError} onToggleSidebar={onToggleSidebar} />;
  }
  return <Starter cleared={cleared} onToggleSidebar={onToggleSidebar} />;
}

function AssistantLoadFailed({ error, onToggleSidebar }: { error: string; onToggleSidebar: () => void }) {
  const [retrying, setRetrying] = useState(false);
  const retry = async () => {
    setRetrying(true);
    try {
      await refreshAssistant();
    } catch {
      // Keep the failure displayed with the latest error
    } finally {
      setRetrying(false);
    }
  };
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 px-3 sm:px-4">
        <ShellHeaderSlots page="assistant" onToggleSidebar={onToggleSidebar} end={<IconButton icon="info" label={t("personaassistant.profile")} onClick={() => navigate({ name: "profile" })} />} />
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-5 text-center">
        <p className="text-[13px] text-ink-2">{t("personaassistant.load_failed", { error })}</p>
        <Button onClick={retry} busy={retrying} icon="retry">
          {t("personaassistant.retry")}
        </Button>
      </div>
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full items-center justify-center">{children}</div>;
}

/** The main conversation's start page: the centered box with the example questions, or, for the main conversation this tab
 *  just cleared (`cleared`, as Cindy's /clear leaves an empty chat), the conversation's own layout with nothing in it yet. */
function Starter({ cleared, onToggleSidebar }: { cleared?: boolean; onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const manager = useShownManager();
  const selectedLauncher = usePickedLauncher(NEW_CONVERSATION);
  const mention = useMemo<MentionSource>(() => ({ self: "persona-new", files: () => [], workspaces: true, repositories: manager,
    skills: { manager, launcher: selectedLauncher, environment: assistant?.launcher ?? "" },
  }), [manager, selectedLauncher, assistant?.launcher]);
  const persona = usePersona();
  const [seed, setSeed] = useState(0);

  const gate = useReadinessGate();
  const [failure, setFailure] = useState<CodedFailure | null>(null);
  const offline = useShownOffline();
  const mainTitle = useAssistantTitle();
  // the first message while its start is on its way (#1697): the page is the conversation's already, the message at its
  // top and "Preparing" under it, the box at the bottom, so nothing moves when the conversation itself takes over
  const [sending, setSending] = useState<{ out: Outgoing; launcher: string; at: number } | null>(null);
  const slow = useSlowStart(Boolean(sending));
  // the conversation's layout (its header, the box docked at the bottom): while the first message's start is on its
  // way, and in place of the main conversation just cleared, so nothing moves when it empties or when it starts again
  const docked = Boolean(sending) || Boolean(cleared);
  // /clear on the main conversation's page while there is none: nothing to clear, and never its first message
  const clearable = useStore((s) => hubHas(s.features, CLEAR_MAIN_FEATURE));
  const startServiceTier = useStore((s) => hubHas(s.features, ASSISTANT_START_SERVICE_TIER_FEATURE));
  const commands = useMemo(() => (clearable && docked ? mainCommands() : []), [clearable, docked]);
  // the box is the same element in both layouts (a refused start gets its words and pictures back in it): it moves
  const box = useRef<HTMLDivElement>(null);
  const boxFrom = useRef<number | null>(null);
  const moveBox = (next: typeof sending) => {
    boxFrom.current = box.current?.getBoundingClientRect().top ?? null;
    setSending(next);
  };
  useLayoutEffect(() => {
    const el = box.current;
    const from = boxFrom.current;
    boxFrom.current = null;
    if (!el || from === null || typeof el.animate !== "function" || matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    const dy = from - el.getBoundingClientRect().top;
    if (Math.abs(dy) > 1) el.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], { duration: MOVE_MS, easing: EASE });
  }, [sending]);

  const start = async (out: Outgoing): Promise<boolean> => {
    if (clearable && isClearCommand(out.text, out.images.length)) return true;
    setFailure(null);
    moveBox({ out, launcher: effectiveLauncher({ isNew: true, picked: pickedLauncher(NEW_CONVERSATION), config: assistant?.launcher }), at: Date.now() });
    const sent = await startOnce(out);
    // started: the page moves to the conversation, where the message is; refused: back to this page, its words in the box
    if (!sent) moveBox(null);
    return sent;
  };

  const startOnce = async (out: Outgoing): Promise<boolean> => {
    // what the model pill picked for this new chat (#5033): launcher, model, effort and native speed stay independent
    const launcher = pickedLauncher(NEW_CONVERSATION);
    // written for the manager shown now: its retries go there even if the switcher moves meanwhile
    const chosen = chosenManager() ?? undefined;
    const body = { prompt: out.text, images: out.images.length ? out.images : undefined, launcher, model: launcher ? pickedModel(NEW_CONVERSATION) : undefined, effort: pickedEffort(NEW_CONVERSATION), service_tier: startServiceTier && !chosen && pickedFast() ? "priority" : undefined, main: true };
    const headers = ownerHeaders(chosen);
    const from = shownManager();
    // one op id for this message, its quiet retries included (#1382); sent again after a refusal, it gets a new one
    const opId = uuid();
    for (let attempt = 0; ; attempt++) {
      try {
        const r = await startConversation(body, headers, opId);
        // its own requests (feed, stop…) go to the manager it started with, whatever the page shows later
        noteConversation(r.session.session_id, chosen);
        starting.add(r.session.session_id);
        // the prompt shows in the conversation at once (it started with it), until its transcript has it
        noteSent(r.session.machine_id, r.session.session_id, out);
        if (launcher) pickLauncher(NEW_CONVERSATION, undefined);
        pickEffort(NEW_CONVERSATION, undefined);
        pickFast(false);
        void refreshAssistant().catch(() => {});
        return true;
      } catch (e) {
        // the assistant restarting: a few seconds, tried again without a word
        if (passesBySelf(e) && attempt < 3) {
          await new Promise((done) => setTimeout(done, 2000));
          // the page moved to another manager meanwhile: this one's words stay in the box, nothing goes elsewhere
          if (shownManager() !== from) return false;
          continue;
        }
        // a card, not a toast (4.4); the readiness changed with it, most likely
        setFailure(codedFailure(e));
        void refreshReadiness({ force: true });
        return false;
      }
    }
  };

  const fill = (text: string) => {
    storeValue("draft:persona-new", text);
    setSeed((x) => x + 1);
  };

  // the hub's example questions when it gives them: a local one opens help here and is not sent (4.4)
  const hubSuggestions = gate.readiness?.suggestions;
  const suggestions: { key: string; title: string; onClick: () => void; hint?: string }[] = hubSuggestions
    ? hubSuggestions.map((x) => ({
        key: x.id,
        title: x.title,
        hint: x.needs?.includes("phone_calendar") ? t("personaassistant.needs_calendar") : undefined,
        onClick: () => (x.local ? gate.help(helpTopic(x.id)) : fill(x.prompt || x.title)),
      }))
    : SUGGESTIONS.map((x) => ({ key: x, title: x, onClick: () => fill(x) }));
  const setup = Boolean(gate.step);
  const mid = assistant?.machine_id ?? "";
  return (
    // data-new-conversation: the page the shared sync scenarios start a conversation from (spec/fixtures/sync, #1382)
    <div className="flex h-full flex-col" data-new-conversation>
      {docked ? (
        // the conversation page's own header, as it will read once the conversation is there
        <Header
          title={mainTitle}
          machineName=""
          assistant
          main
          siblings={NO_SESSIONS}
          onToggleSidebar={onToggleSidebar}
          onInterrupt={noop}
          topCenter={<ShellSwitch page="assistant" />}
          actions={<StartSessionButton compact />}
        />
      ) : (
      <header className="flex h-14 shrink-0 items-center gap-2 px-3 sm:px-4">
        {/* a phone's assistant page: "Assistant | Agents" between the same side slots as ChatView's and the Agents page's */}
        <ShellHeaderSlots page="assistant" onToggleSidebar={onToggleSidebar} end={<><StartSessionButton compact /><IconButton icon="info" label={t("personaassistant.profile")} onClick={() => navigate({ name: "profile" })} /></>} />
        <span className="flex-1 max-lg:hidden" />
        <StartSessionButton className="max-lg:hidden" />
        <button type="button" onClick={() => navigate({ name: "profile" })} className="flex h-7 items-center gap-1.5 rounded-[8px] px-2 text-[12px] font-medium whitespace-nowrap text-ink-2 shadow-btn hover:bg-hover hover:text-ink max-lg:hidden">
          <PersonaAvatar size={16} fallback={<Icon name="info" size={13} />} /> {t("personaassistant.profile")}
        </button>
      </header>
      )}
      <MainRuntimeSlot starting={Boolean(sending)} />
      <div className={docked ? "flex min-h-0 flex-1 flex-col" : "flex min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-5 pb-[10vh]"}>
        <div className={docked ? "contents" : "w-full max-w-[640px]"} style={docked ? undefined : { animation: `fade-up 450ms ${EASE} both` }}>
          {sending ? (
            // the conversation page's middle (ChatView): the message where its bubble will be, "Preparing" where the answer comes
            <div className="relative min-h-0 flex-1 overflow-y-auto" data-start-pending>
              <div className="mx-auto flex w-full max-w-[880px] flex-col gap-7 px-5 pt-6 pb-10 sm:px-8" style={{ animation: `fade-up ${MOVE_MS}ms ${EASE} ${MOVE_MS / 2}ms both` }}>
                {/* what the conversation page opens with, before and after its transcript comes (ChatView's `opening`) */}
                <div className="text-center text-[11.5px] text-ink-3">{t("chat-chatview.chat_start")}</div>
                <DayLine date={startOfDay(sending.at)} />
                <div className="flex flex-col gap-1.5">
                  <UserBubble msg={outboxMessage({ id: "starting", text: sending.out.text, images: sending.out.images, at: sending.at, state: "sending" })} mid={mid} sid="" sendState="sending" starting />
                </div>
                <div className="flex items-center gap-2.5">
                  <Loading label={slow ? t("personaassistant.still_starting", { launcher: sending.launcher }) : t("chat-chatview.preparing")} />
                </div>
              </div>
            </div>
          ) : docked ? (
            // the cleared main conversation: an empty chat, as Cindy's /clear leaves it
            <div className="min-h-0 flex-1" data-main-cleared />
          ) : (
          <div className="mb-6 flex flex-col items-center text-center">
            <PersonaAvatar
              size={56}
              className="mb-4 shadow-raised"
              fallback={
                <span className="mb-4 flex size-11 items-center justify-center rounded-[12px] bg-ink text-surface shadow-raised">
                  <Icon name="spark" size={20} />
                </span>
              }
            />
            <h1 className="text-[22px] font-semibold tracking-tight text-ink">{t("personaassistant.empty_title")}</h1>
            <p className="mt-1.5 text-[13px] text-ink-2">
              {/* no computers named: the calendar under the questions took their place (#9506) */}
              {offline ? offline.body : setup ? t("personaassistant.empty_body_setup") : t("personaassistant.empty_body")}
            </p>
          </div>
          )}
          {/* files go to the assistant's machine, as in its conversations (iOS StarterComposer) */}
          {!sending && (gate.step || failure || gate.readiness?.mode === "chat_only") && (
            <div className={cx("mb-2.5 flex flex-col gap-2", docked && "mx-auto w-full max-w-[880px] px-4 sm:px-7")}>
              {failure ? <FailureCard failure={failure} onClose={() => setFailure(null)} onHelp={gate.help} onConfirm={gate.confirm} /> : <ReadinessCard gate={gate} />}
              <ChatOnlyNote gate={gate} />
            </div>
          )}
          {gate.dialog}
          {/* the box: in the page's middle, or docked at the bottom as the conversation's (ChatView's dock) while the start is on its way */}
          <div ref={box} className={docked ? "mx-auto flex w-full max-w-[880px] flex-col justify-end gap-2 px-4 pb-4 sm:px-7 [@media(max-height:500px)]:pb-2" : undefined}>
            <Composer key={seed} blocked={offline ? undefined : gate.blocked} disabled={Boolean(offline) || Boolean(sending)} draftKey="persona-new" placeholder={offline ? offline.composer : t("personaassistant.ask_placeholder", { name: persona.name })} machineId={assistant?.machine_id} mention={mention} onSend={start} autoFocus commands={commands} trailing={assistant && !sending ? <AssistantModelPill mainStart /> : undefined} />
          </div>
          {!docked && (
          <div className="mt-4 grid grid-cols-1 gap-1.5 sm:grid-cols-2">
            {/* an offline manager takes nothing: no questions to fill in */}
            {!offline && suggestions.map((x, i) => (
              <button
                key={x.key}
                type="button"
                onClick={x.onClick}
                title={x.hint}
                className="flex items-center gap-2 rounded-[10px] px-3 py-2 text-left text-[12.5px] text-ink-2 shadow-hairline transition-colors hover:bg-hover hover:text-ink"
                style={{ animation: `fade-up 380ms ${EASE} ${120 + i * 60}ms both` }}
              >
                <Icon name={hubSuggestions?.[i]?.local ? "info" : "chevronRight"} size={12} stroke={2.2} className="shrink-0 text-ink-3" />
                <span className="min-w-0 flex-1 truncate">{x.title}</span>
                {x.hint && <span className="shrink-0 text-[11px] whitespace-nowrap text-ink-3">{x.hint}</span>}
              </button>
            ))}
          </div>
          )}
          {!docked && !offline && !setup && !sending && (
            <Suspense fallback={null}>
              <TokenCalendar />
            </Suspense>
          )}
        </div>
      </div>
    </div>
  );
}

/** True once a start has taken SLOW_START_MS: "Preparing" gives way to "Still starting claude…" so it doesn't look stuck. */
function useSlowStart(on: boolean): boolean {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    setSlow(false);
    if (!on) return;
    const timer = setTimeout(() => setSlow(true), SLOW_START_MS);
    return () => clearTimeout(timer);
  }, [on]);
  return slow;
}
