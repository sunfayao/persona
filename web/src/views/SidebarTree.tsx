// The Agents section's project (space) sections, the to-do tree rows the 常驻 directions reuse, and the rows they hold. Imports only
// SidebarParts and SidebarRows among the sidebar's files.
import { memo, useCallback, useMemo, useRef } from "react";
import { t } from "../i18n";
import { isQuiet, sessionHost, type Space } from "../lib/agent-groups";
import { machineWithHub } from "../lib/mesh";
import { baseName } from "../lib/format";
import { href, navigate } from "../lib/router";
import { useStore } from "../lib/store";
import { treeStateLabel, type TreeAgent, type TreeState, type TreeTodo } from "../lib/todo-tree";
import { findTodoForSpace, openTodoFromTag } from "../lib/todos";
import type { Machine, Session } from "../lib/types";
import { Collapse, cx, sidebarFoldMotion } from "../ui/atoms";
import { StateTally } from "../ui/StatusMark";
import { foldedCounts, hasFoldedCounts } from "../lib/folded-counts";
import { Icon, type IconName } from "../ui/icons";
import { type MenuNode } from "../lib/node-actions";
import type { MenuAt } from "../ui/menu-model";
import { useFolded } from "../lib/use-folded";
import { sameValue } from "../lib/stable-lines";
import { type WaitReason } from "../lib/state-labels";
import { type GoFn, GroupCount, holdsRow, type RegFn, rowKey, type RowMenuFn, Section, SidebarGroup } from "./SidebarParts";
import { SessionRow } from "./SidebarRows";
import { WindowedRows } from "./WindowedRows";
import { DirectionStateText } from "./DirectionMarks";

export type TreeRowProps = { machineLabel: MachineLabel; showHost?: boolean; /** No space · computer at a row's end (the sidebar's 常驻 rows, nested deep, #4352). */ noContext?: boolean; /** Before each agent row's glide key and its children's fold key: a second copy of a row (the sidebar's 常驻 tree) keeps its own, apart from the Agents list's. */ keyPrefix?: string; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onRowMenu?: RowMenuFn; onNavigate?: () => void };
/** A computer's name as an agent row shows it ("" with a single computer). */
export type MachineLabel = (machineId: string) => string;

const TREE_TONE: Record<TreeState, string> = { needs_you: "text-attention", working: "text-orange", bg_waiting: "text-status-waiting", waiting: "text-ink-2", done: "text-ink-3" };

/** "Needs you" / "Working" / "Done" at the end of a tree node's line; `label`: a to-do's own word for it (the To-dos
 *  page's, #6458), in the color of the state it ranks as, but for a waiting to-do's (`wait`, #11090): Needs you's
 *  attention when it waits on the owner, the neutral one otherwise. */
const TreeStateText = memo(function TreeStateText({ state, label, wait }: { state: TreeState; label?: string; wait?: WaitReason }) {
  const tone = wait ? (wait === "owner" ? TREE_TONE.needs_you : TREE_TONE.waiting) : TREE_TONE[state];
  return <span className={cx("shrink-0 text-[11px] font-semibold whitespace-nowrap", tone)} data-tree-state={wait}>{label ?? treeStateLabel(state)}</span>;
});

// The tree's rows are memoized, and a push hands them the nodes it did not change as the same objects (shareTree), so
// only the branches a push changed render again. The open row's key goes only to the branch that holds it ("" to the
// others, where holdsRow and `active` come out the same), so opening another agent re-renders two branches, not the
// whole tree; and the rows get the computers' names as a lookup that stays the same object while the names do, so a
// machines push (load, memory) re-renders none of them.

/** `activeKey` for an agent's branch (it and the agents it spawned), else "". */
const withinAgent = (a: TreeAgent, activeKey: string): string => (activeKey && (rowKey(a.session) === activeKey || a.children.some((c) => withinAgent(c, activeKey))) ? activeKey : "");

/** Each computer's row label, one lookup while the labels are the same. */
export function useMachineLabel(machines: Machine[]): MachineLabel {
  const last = useRef<Map<string, string> | null>(null);
  const names = useMemo(() => {
    const next = machines.length > 1 ? new Map(machines.map((m) => [m.id, machineWithHub(m.id, machines)])) : null;
    return sameValue(last.current, next) ? last.current : next;
  }, [machines]);
  last.current = names;
  return useCallback((machineId: string) => (names ? names.get(machineId) ?? machineId : ""), [names]);
}

