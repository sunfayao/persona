// The logical Agent space's menu, loaded when the user opens it.
import { flipSpaceMute, flipSpacePin } from "../lib/agent-actions";
import { isSpaceMuted, unreadOf, type SpaceLike } from "../lib/agent-filter";
import { isSpacePinned } from "../lib/pins";
import { getState, refresh, useStore } from "../lib/store";
import type { Session } from "../lib/types";
import type { MenuNode } from "../lib/node-actions";
import { navigate } from "../lib/router";
import { type MenuItem } from "../ui/atoms";
import type { MenuAt } from "../ui/menu-model";
import { NodeMenu } from "./NodeMenu";
import { t } from "../i18n";
import { markAllSeenLabel } from "../lib/state-labels";
import { refreshWorkspaceCatalog, useWorkspaceCatalog } from "../lib/workspace-catalog";
import { spaceWorktreeSources, worktreeActionKey } from "../lib/worktree-actions-context";
import type { WorkspaceTarget } from "../lib/workspace-target";

export function SpaceMenu({
  sp,
  anchor,
  at,
  fold,
  onClose,
  onRename,
  onMarkSeen,
  openPage = true,
}: {
  sp: SpaceLike;
  anchor: HTMLElement;
  /** Where a right click opened it (the pointer); without it, beside `anchor`. */
  at?: MenuAt;
  /** The space's fold, when it has one to toggle. */
  fold?: MenuNode["fold"];
  onClose: () => void;
  onRename: (s: Session) => void;
  onMarkSeen: (list: Session[]) => void;
  /** "Open space page" (/space/…) leads the menu; the space's own page leaves it out. */
  openPage?: boolean;
}) {
  const pins = useStore((st) => st.pins);
  const mutes = useStore((st) => st.mutes);
  const pinned = isSpacePinned(sp.machineId, sp.name, pins);
  const muted = isSpaceMuted(sp, mutes);
  const unread = unreadOf(sp.sessions);
  const first = sp.sessions[0];
  const machines = useStore((st) => st.machines);
  const online = machines.find((m) => m.id === sp.machineId)?.online !== false;
  const catalog = useWorkspaceCatalog(true);
  const sources = spaceWorktreeSources(sp, catalog, machines);
  const newWorktree = async (target: WorkspaceTarget) => {
    const { openChildWorktree } = await import("../lib/worktree-creation");
    const current = spaceWorktreeSources(sp, catalog, getState().machines);
    if (!current.some((source) => worktreeActionKey(source) === worktreeActionKey(target))) throw new Error(t("worktree.owner_changed"));
    openChildWorktree(target, { changed: () => { void refresh(); void refreshWorkspaceCatalog(); } });
  };


  const page = { name: "space" as const, mid: sp.machineId, space: sp.name };
  const items: MenuItem[] = [
    ...(openPage && first?.space_id ? [{ key: "open", label: t("agentmenus.open_space"), icon: "layers" as const, onSelect: () => navigate(page) }] : []),
    ...(unread.length ? [{ key: "seen", label: markAllSeenLabel(unread.length), icon: "check" as const, onSelect: () => onMarkSeen(unread) }] : []),
    ...(first?.space_id && !first.source_kind ? [{ key: "rename", label: t("agentmenus.rename_space"), icon: "pencil" as const, onSelect: () => onRename(first) }] : []),
    { key: "pin", label: pinned ? t("agentmenus.unpin_space") : t("agentmenus.pin_space"), icon: "pin", onSelect: () => void flipSpacePin(sp.machineId, sp.name) },
    { key: "mute", label: muted ? t("agentmenus.unmute_space") : t("agentmenus.mute_space"), icon: muted ? "bell" : "bellOff", onSelect: () => void flipSpaceMute(sp.machineId, sp.name) },
    ...(first?.cwd ? [{ key: "new-worktree", label: t("worktree-actions.new_child"), icon: "git" as const, divider: true,
      disabled: !sources.length, detail: sources.length ? undefined : t("agentmenus.worktree_source_unavailable"),
      onSelect: () => sources.length === 1 ? newWorktree(sources[0]) : undefined,
      ...(sources.length > 1 ? { children: sources.map((source) => ({ key: worktreeActionKey(source), label: source.label || source.cwd,
        detail: `${source.host} · ${source.cwd}`, onSelect: () => newWorktree(source) })) } : {}),
    }] : []),
  ];
  // A runtime's space (its folder) is no terminal host's workspace: no herdr ID to copy.
  const node: MenuNode = { kind: "workspace", mid: sp.machineId, online, host: first?.space_kind, label: sp.name,
    workspaceId: first?.source_kind ? undefined : first?.workspace_id ?? first?.space_id, paneId: first?.pane_id, cwd: first?.cwd, fold };
  return <NodeMenu node={node} at={at ?? { anchor }} onClose={onClose} header={t("agentmenus.space_header", { name: sp.name })} before={items} omit={["node-rename", "node-worktree"]} />;
}

export type SpaceMenuProps = Parameters<typeof SpaceMenu>[0];
