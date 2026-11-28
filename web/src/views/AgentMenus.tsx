// The Agents list's menus (iOS AgentRowMenu / SpaceMenuItems): a row's ⋯ or right click, a space's ⋯ on its header,
// and the confirmation of "Mark all as seen". What they do is in lib/agent-actions.ts.
import { flipMute, flipPin, handToPersona } from "../lib/agent-actions";
import { resumeCommand, sessionCanRename, sessionDeletable, sessionMenuActions } from "../lib/format";
import { appArchiveDeps, archiveAgent, archiveOffers } from "../lib/agent-archives";
import { machineWithHub } from "../lib/mesh";
import { isPinned } from "../lib/pins";
import { isMuted } from "../lib/session-actions";
import { getState, markSeen, useStore } from "../lib/store";
import type { Session } from "../lib/types";
import { sessionNode } from "../lib/node-actions";
import { Button, copyText, Dialog, type MenuItem } from "../ui/atoms";
import type { MenuAt } from "../ui/menu-model";
import { NodeMenu } from "./NodeMenu";
import { preloadSessionDialogs } from "./chat/SessionDialogs";
import { t } from "../i18n";
import { markAllSeenLabel } from "../lib/state-labels";
import { lazy, Suspense, useEffect } from "react";
import type { SpaceMenuProps } from "./SpaceMenu";

export function agentRowMenuItems(s: Session, o: { pinned: boolean; muted: boolean; onRename: (s: Session) => void; onDelete: (s: Session) => void; features?: readonly string[] }): MenuItem[] {
  const { onRename, onDelete } = o;
  // "Live link" shows when the chat's ⋯ shows it (sessionMenuActions), and does the same (#3405); so do Archive and Close…
  const allowed = sessionMenuActions(s);
  const features = o.features ?? getState().features;
  return [
    ...(allowed.includes("attach") ? [{ key: "attach", label: t("chat-chatview.attach"), icon: "link" as const, onSelect: () => void import("../lib/attach-live").then((m) => m.attachLive(s)) }] : []),
    { key: "pin", label: o.pinned ? t("agentmenus.unpin") : t("agentmenus.pin"), icon: "pin", accel: "p", onSelect: () => void flipPin(s) },
    // as the chat's ⋯: by capabilities.rename where the node says so (a runtime's session, #6294)
    ...(sessionCanRename(s) ? [{ key: "rename", label: t("agentmenus.rename"), icon: "pencil" as const, accel: "r", onSelect: () => onRename(s) }] : []),
    { key: "mute", label: o.muted ? t("agentmenus.unmute") : t("agentmenus.mute"), icon: o.muted ? "bell" : "bellOff", onSelect: () => void flipMute(s) },
    { key: "persona", label: t("agentmenus.hand_to_persona"), icon: "spark", onSelect: () => void handToPersona(s, machineWithHub(s.machine_id, getState().machines)) },
    ...(s.cwd ? [{ key: "cwd", label: t("agentmenus.copy_folder"), icon: "folder" as const, accel: "c", onSelect: () => copyText(s.cwd) }] : []),
    ...(allowed.includes("resume") ? [{ key: "resume", label: t("agentmenus.copy_resume"), icon: "terminal" as const, onSelect: () => copyText(resumeCommand(s)) }] : []),
    ...archiveItems(s, features),
    ...(sessionDeletable(s) ? [{ key: "delete", label: t("agentmenus.delete_chat"), icon: "trash" as const, danger: true, onSelect: () => onDelete(s) }] : []),
  ];
}

/** Archive remains available for people who want to find the Agent in Archived later. */
export function archiveItems(s: Session, features: readonly string[]): MenuItem[] {
  const archive = archiveOffers(s, features).archive;
  return archive === "no" ? [] : [{ key: "archive", label: t("agentmenus.archive"), icon: "archive", divider: true, disabled: archive === "busy", detail: archive === "busy" ? t("agentmenus.archive_busy") : undefined, onSelect: () => void archiveAgent(s, false, appArchiveDeps) }];
}

/** The row's menu, then what any pane offers (split or close it, git, copy its ids): src/lib/node-actions.ts. The path
 *  and the rename are the row's own above. `at`: where a right click opened it (the pointer); without it, beside `anchor`. */
export function AgentRowMenu({ s, anchor, at, onClose, ...o }: { s: Session; anchor: HTMLElement; at?: MenuAt; onClose: () => void; onRename: (s: Session) => void; onDelete: (s: Session) => void }) {
  const pinned = useStore((st) => isPinned(s, st.pins));
  const muted = useStore((st) => isMuted(s, st.mutes));
  const features = useStore((st) => st.features);
  const online = useStore((st) => st.machines.find((m) => m.id === s.machine_id)?.online !== false);
  // its Rename… and Delete… open dialogs from a chunk of their own: on its way while the pointer moves to them (#10382)
  useEffect(preloadSessionDialogs, []);
  return <NodeMenu node={sessionNode(s, online)} at={at ?? { anchor }} onClose={onClose} before={agentRowMenuItems(s, { pinned, muted, features, ...o })} omit={["node-copy-path", "node-rename", "node-close-pane", "node-close-tab"]} />;
}

const LazySpaceMenu = lazy(() => import("./SpaceMenu").then((module) => ({ default: module.SpaceMenu })));
export function SpaceMenu(props: SpaceMenuProps) {
  useEffect(preloadSessionDialogs, []); // its Rename… opens a dialog from a chunk of its own (#10382)
  return <Suspense fallback={null}><LazySpaceMenu {...props} /></Suspense>;
}

/** "Mark all as seen", asked first: it changes what the other devices show too. */
export function MarkSeenDialog({ list, onClose, onDone }: { list: Session[] | null; onClose: () => void; onDone?: () => void }) {
  const n = list?.length ?? 0;
  return (
    <Dialog
      open={Boolean(list)}
      onClose={onClose}
      title={markAllSeenLabel()}
      width={380}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            tone="primary"
            icon="check"
            onClick={() => {
              if (list) markSeen(list);
              onDone?.();
              onClose();
            }}
          >
            {markAllSeenLabel(n)}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-ink-2">{t("agentmenus.mark_all_seen_body", { count: n })}</p>
    </Dialog>
  );
}