/** The chevron that folds a branch, turned as its group's (DESIGN_SYSTEM 3.4). */
function BranchChevron({ folded, size = 9 }: { folded: boolean; size?: number }) {
  return <Icon name="chevron" size={size} stroke={2.4} className="opacity-60 transition-transform" style={{ ...sidebarFoldMotion(!folded), transform: folded ? "rotate(-90deg)" : "none" }} />;
}

/**
 * One branch of "By to-do" (#6458): a to-do or a group of agents, as one kind of row whatever it is. The chevron that folds
 * it, its mark, its title, an ⓘ for a group, then its count and its state, each in the same place; `top`: one of the
 * list's sticky group headings (a grouping's top level, DESIGN_SYSTEM 3.4, which Collapse all reads), else a row inside a
 * section. Its body is `children`, or `empty` said in their place when there are none. The row selects no text: a
 * press-and-drag over the rows that fold groups painted the theme's grey selection behind each of their lines (#6458).
 * On a touch screen the row is 44 px tall and a tap anywhere on it folds it: the chevron's touch-hit square, its button
 * unpositioned there, takes the row as its box, under the positioned controls that keep their own taps (the to-do's link,
 * the ⓘ) and over the words, the count and the state.
 */
export function TreeBranch({ branch, top, folded, onToggle, toggleLabel, icon, title, info, end, empty, children }: { branch: string; top?: boolean; folded: boolean; /** None: nothing to unfold, the title stays in the column. */ onToggle?: () => void; toggleLabel: string; icon?: IconName; title: React.ReactNode; info?: React.ReactNode; end: React.ReactNode; empty?: string; children?: React.ReactNode }) {
  const row = (
    <div data-tree-branch-row className="relative z-10 flex w-full min-w-0 select-none items-center gap-1.5 rounded-control py-1 pl-2 pr-2 text-label text-ink-2 hover:bg-hover [@media(pointer:coarse)]:min-h-11">
      {onToggle ? (
        <button type="button" aria-expanded={!folded} aria-label={toggleLabel} onClick={onToggle} className="touch-hit flex shrink-0 items-center [@media(pointer:coarse)]:static">
          <BranchChevron folded={folded} />
        </button>
      ) : (
        <span aria-hidden className="invisible flex shrink-0 items-center"><Icon name="chevron" size={9} stroke={2.4} /></span>
      )}
      {icon && <Icon name={icon} size={11} stroke={2} className="shrink-0 text-ink-3" />}
      {title}
      {info}
      {end}
    </div>
  );
  const body = (
    <Collapse open={!folded} sidebarFold>
      <div data-tree-branch-list className="flex flex-col gap-px sidebar-indent" inert={folded || undefined}>
        {children ?? (empty ? <EmptyHint text={empty} /> : null)}
      </div>
    </Collapse>
  );
  return (
    <div data-tree-branch={branch} className="mt-px">
      {top ? <SidebarGroup heading={row}>{body}</SidebarGroup> : <>{row}{body}</>}
    </div>
  );
}

/** What an open group with nothing in it says: what it would hold (#6458). */
function EmptyHint({ text }: { text: string }) {
  return <p className="select-none px-2 py-1 text-label leading-[1.45] text-ink-3">{text}</p>;
}

/** One todo and its agents under its own folding line (`top`: at the list's top level, "By to-do"); a direction tree's
 *  task (#4352) says its task state. */
