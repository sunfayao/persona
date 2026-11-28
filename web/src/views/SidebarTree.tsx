// The Agents section's project (space) sections, the to-do tree rows the 常驻 directions reuse, and the rows they hold. Imports only
// SidebarParts and SidebarRows among the sidebar's files.
import { memo } from "react";
import { t } from "../i18n";
import { isQuiet, sessionHost, type Space } from "../lib/agent-groups";
import { baseName } from "../lib/format";
import { navigate } from "../lib/router";
import { openNewAgent } from "../lib/new-agent";
import { useStore } from "../lib/store";
import { type TreeAgent } from "../lib/todo-tree";
import { findTodoForSpace, openTodoFromTag } from "../lib/todos";
import type { Session } from "../lib/types";
import { Collapse, cx, sidebarFoldMotion } from "../ui/atoms";
import { StateTally } from "../ui/StatusMark";
import { foldedCounts, hasFoldedCounts } from "../lib/folded-counts";
import { Icon } from "../ui/icons";
import { type MenuNode } from "../lib/node-actions";
import type { MenuAt } from "../ui/menu-model";
import { useFolded } from "../lib/use-folded";
import { type GoFn, GroupCount, holdsRow, type RegFn, rowKey, type RowMenuFn, Section } from "./SidebarParts";
import { SessionRow } from "./SidebarRows";
import { SectionAdd } from "./SectionAdd";
import { WindowedRows } from "./WindowedRows";

type TreeRowProps = { machineLabel: MachineLabel; showHost?: boolean; /** No space · computer at a row's end (the sidebar's 常驻 rows, nested deep, #4352). */ noContext?: boolean; /** Before each agent row's glide key and its children's fold key: a second copy of a row (the sidebar's 常驻 tree) keeps its own, apart from the Agents list's. */ keyPrefix?: string; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onRowMenu?: RowMenuFn; onNavigate?: () => void };
/** A computer's name as an agent row shows it ("" with a single computer). */
type MachineLabel = (machineId: string) => string;



// The tree's rows are memoized, and a push hands them the nodes it did not change as the same objects (shareTree), so
// only the branches a push changed render again. The open row's key goes only to the branch that holds it ("" to the
// others, where holdsRow and `active` come out the same), so opening another agent re-renders two branches, not the
// whole tree; and the rows get the computers' names as a lookup that stays the same object while the names do, so a
// machines push (load, memory) re-renders none of them.

/** `activeKey` for an agent's branch (it and the agents it spawned), else "". */
const withinAgent = (a: TreeAgent, activeKey: string): string => (activeKey && (rowKey(a.session) === activeKey || a.children.some((c) => withinAgent(c, activeKey))) ? activeKey : "");






/** An agent and, folded under it, the agents it spawned for the same todo. */
const TreeAgentRows = memo(function TreeAgentRows({ a, depth, machineLabel, showHost, noContext, keyPrefix = "", reg, hover, activeKey, go, onRowMenu }: { a: TreeAgent; depth: number } & TreeRowProps) {
  const subtree = (n: TreeAgent): Session[] => n.children.flatMap((c) => [c.session, ...subtree(c)]);
  const [folded, toggle] = useFolded(keyPrefix + a.key + ":children", a.folded, { follow: true, holds: holdsRow(subtree(a), activeKey) });
  const s = a.session;
  const key = `${keyPrefix}s:${s.machine_id}/${s.session_id}`;
  const context = noContext ? undefined : [s.space_name || baseName(s.cwd), machineLabel(s.machine_id)].filter(Boolean).join(" · ");
  return (
    <div style={depth ? { paddingLeft: 12 } : undefined}>
      <div className="flex items-center">
        {a.children.length > 0 && (
          <button type="button" aria-expanded={!folded} aria-label={t("lib-todo-tree.spawned_agents", { count: a.children.length })} onClick={toggle} className="touch-hit relative z-10 -mr-1 flex size-4 shrink-0 items-center justify-center text-ink-3">
            <Icon name="chevron" size={9} stroke={2.2} className="opacity-65 transition-transform" style={{ ...sidebarFoldMotion(!folded), transform: folded ? "rotate(-90deg)" : "none" }} />
          </button>
        )}
        <div className="min-w-0 flex-1">
          <SessionRow s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={context} host={showHost && sessionHost(s) !== "herdr" && sessionHost(s) !== "botmux" ? sessionHost(s) : undefined} onMenu={onRowMenu} />
        </div>
      </div>
      {a.children.length > 0 && (
        <Collapse open={!folded} sidebarFold unclipped>
          <div className="flex flex-col gap-1 pl-2.5" inert={folded || undefined}>
            {a.children.map((c) => <SpawnedAgentRows key={c.key} a={c} depth={depth + 1} machineLabel={machineLabel} showHost={showHost} noContext={noContext} keyPrefix={keyPrefix} reg={reg} hover={hover} activeKey={withinAgent(c, activeKey)} go={go} onRowMenu={onRowMenu} />)}
          </div>
        </Collapse>
      )}
    </div>
  );
});
// the memo again, for the spawned agents' rows: inside the function above, its own name is the bare function
const SpawnedAgentRows = TreeAgentRows;

