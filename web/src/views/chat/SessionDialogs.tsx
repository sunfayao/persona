// The chat's ⋯ menu dialogs: session info, rename (the agent / its space), delete, close, and the header's permission mode.
// Session info and Rename / Rename space / Delete load as chunks of their own, with their worktree cleanup beside them;
// what stays here is what the first screen uses.
import { useRef } from "react";
import { api, ApiError, errorText, HubTimeoutError, pastHub, sessionPath } from "../../lib/api";
import { isTraeCli } from "../../lib/agent-kind";
import { BYPASS_MODE, renameBody } from "../../lib/session-actions";
import { bypassRestarts } from "../../lib/header-mode";
import { canQueueTerminalEffort, modelBusy, sessionLauncher } from "../../lib/model-switch";
import { getSession, refreshAssistant, upsertSession } from "../../lib/store";
import type { Session } from "../../lib/types";
import { toast } from "../../ui/atoms";
import { t } from "../../i18n";
import { lazyDialog } from "../LazyOverlay";

// "Session info" (iOS SessionInfoSheet, SessionInfoDialog.tsx): its own chunk, loaded on the dialog's first open, so
// its fetch and rows are not the first screen's (#8261). The same component as before for the header that draws it.
export const SessionInfoDialog = lazyDialog(() => import("./SessionInfoDialog").then((m) => m.SessionInfoDialog));

// Rename, Rename space and Delete (SessionMenuDialogs.tsx): one chunk of their own, loaded on the first open or, sooner,
// when a menu that offers one opens (preloadSessionDialogs: the chat's ⋯ menu, an Agent row's, a space's), so the click
// draws the dialog at once. Off the first screen (#10382).
const loadMenuDialogs = () => import("./SessionMenuDialogs");
export const RenameDialog = lazyDialog(() => loadMenuDialogs().then((m) => m.RenameDialog));
export const RenameSpaceDialog = lazyDialog(() => loadMenuDialogs().then((m) => m.RenameSpaceDialog));
export const DeleteSessionDialog = lazyDialog(() => loadMenuDialogs().then((m) => m.DeleteSessionDialog));
/** Starts loading the three, once (a failed load is asked again by the next menu or open). */
export function preloadSessionDialogs(): void {
  for (const d of [RenameDialog, RenameSpaceDialog, DeleteSessionDialog]) void d.preload().catch(() => {});
}

/** Saves a new title from the header's in-place rename (#5421, cindy's double-click on the title, which updates at once and
 *  puts the old title back if the save fails): only the title, the tab keeps its name. False when nothing changed or it
 *  failed (the error toasted). */
export async function renameTitle(session: Session, title: string): Promise<boolean> {
  const body = renameBody(session, "", title);
  if (!body?.title) return false;
  // the header and the list show the new name at once; the hub's push confirms it
  upsertSession({ ...session, title: body.title });
  try {
    await api(`${sessionPath(session.machine_id, session.session_id)}/rename`, { method: "POST", json: { title: body.title } });
    if (session.assistant) void refreshAssistant().catch(() => {});
    return true;
  } catch (e) {
    // back to the old title, unless something renamed it meanwhile
    const now = getSession(session.machine_id, session.session_id);
    if (!now || now.title === body.title) upsertSession({ ...(now ?? session), title: session.title });
    toast(t("chat-sessiondialogs.change_failed", { error: errorText(e) }), "error");
    return false;
  }
}

/** The hub gives a mode call 90 seconds. Five more lets its own refusal arrive first; a lost response still ends Apply. */
const MODE_CONFIRM_MS = pastHub(90_000);

function modeChangeError(e: unknown) {
  toast(e instanceof HubTimeoutError ? t("chat-sessiondialogs.change_unconfirmed") : t("chat-sessiondialogs.change_failed", { error: errorText(e) }), "error");
}

/** POST …/modes with a patch, drawn at once and put back if it is refused (the error toasted). A timeout may mean the
 *  change ran but its response was lost, so it stays drawn for the session push. `latest` is the session as it is now:
 *  a pick applies the model and then the effort, and the second must build on the session the first changed. */