export const TreeTodoRows = memo(function TreeTodoRows({ td, top, ...p }: { td: TreeTodo; top?: boolean } & TreeRowProps) {
  const [folded, toggle] = useFolded(td.key, td.folded, { follow: true, holds: holdsRow(td.sessions, p.activeKey) });
  // a direction's task no agent is on yet (#4352): nothing to unfold, and no bare 0 beside its state
  const unfolds = td.agents.length > 0 || !td.taskState;
  // the link takes the touch row's height (touch-hit; the words truncate inside it, which would clip its square), so a
  // finger just off the words opens the to-do rather than folding it
  const title = td.todoId ? (
    <a href={href({ name: "todos", id: td.todoId })} onClick={(e) => { e.preventDefault(); openTodoFromTag(td.todoId!, navigate, p.onNavigate); }} className="touch-hit min-w-0 flex-1 font-medium text-ink hover:underline"><span className="block truncate">{td.name}</span></a>
  ) : (
    <button type="button" onClick={toggle} className="min-w-0 flex-1 truncate text-left">{td.name}</button>
  );
  const state = td.taskState ? (
    <DirectionStateText state={td.taskState} />
  ) : td.status === "usage_limit" ? (
    <span className="shrink-0 text-[11px] font-semibold whitespace-nowrap text-orange">{t("lib-usage-limit.short")}</span>
  ) : (
    <TreeStateText state={td.state} label={td.statusLabel} wait={td.wait} />
  );
  return (
    <TreeBranch
      branch={td.todoId ? `todo:${td.todoId}` : td.key}
      top={top}
      folded={folded}
      onToggle={unfolds ? toggle : undefined}
      toggleLabel={t("sidebar.task_agents", { name: td.name })}
      icon={td.todoId ? "todo" : undefined}
      title={title}
      end={<>{(td.sessions.length > 0 || !td.taskState) && <GroupCount n={td.sessions.length} label={t("sidebar.agent_count", { count: td.sessions.length })} className="shrink-0" />}{state}</>}
    >
      <WindowedRows items={td.agents} holds={(a) => withinAgent(a, p.activeKey) !== ""} render={(a) => <TreeAgentRows key={a.key} a={a} depth={0} {...p} activeKey={withinAgent(a, p.activeKey)} />} />
    </TreeBranch>
  );
});

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
            <Icon name="chevron" size={9} stroke={2.4} className="opacity-60 transition-transform" style={{ ...sidebarFoldMotion(!folded), transform: folded ? "rotate(-90deg)" : "none" }} />
          </button>
        )}
        <div className="min-w-0 flex-1">
          <SessionRow s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={context} host={showHost && sessionHost(s) !== "herdr" && sessionHost(s) !== "botmux" ? sessionHost(s) : undefined} onMenu={onRowMenu} />
        </div>
      </div>
      {a.children.length > 0 && (
        <Collapse open={!folded} sidebarFold>
          <div className="flex flex-col gap-px border-l border-line ml-2" inert={folded || undefined}>
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
  const allIdle = sp.sessions.every((s) => isQuiet(s, mutes));
  const [folded, toggle] = useFolded(`space:${sp.key}`, allIdle && sp.sessions.length > 3, { holds: holdsRow(sp.sessions, activeKey) });

  return (
    <Section
      folded={folded}
      onToggle={toggle}
      onMenu={(at, el) => onMenu(sp, el, at, { folded, toggle })}
      title={
        <span className="flex items-center gap-1.5">
          <span className="truncate normal-case tracking-normal text-[11.5px] text-ink-2">{sp.name}</span>
          <SpaceMarks pinned={pinned} muted={muted} />
          {machineName && <span className="truncate normal-case tracking-normal text-[11px] font-normal text-ink-3">{machineName}</span>}
        </span>
      }
      right={
        <>
          <span className={cx("flex items-center gap-1", MENU_STEP_ASIDE, MENU_TOUCH_ROOM)}>
            <SpaceTodoTag sp={sp} onNavigate={onNavigate} />
            <span className={MENU_HIDES}><SessionTally sessions={sp.sessions} folded={folded} /></span>
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

// The todo a space hangs on (docs/assistant/todos.md), when the hub supports both: a tag that opens it.
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
      className="relative z-10 flex shrink-0 items-center justify-center rounded bg-hover px-1 py-0.5 text-ink-3 hover:bg-hover-2 hover:text-ink transition-colors cursor-pointer"
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
        className="touch-hit relative flex size-5 items-center justify-center rounded-[5px] text-ink-3 opacity-0 transition-opacity duration-150 group-hover/sec:opacity-100 hover:bg-hover-2 hover:text-ink focus-visible:opacity-100 [@media(hover:none)]:size-8 [@media(hover:none)]:opacity-100"
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