export function SpaceSection({ sp, machineName, reg, hover, activeKey, go, pinned, muted, onMenu, onRowMenu, onNavigate }: { sp: Space; machineName?: string; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; pinned: boolean; muted: boolean; onMenu: SpaceMenuFn; onRowMenu: RowMenuFn; onNavigate?: () => void }) {
  const mutes = useStore((st) => st.mutes);
  const online = useStore((st) => Boolean(st.machines.find((machine) => machine.id === sp.machineId)?.online));
  const place = sp.sessions.find((session) => session.workspace_cwd || session.cwd);
  const cwd = place?.workspace_cwd || place?.cwd;
  const allIdle = sp.sessions.every((s) => isQuiet(s, mutes));
  const [folded, toggle] = useFolded(`space:${sp.key}`, allIdle && sp.sessions.length > 3, { holds: holdsRow(sp.sessions, activeKey) });

  return (
    <Section
      tree
      folded={folded}
      onToggle={toggle}
      onMenu={(at, el) => onMenu(sp, el, at, { folded, toggle })}
      title={
        <span className="flex items-center gap-x-1.5 max-lg:flex-wrap">
          <Icon name="folder" size={11} stroke={1.8} className="shrink-0 text-ink-3/75" />
          <span className="truncate max-lg:whitespace-normal max-lg:wrap-anywhere normal-case tracking-normal text-[12px] font-medium text-ink-2 transition-colors group-hover/sec:text-ink">{sp.name}</span>
          <SpaceMarks pinned={pinned} muted={muted} />
          {machineName && <span className="truncate max-lg:basis-full normal-case tracking-normal text-[11px] font-normal text-ink-3">{machineName}</span>}
        </span>
      }
      right={
        <>
          <span className={cx("flex items-center gap-1 transition-transform duration-150", MENU_STEP_ASIDE, MENU_TOUCH_ROOM)}>
            <SpaceTodoTag sp={sp} onNavigate={onNavigate} />
            <span className={MENU_HIDES}><SessionTally sessions={sp.sessions} folded={folded} /></span>
            {online && place && cwd && <SectionAdd label={t("agents-groupedsessions.new_agent_in", { name: sp.name })}
              onClick={() => openNewAgent({ machineId: sp.machineId, cwd, spaceId: place.workspace_id ?? place.space_id })}
              className="rounded-[6px] hover:bg-hover-2 [@media(pointer:coarse)]:size-9" />}
          </span>
          <SpaceMenuButton sp={sp} onMenu={onMenu} />
        </>
      }
    >
      <WindowedRows items={sp.sessions} holds={(s) => rowKey(s) === activeKey} render={(s) => (
        <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} tab showTodoTag onMenu={onRowMenu} />
      )} />
    </Section>
  );
}

function SpaceMarks({ pinned, muted }: { pinned: boolean; muted: boolean }) {
  return (
    <>
      {pinned && <span title={t("sidebar.pinned")} className="shrink-0 text-accent"><Icon name="pin" size={10} stroke={2.2} /><span className="sr-only">{t("sidebar.pinned")}</span></span>}
      {muted && <span title={t("sidebar.muted")} className="shrink-0 text-ink-3"><Icon name="bellOff" size={10} stroke={2.2} /><span className="sr-only">{t("sidebar.muted")}</span></span>}
    </>
  );
}