export async function applyModes(latest: () => Session, patch: { permission_mode?: string; effort?: string; model?: string }): Promise<boolean> {
  const base = latest();
  const before = {
    permission_mode: base.permission_mode,
    effort: base.effort,
    model: base.model,
    bypass_pending: base.bypass_pending,
    pending_effort: base.pending_effort,
    effort_change_failed: base.effort_change_failed,
  };
  const queueEffort = Boolean(patch.effort) && canQueueTerminalEffort(base);
  const body = queueEffort ? { ...patch, queue_effort: true } : patch;
  // Full access the agent reaches only by a restart (#6476): drawn as waiting for it, in the mode it still runs in;
  // any other mode drops a restart that waits
  const restart = patch.permission_mode === BYPASS_MODE && bypassRestarts(base);
  const drawn: Partial<Session> = restart ? { ...patch, permission_mode: base.permission_mode, bypass_pending: true } : patch.permission_mode ? { ...patch, bypass_pending: undefined } : { ...patch };
  if (queueEffort && patch.effort) {
    if (patch.effort === base.effort && base.pending_effort) {
      drawn.effort = base.effort;
      drawn.pending_effort = undefined;
      drawn.effort_change_failed = undefined;
    } else if (modelBusy(base)) {
      drawn.effort = base.effort;
      drawn.pending_effort = patch.effort;
      drawn.effort_change_failed = undefined;
    } else {
      drawn.pending_effort = undefined;
      drawn.effort_change_failed = undefined;
    }
  }
  // TRAE publishes its native settings after readback. Leave the prior model visible while the request is pending.
  const optimistic = !(patch.model && isTraeCli(base.agent));
  if (optimistic) upsertSession({ ...base, ...drawn });
  try {
    const reply = await api<{ pending_effort?: string } | undefined>(`${sessionPath(base.machine_id, base.session_id)}/modes`, { method: "POST", json: body, timeout: MODE_CONFIRM_MS });
    const current = getSession(base.machine_id, base.session_id) ?? latest();
    if (!optimistic && current.model === before.model) {
      upsertSession({ ...current, model: patch.model });
    } else if (queueEffort && patch.effort) {
      if (typeof reply?.pending_effort === "string" && reply.pending_effort) {
        upsertSession({
          ...current,
          effort: current.effort === patch.effort ? before.effort : current.effort,
          pending_effort: reply.pending_effort,
          effort_change_failed: undefined,
        });
      } else {
        upsertSession({ ...current, effort: patch.effort, pending_effort: undefined, effort_change_failed: undefined });
      }
    }
    return true;
  } catch (e) {
    if (optimistic && !(e instanceof HubTimeoutError)) {
      const current = getSession(base.machine_id, base.session_id) ?? latest();
      upsertSession({ ...current, ...Object.fromEntries(Object.keys(drawn).map((k) => [k, before[k as keyof typeof before]])) });
    }
    modeChangeError(e);
    return false;
  }
}

/** applyModes for one session: the composer's permission picker and model pill, the header's mode. */
export function useModeSet(session: Session) {
  const latest = useRef(session);
  latest.current = session;
  const set = (patch: { permission_mode?: string; effort?: string; model?: string }): Promise<boolean> => applyModes(() => latest.current, patch);
  /** POST …/modes {launcher, from, model?, effort?} (#5035): another launcher records a switch that the user's next
   *  message carries out, the session's own drops one; `from` is the launcher the pick was made against, so one made on
   *  a session that switched meanwhile is refused (`switch_stale`). The pending switch shows at once (the pill's clock)
   *  and goes back to what it was if the hub says no, with its words. `agent`: the engine a launcher of the other engine
   *  runs (#5054), drawn as the node will record it (`pending_switch.agent`); it is not sent. */
  const switchLauncher = async (step: { launcher: string; from?: string; model?: string; effort?: string }, agent?: string): Promise<boolean> => {
    const base = latest.current;
    const before = base.pending_switch;
    const pending = { launcher: step.launcher, model: step.model, effort: step.effort, ...(agent ? { agent } : {}), at: new Date().toISOString() };
    const next: Session = { ...base, pending_switch: step.launcher === sessionLauncher(base) ? undefined : pending };
    upsertSession(next);
    latest.current = next; // a step after this one builds on it before the push draws it
    try {
      await api(`${sessionPath(base.machine_id, base.session_id)}/modes`, { method: "POST", json: step, timeout: MODE_CONFIRM_MS });
      return true;
    } catch (e) {
      if (!(e instanceof HubTimeoutError)) upsertSession({ ...latest.current, pending_switch: before });
      modeChangeError(e);
      return false;
    }
  };
  return { set, latest, switchLauncher };
}

/** POST …/modes {service_tier}: Codex's speed (the Speed submenu, the model pill). Nothing is drawn ahead of it: the
 *  session's push after the agent took it owns what is shown (DESIGN_SYSTEM.md "Codex speed controls"). */
export async function setServiceTier(s: Session, tier: string): Promise<boolean> {
  try {
    await api(`${sessionPath(s.machine_id, s.session_id)}/modes`, { method: "POST", json: { service_tier: tier }, timeout: MODE_CONFIRM_MS });
    return true;
  } catch (e) {
    modeChangeError(e);
    return false;
  }
}

const EFFORT_PENDING_CODE = "effort_change_pending";

/** Whether the node refused a send because a queued Claude Code thinking effort change is still pending. */
export function isEffortQueued(e: unknown): boolean {
  return e instanceof ApiError && e.code === EFFORT_PENDING_CODE;
}

/** Resolves true once the session's queued thinking effort is confirmed and cleared; false if it fails, times out, or the session disappears. */
export function effortQueueDone(mid: string, sid: string, { minMs = 1500, maxMs = 30 * 60_000, every = 1000 }: { minMs?: number; maxMs?: number; every?: number } = {}): Promise<boolean> {
  const start = Date.now();
  return new Promise((resolve) => {
    const tick = setInterval(() => {
      const s = getSession(mid, sid);
      const waited = Date.now() - start;
      const done = !s || s.effort_change_failed || waited >= maxMs ? false : waited >= minMs && !s.pending_effort ? true : undefined;
      if (done === undefined) return;
      clearInterval(tick);
      resolve(done);
    }, every);
  });
}