// The todo a space hangs on, when the hub supports both: a tag that opens it.
function SpaceTodoTag({ sp, onNavigate }: { sp: Space; onNavigate?: () => void }) {
  const todos = useStore((st) => st.todos);
  const todosSupported = useStore((st) => st.todosSupported);
  const todosLoaded = useStore((st) => st.todosLoaded);
  const spacesSupported = useStore((st) => st.spacesSupported);
  const supportSpaces = todosLoaded && todosSupported !== false && spacesSupported !== false;
  const linkedTodo = supportSpaces ? findTodoForSpace(todos, sp.machineId, sp.name) : undefined;
  if (!linkedTodo) return null;
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        openTodoFromTag(linkedTodo.id, navigate, onNavigate);
      }}
      className="relative z-10 flex size-5 shrink-0 items-center justify-center rounded-[6px] text-ink-3 hover:bg-hover-2 hover:text-ink transition-colors cursor-pointer"
      title={t("sidebar.todo_tag", { title: linkedTodo.title })}
      aria-label={t("sidebar.todo_tag", { title: linkedTodo.title })}
    >
      <Icon name="todo" size={10} stroke={2} />
    </button>
  );
}

// A header row's ⋯ lies over the row's right end (absolute, so it reserves no width and every row's count keeps the
// same right edge, the machine row's, which has none). With a pointer it shows on hover or keyboard focus and the count
// under it steps aside (MENU_HIDES) with whatever sits beside the count (MENU_STEP_ASIDE); on a touch screen it is
// always there and the row keeps room for it (MENU_TOUCH_ROOM here, the 44px the tree's headers reserve).
// (Written out in full: Tailwind finds class names by scanning the source for them.)
const MENU_HIDES = "group-hover/sec:invisible group-has-[[data-row-menu]:focus-visible]/sec:invisible";
const MENU_STEP_ASIDE = "group-hover/sec:-translate-x-5 group-has-[[data-row-menu]:focus-visible]/sec:-translate-x-5";
const MENU_TOUCH_ROOM = "[@media(hover:none)]:mr-9";

// A heading's ⋯ for what the heading stands for (a space By project, a lone workspace By repo): on hover with a pointer,
// always on a touch screen.
function HeadingMenuButton({ label, title = label, onOpen }: { label: string; title?: string; onOpen: (anchor: HTMLElement) => void }) {
  return (
    <span className="absolute top-1/2 right-2 z-10 -translate-y-1/2">
      <button
        type="button"
        data-row-menu
        aria-label={label}
        aria-haspopup="menu"
        title={title}
        onClick={(e) => onOpen(e.currentTarget)}
        className="touch-hit relative flex size-5 items-center justify-center rounded-[6px] text-ink-3 opacity-0 transition-[opacity,background-color,color] duration-150 group-hover/sec:opacity-100 hover:bg-hover-2 hover:text-ink focus-visible:opacity-100 [@media(hover:none)]:size-8 [@media(hover:none)]:opacity-100"
      >
        <Icon name="more" size={12} />
      </button>
    </span>
  );
}

// A space's ⋯ (rename, pin, mute, mark seen, link a todo).
function SpaceMenuButton({ sp, onMenu }: { sp: Space; onMenu: (sp: Space, anchor: HTMLElement) => void }) {
  return <HeadingMenuButton label={t("sidebar.space_actions_named", { name: sp.name })} title={t("sidebar.space_actions")} onOpen={(el) => onMenu(sp, el)} />;
}

export type SpaceMenuFn = (sp: Space, anchor: HTMLElement, at?: MenuAt, fold?: MenuNode["fold"]) => void;
// Agents outside any space (Codex sessions and the like): folds like a space, folded at first when it is long and
// nothing in it wants a look. The app lists them as a space of their machine.
export function LooseSection({ sessions, reg, hover, activeKey, go, onRowMenu }: { sessions: Session[]; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onRowMenu: RowMenuFn }) {
  const mutes = useStore((st) => st.mutes);
  const [folded, toggle] = useFolded("loose", sessions.every((s) => isQuiet(s, mutes)) && sessions.length > 3, { holds: holdsRow(sessions, activeKey) });
  return (
    <Section title={t("sidebar.other_agents")} folded={folded} onToggle={toggle} right={<SessionTally sessions={sessions} folded={folded} />}>
      <WindowedRows items={sessions} holds={(s) => rowKey(s) === activeKey} render={(s) => (
        <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} onMenu={onRowMenu} />
      )} />
    </Section>
  );
}

// A group's count; folded, the states that want a look instead (a mark and a count each, needs you first, #3559), so a
// folded group doesn't hide them.
export function SessionTally({ sessions, folded }: { sessions: Session[]; folded: boolean }) {
  const counts = foldedCounts(sessions);
  if (folded && hasFoldedCounts(counts)) return <StateTally counts={counts} />;
  return <GroupCount n={sessions.length} label={t("sidebar.agent_count", { count: sessions.length })} />;
}
