import { decisionSourceLabel } from "../lib/todo-decision-label";
import type { AgentLifecyclePage } from "../lib/types";
import { TodoRepositoryBindings } from "../ui/TaskRepositoryBindingsEditor";
import { hasRequestPieces } from "../lib/request-pieces";
import { defaultManager, MANAGER_HEADER } from "../lib/manager-choice";
// The To-dos page: the list on the left (status sections, folders or requests), the open to-do on the right. The main
// conversation and the agents write every to-do; the owner reads it here, and nothing on this page changes one.
import { lazy, memo, Suspense, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode, type RefObject } from "react";
import { ApiError, apiBlob, credentialOwner, errorText, getToken, subscribeCredentialOwner } from "../lib/api";
import { ago, backgroundWaiting, clock, shortPath, stateOf, tabLabel } from "../lib/format";
import { foldedState } from "../lib/child-sessions";
import { sessionCard } from "../lib/folded-rows";
import { TaskWaitInfo } from "./assistant/TaskWaitInfo";
import { navigate } from "../lib/router";
import { openFromPage } from "../lib/handoff";
import { ONE_PANE, useMedia } from "../lib/viewport";
import { interactionAsk } from "../lib/inbox";
import { usageLimitLabel } from "../lib/usage-limit";
import { plainMarkdown } from "../lib/plain-markdown";
import { useScrollMemory } from "../lib/scroll-memory";
import { useAgentSessions } from "../lib/sessions";
import { useScoped } from "../lib/current-assistant";
import { lazyPage } from "../lib/lazy-page";
import { refreshTodos, useStore } from "../lib/store";
import {
  DECISIONS_FIRST,
  type DecisionsView,
  fetchTodoDecisions,
  shownDecisions,
  isLegacySpacesRouteError,
  makeSessionLookup,
  mostUrgentAgent,
  orphanedLine,
  responsibleOf,
  sortTodos,
  heldDelivery,
  TODO_TONE_CLASS,
  isAutoDone,
  isCancelled,
  shownStatus,
  strikesThrough,
  todoStatus,
  type Todo,
  type TodoAgent,
  type TodoImage,
  type TodoDecision,
  type TodoStatus,
  type SessionLookup,
  taskNow,
} from "../lib/todos";
import { todoLook, waitOf, type WaitReason } from "../lib/todo-wait-reason";
import type { AgentState, Interaction, Machine } from "../lib/types";
import { Button, cx, IconButton, Loading, toast } from "../ui/atoms";
import { SearchField } from "../ui/SearchField";
import { StateTally, StatusMark } from "../ui/StatusMark";
import { type FoldedCounts } from "../lib/folded-counts";
import { Icon, type IconName } from "../ui/icons";
import { FoldAllButton } from "../ui/FoldAllButton";
import { SESSION_LIST_STYLE } from "../ui/list-metrics";
import { Markdown } from "../ui/Markdown";
import { formatDate, t } from "../i18n";
import { tRich } from "../i18n/rich";
import { dayLabel } from "../lib/feed";
import { entryWords, supervisionDays, supervisionLog, supervisionRole, supervisorOf, workerAgent, workerStep, workerStepLabel, type SupervisionDay, type SupervisionEntry, type SupervisionRole } from "../lib/todo-supervisor";
import { groupTodosByProject, treeStateLabel, type TodoProjectGroup, type TreeState } from "../lib/todo-tree";
import { cancellationReason, fetchAgentLifecycle, formatAgentSource, seenCanFile } from "../lib/todo-details";
import { TRIAGE_SECTIONS, checksAsOpen, countsAsOpen, lastActivity, openMergePRs, sectionStartsFolded, supersededFailedStarts, triage, triageTodos, type OwnerNeed, type TriageGroup, type TriageResult, type TriageRule, type TriageSection } from "../lib/todo-triage";
import { orderWaitGroups } from "../lib/task-wait";
import { viewDrawOn } from "../features/todos/model/view-gates";
import { drawnFromView, openAsDrawn } from "../features/todos/model/view-triage";
import { useRestoredTodos } from "../lib/todo-restore";
import { stepsWords } from "../lib/todo-steps";
import { useOwnerNeeds, useTodoLookup, useTodoMinute } from "../lib/todo-owner-needs";
import { autoReviewLine, checklist, latestReview, parkedLine, progressLine, type Supervision, type TaskReview } from "../lib/supervision";
import { briefCriteria, resultExtras } from "../lib/brief";
import { prLink } from "./TodoStageBar";
import { useFolded } from "../lib/use-folded";
import { stateName } from "../lib/state-labels";
import { Empty, Segmented, Tag } from "./assistant/parts";
import { OfflineState, useShownOffline } from "./ManagerParts";
import { LatestReport, ReviewBadge } from "./assistant/Supervision";
import { TodoStageBar } from "./TodoStageBar";
import { CriterionMark } from "./TodoCriterionMark";
import { TodoAskLine, TodoAskPanel } from "./TodoAsk";
import { todoUltracode } from "../lib/ultracode";
import { UltraMark } from "./chat/Ultracode";
import { boundThread, threadLine, todoThreadsOn } from "../lib/todo-thread";
import { setTodoSeen, todoSeenOn } from "../lib/todo-seen";
import { AttachmentTray } from "../ui/AttachmentTray";

/** The list by status sections (default, #3156), by folder (#2526) or as a graph of requests. */
const GROUPING_KEY = "persona.todos.grouping";

type TodoGrouping = "status" | "folder" | "request";

/** The grouping saved in this browser; status for none or an unknown one. */
function todoGrouping(saved: string | null): TodoGrouping {
  return saved === "folder" || saved === "request" ? saved : "status";
}

function pref(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function setPref(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // private mode, storage full: the choice lasts the page
  }
}

export function TodosView({
  selectedId,
  onToggleSidebar,
}: {
  selectedId?: string;
  onToggleSidebar?: () => void;
}) {
  const owner = useSyncExternalStore(subscribeCredentialOwner, credentialOwner, credentialOwner);
  // with several assistants, the current one's to-dos; a link to another assistant's
  // to-do still opens it, without switching
  const everyTodo = useStore((s) => s.todos);
  const ownTodos = useScoped(everyTodo);
  const todos = useMemo(() => {
    const linked = selectedId && !ownTodos.some((x) => x.id === selectedId) ? everyTodo.filter((x) => x.id === selectedId) : [];
    return linked.length ? [...ownTodos, ...linked] : ownTodos;
  }, [everyTodo, ownTodos, selectedId]);
  const todosLoaded = useStore((s) => s.todosLoaded);
  const todosError = useStore((s) => s.todosError);
  const todosSupported = useStore((s) => s.todosSupported);
  // the current assistant offline (several assistants): the hub's merged list leaves it out, so none here means it
  // couldn't answer; the page says so instead of the empty one
  const offline = useShownOffline();
  const machines = useStore((s) => s.machines);
  const interactions = useStore((s) => s.interactions);
  // its to-dos' Agents as the sidebar's badge reads them (lib/todo-owner-needs.ts): an open task's Agent outside the list's
  // window too, and a card answered elsewhere stops its agent's "Needs you" at once, not at the hub's next push (#4186)
  const lookup = useTodoLookup();
  // the to-dos the sidebar's badge counts, each with why (#11838): their rows carry its mark, from the badge's own hook
  const needs = useOwnerNeeds();

  const [activeId, setActiveId] = useState<string | undefined>(selectedId);
  const [search, setSearch] = useState("");
  const [retrying, setRetrying] = useState(false);
  const [showDone, setShowDone] = useState(false);
  const [showCancelled, setShowCancelled] = useState(false);
  const [showReleased, setShowReleased] = useState(false);

  // Sync selectedId from URL
  useEffect(() => {
    if (selectedId) setActiveId(selectedId);
  }, [selectedId]);
  // A link to a to-do the list doesn't have (deleted, or an old link): said once, and the address loses it, so another
  // to-do is not shown under it (web usability audit item 42). Not while the list can't tell: the current assistant
  // offline (the merged list leaves its to-dos out) or a hub without to-dos (an empty list); the link is kept for later.
  const linkGone = todosLoaded && !todosError && todosSupported && !offline && Boolean(selectedId) && !todos.some((td) => td.id === selectedId);
  useEffect(() => {
    if (!linkGone) return;
    toast(t("not-found.todo_gone"));
    navigate({ name: "todos" }, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per link that is gone
  }, [linkGone]);

  // a minute's tick moves to-dos across the 24 h and 48 h lines (and today's end) while nothing else changes: the
  // badge's clock, so the sections and the badge's marks move together
  const tick = useTodoMinute();
  const [restored, restoreTodo] = useRestoredTodos();
  // whether a failed start was superseded by finished work (#3713) is decided on every to-do: a search that leaves the
  // finished one out must not put it back in Needs you
  const superseded = useMemo(() => supersededFailedStarts(todos), [todos]);
  // Each to-do's own status section, whatever the list is grouped by: its status mark and its page read a failed start
  // waiting on the owner as open (audit item 13).
  const sectionOf = useMemo(
    () => new Map(todos.map((td) => [td.id, triage(td, lookup, Date.now(), restored.get(td.id) ?? 0, superseded.has(td.id)).section])),
    [todos, lookup, restored, superseded, tick],
  );
  // A sessions push builds a new lookup: what it leaves in the same order stays the same list (and the same groups,
  // the same row views below), so the rows and sections it did not touch skip their render.
  const views = useSteady(useMemo(() => todoRowViews(todos, lookup, interactions, sectionOf, needs), [todos, lookup, interactions, sectionOf, needs]), steadyViews);

  // Filter by search query
  const matchesSearch = (t: Todo) => {
    const q = search.trim().toLowerCase();
    if (!q) return true;
    return (
      t.title.toLowerCase().includes(q) ||
      (t.notes ?? "").toLowerCase().includes(q) ||
      (responsibleOf(t)?.name.toLowerCase().includes(q) ?? false)
    );
  };

  // Developing Persona's view switch: the sections from the hub's view, a row
  // without one placed as before
  const features = useStore((s) => s.features);
  const fromView = useMemo(() => viewDrawOn(features), [features]);
  // a search narrows the to-dos first; the sections (or the folders) place what is left
  const triaged = useMemo(
    () => {
      const r = orderWaitGroups(triageTodos(todos.filter(matchesSearch), lookup, Date.now(), restored, { superseded }), Date.now());
      return fromView ? drawnFromView(r, Date.now()) : r;
    },
    [todos, lookup, restored, search, superseded, tick, fromView],
  );
  // the header's counts: every to-do, whatever is searched
  const triageAll = useMemo(
    () => {
      if (!search.trim()) return triaged;
      const r = orderWaitGroups(triageTodos(todos, lookup, Date.now(), restored, { superseded }), Date.now());
      return fromView ? drawnFromView(r, Date.now()) : r;
    },
    [todos, lookup, restored, search, superseded, triaged, tick, fromView],
  );
  // Where the status sections place each to-do the search leaves: its row's section (a merged row takes its most
  // urgent member's), and whether it counts as open (countsAsOpen, the header's Not done, its own reading). Folders
  // and directions share this placement and one number for it; cancelled stays out of both open and Done (#5732).
  const placed = useMemo(() => {
    const row = new Map<string, TriageSection>();
    const open = new Set<string>();
    for (const sec of TRIAGE_SECTIONS)
      for (const g of triaged.sections[sec])
        for (const m of g.members) {
          row.set(m.id, sec);
          if ((fromView ? openAsDrawn : countsAsOpen)(m, g.triage.get(m.id)!)) open.add(m.id);
        }
    return { row, open };
  }, [triaged, fromView]);
  const filteredUncompleted = useSteady(
    useMemo(() => sortTodos(todos.filter((td) => {
      const sec = placed.row.get(td.id);
      return placed.row.has(td.id) && sec !== "done" && sec !== "cancelled";
    }), lookup), [todos, placed, lookup]),
    sameList,
  );
  const filteredCompleted = useSteady(
    useMemo(() => sortTodos(todos.filter((td) => placed.row.get(td.id) === "done"), lookup), [todos, placed, lookup]),
    sameList,
  );
  const filteredCancelled = useSteady(
    useMemo(() => sortTodos(todos.filter((td) => placed.row.get(td.id) === "cancelled"), lookup), [todos, placed, lookup]),
    sameList,
  );
  // open todos under the project they were put in, else the one their agents work in (#2526)
  const projectGroups = useSteady(
    // a folder holding a to-do in Needs you says so, a failed start waiting on the owner too (audit item 13)
    useMemo(() => groupTodosByProject(filteredUncompleted, lookup, Date.now(), (td) => sectionOf.get(td.id) === "waiting"), [filteredUncompleted, lookup, sectionOf]),
    steadyGroups,
  );
  // each section gets only its own rows' views, so one row's change re-renders that row's section alone
  const sectionViews = useSteady(
    useMemo((): ReadonlyMap<string, TodoRowView[]> => new Map(projectGroups.map((g) => [g.key, g.todos.map((td) => views.get(td.id)!)])), [projectGroups, views]),
    steadyLists,
  );

  // The list by status sections (#3156), or by folder as before
  const [chosenGrouping, setGrouping] = useState<TodoGrouping>(() => todoGrouping(pref(GROUPING_KEY)));
  // "By request" is offered while some request is in pieces; else the list goes by status and the choice is kept.
  // Until the to-dos are read a saved choice stays.
  const byRequest = useMemo(() => hasRequestPieces(todos), [todos]);
  const requestGone = chosenGrouping === "request" && todosLoaded && !byRequest;
  const grouping: TodoGrouping = requestGone ? "status" : chosenGrouping;
  const byStatus = grouping === "status";
  // "By request": the requests graph takes the page's width, and the detail opens beside it only once a piece is picked
  const requestMode = grouping === "request";
  const onRestore = useCallback(
    (id: string) => {
      restoreTodo(id);
      toast(t("todos.restored"), "ok");
    },
    [restoreTodo],
  );
  // Got it on a Just finished row (#5707): filed under Done at once, the hub
  // keeps the mark for every device, and the toast's Undo takes it off. Only while the hub keeps seen marks.
  const seenOn = useStore((s) => todoSeenOn(s.features));
  // Opening a Just finished to-do is seeing it too: it is filed under Done once the page leaves it (another to-do, the
  // list, another page), so its row does not move while it is read. Nothing is said: the user did not ask for it.
  const seenOnLeave = useRef<string | undefined>(undefined);
  // to-dos the user pressed Got it on here: their own choice (and its Undo) stands, leaving one marks nothing
  const decided = useRef(new Set<string>());
  // what the rows' callback reads when it runs, so it stays the same function and a push re-renders no row
  const seenInputs = useRef({ todos, lookup, restored, superseded });
  seenInputs.current = { todos, lookup, restored, superseded };
  const onSeen = useCallback((id: string) => {
    const failed = (e: unknown) => toast(t("todos.action_failed", { error: errorText(e) }), "error");
    // the mark shown before the hub answers is not earlier than the moment that put it in Just finished
    const { todos: list, lookup: look, restored: back, superseded: sup } = seenInputs.current;
    const td = list.find((x) => x.id === id);
    const after = td ? triage(td, look, Date.now(), back.get(id) ?? 0, sup.has(id)).last : 0;
    decided.current.add(id);
    if (seenOnLeave.current === id) seenOnLeave.current = undefined;
    setTodoSeen(id, true, after).catch(failed);
    toast(t("todos.seen_moved"), "ok", { label: t("lib-agents.undo"), run: () => void setTodoSeen(id, false).catch(failed) });
  }, []);
  const opened = seenOn && selectedId !== undefined ? todos.find((td) => td.id === selectedId) : undefined;
  const openedJustDone = opened !== undefined && triage(opened, lookup, Date.now(), restored.get(opened.id) ?? 0, superseded.has(opened.id)).section === "just_done";
  useEffect(() => {
    if (openedJustDone && !decided.current.has(selectedId!)) seenOnLeave.current = selectedId;
  }, [openedJustDone, selectedId]);
  useEffect(() => {
    const id = selectedId;
    return () => {
      if (id === undefined || seenOnLeave.current !== id) return;
      seenOnLeave.current = undefined;
      void setTodoSeen(id, true).catch(() => {}); // it stays in Just finished, as if never opened
    };
  }, [selectedId]);

  // List and detail side by side (md and up, Tailwind's max-md: below it they take turns, as on a phone)
  const split = !useMedia(ONE_PANE);
  // Active selected todo
  const activeTodo = useMemo(() => {
    // taking turns, the open to-do is the address's: one left by Back stays closed, and a link opens its own at once
    const id = split ? activeId : selectedId;
    if (id) {
      const found = todos.find((t) => t.id === id);
      if (found) return found;
    }
    // taking turns, nothing is open until a row is: no first row stands in, marked current with its detail loading unseen
    if (requestMode || !split) return undefined;
    if (byStatus) {
      // the first row of an open section; none rather than one that would unfold a folded section (Shelved, Done)
      for (const sec of TRIAGE_SECTIONS) if (!sectionStartsFolded(sec, triaged.sections[sec], tick) && triaged.sections[sec].length) return triaged.sections[sec][0].lead;
      return undefined;
    }
    return filteredUncompleted[0] ?? filteredCompleted[0] ?? filteredCancelled[0];
  }, [activeId, selectedId, todos, filteredUncompleted, filteredCompleted, filteredCancelled, byStatus, requestMode, split, triaged]);

  // Active agents of current selected todo
  const activeAgents = useMemo(() => {
    return (activeTodo?.agents ?? []).filter((a) => !a.released_at);
  }, [activeTodo]);

  const releasedAgents = useMemo(() => {
    return (activeTodo?.agents ?? []).filter((a) => a.released_at);
  }, [activeTodo]);

  // Narrow (< md): the list and a todo's detail take turns, as on a phone (iOS pushes TodoDetailView). The URL's ?id=
  // says a todo was opened; back goes to the list.
  const detailOnNarrow = Boolean(selectedId && todos.some((t) => t.id === selectedId));
  // The id a row click in this page pushed (/todos → /todos?id=…). Back from that detail pops the entry, as iOS pops
  // its NavigationStack, so the browser's Back does not return to the detail just left; a deep link replaces instead.
  const pushedId = useRef<string | undefined>(undefined);
  const listColumn = useRef<HTMLDivElement>(null);
  const listBox = useRef<HTMLDivElement>(null);
  const detailColumn = useRef<HTMLDivElement>(null);
  // The to-do a row opened and where the list was scrolled then: on a phone's width the list is hidden under the
  // detail, and the focus on its row goes with it (audit item 74).
  const fromList = useRef<{ id: string; top: number } | undefined>(undefined);
  // stable across pushes, so the memoized rows keep the same props
  const openFromList = useCallback(
    (id: string) => {
      setActiveId(id);
      if (id === selectedId) return; // already this entry (a deep link stays a deep link)
      pushedId.current = id;
      fromList.current = { id, top: listBox.current?.scrollTop ?? 0 };
      navigate(`/todos?id=${encodeURIComponent(id)}`);
    },
    [selectedId],
  );
  const backToList = () => {
    if (selectedId && pushedId.current === selectedId) {
      pushedId.current = undefined;
      history.back();
    } else navigate("/todos", true);
  };
  const back = useRef(backToList);
  back.current = backToList;
  // On a phone's width a to-do opened from the list takes the focus to the detail's Back, as a page pushed on iOS
  // does; back on the list (Back, Esc, the browser's Back), the list is scrolled where it was and the focus is on that
  // to-do's row again. Side by side, the focus stays on the row and nothing moves.
  // A to-do shown over the hidden list, a deep link's too: back on the list, the focus goes to its row, not the page.
  const shownOverList = useRef<string | undefined>(undefined);
  useLayoutEffect(() => {
    const list = listColumn.current;
    if (!list) return;
    const from = fromList.current;
    if (detailOnNarrow) {
      const hidden = !list.getClientRects().length;
      shownOverList.current = hidden ? selectedId : undefined;
      if (!from || selectedId !== from.id) return;
      if (!hidden) fromList.current = undefined;
      else detailColumn.current?.querySelector<HTMLElement>("[data-testid=todos-back] button")?.focus();
      return;
    }
    const id = from?.id ?? shownOverList.current;
    fromList.current = undefined;
    shownOverList.current = undefined;
    if (from && listBox.current) listBox.current.scrollTop = from.top;
    if (id) [...list.querySelectorAll<HTMLElement>("[data-todo]")].find((el) => el.dataset.todo === id)?.focus();
  }, [detailOnNarrow, selectedId]);
  // Esc on a phone's width goes back to the list, as Back does; a dialog or a menu over the page keeps it.
  useEffect(() => {
    if (!detailOnNarrow) return;
    const onKey = (e: KeyboardEvent) => {
      const list = listColumn.current;
      if (e.key !== "Escape" || e.defaultPrevented || e.isComposing || !list || list.getClientRects().length) return;
      if (document.querySelector("[role=menu],[role=dialog]")) return;
      e.preventDefault();
      back.current();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [detailOnNarrow]);

  // "By request": the detail closes and the graph has the page again
  const closeDetail = () => {
    setActiveId(undefined);
    navigate("/todos", true);
  };

  const retryTodos = async () => {
    if (retrying) return;
    setRetrying(true);
    try {
      await refreshTodos();
    } finally {
      setRetrying(false);
    }
  };

  return (
    <div className="flex h-full flex-col bg-surface">
      {/* Top Header */}
      <header className="flex h-13 shrink-0 items-center justify-between border-b border-line bg-surface px-4 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          {onToggleSidebar && <IconButton icon="sidebar" label={t("agents-agentsview.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />}
          <div className="flex min-w-0 items-baseline gap-2">
            <h1 className="text-[16px] font-semibold whitespace-nowrap text-ink">{t("todos.title")}</h1>
            {/* the counts once the list has its to-dos (audit item 83): not while it loads, nor after an empty failed read */}
            {(todosLoaded || todos.length > 0) && !(todos.length === 0 && todosError) && (
              // a phone's header is too narrow for them beside the title: they stay on wider screens
              <span className="truncate text-[12px] whitespace-nowrap text-ink-3 max-sm:hidden" data-testid="todos-summary">
                {/* one count in every grouping (audit item 12): open as countsAsOpen counts it, section counts as their
                    titles do, with Cancelled always separate from Done (#11085, #5732) */}
                {t("todos.summary_triage", { open: triageAll.open, parked: sectionSize(triageAll, "parked"), shelved: sectionSize(triageAll, "shelved"), done: sectionSize(triageAll, "done"), cancelled: sectionSize(triageAll, "cancelled") })}
              </span>
            )}
          </div>
        </div>
      </header>

      {/* Main Split Layout: Left List & Right Detail */}
      <div className="flex min-h-0 flex-1">
        {/* Left Column: Todo List */}
        <div
          ref={listColumn}
          data-testid="todos-list-column"
          data-narrow-hidden={detailOnNarrow || undefined}
          // the edge parts the list from the detail beside it; on a phone the list is the whole page and has none
          className={cx("flex w-full flex-col border-line bg-page", requestMode ? "min-w-0 md:flex-1" : "shrink-0 md:w-[380px] lg:w-[400px]", (!requestMode || activeTodo) && "md:border-r", detailOnNarrow && "max-md:hidden")}
        >
          {/* Search Bar */}
          <div className="flex items-center gap-2 p-3">
            {/* the graph shows every request: a search has nothing to narrow there */}
            {!requestMode && (
              <div className="min-w-0 flex-1">
                <SearchField tone="inset" value={search} onChange={setSearch} placeholder={t("todos.search_placeholder")} />
              </div>
            )}
            <Segmented
              label={t("todos.grouping_label")}
              value={grouping}
              options={[
                { v: "status", label: t("todos.grouping_status") },
                { v: "folder", label: t("todos.grouping_folder") },
                ...(byRequest || chosenGrouping === "request" && !todosLoaded ? [{ v: "request" as const, label: t("todos.grouping_request") }] : []),
              ]}
              onChange={(next) => {
                setPref(GROUPING_KEY, next);
                setGrouping(next);
              }}
              compact
            />
          </div>

          {/* List Content */}
          {requestMode ? (
            <Suspense fallback={<div className="flex justify-center py-16"><Loading /></div>}>
              <TodoRequestGraph todos={todos} lookup={lookup} machines={machines} selectedId={activeTodo?.id} onOpenTodo={openFromList} />
            </Suspense>
          ) : (
          <TodoListScroller box={listBox}>
            {!todosLoaded && todos.length === 0 ? (
              <div className="flex justify-center py-16" data-testid="todos-loading">
                <Loading />
              </div>
            ) : todos.length === 0 && todosError ? (
              <Empty
                icon="todo"
                title={t("todos.load_failed")}
                action={
                  <Button tone="secondary" icon="retry" busy={retrying} onClick={() => void retryTodos()}>
                    {t("todos.retry")}
                  </Button>
                }
              >
                {todosError}
              </Empty>
            ) : filteredUncompleted.length === 0 && filteredCompleted.length === 0 && filteredCancelled.length === 0 ? (
              offline && todos.length === 0 ? (
                <OfflineState offline={offline} ring="ring-surface" className="py-14" />
              ) : search.trim() ? (
                <Empty icon="search" title={t("todos.no_match_title")}>
                  {t("todos.no_match_body")}
                </Empty>
              ) : (
                <Empty icon="todo" title={t("todos.empty_title")}>
                  {t("todos.empty_body")}
                </Empty>
              )
            ) : (
              byStatus ? (
              <div className="flex flex-col gap-1" data-testid="todos-sections" data-todo-source={fromView ? "view" : undefined}>
                {TRIAGE_SECTIONS.map((sec) =>
                  triaged.sections[sec].length === 0 ? null : (
                    <TodoTriageSection
                      key={sec}
                      section={sec}
                      // Fresh accepted, ticked and cancelled outcomes stay visible; older history keeps the folded default.
                      folded={sectionStartsFolded(sec, triaged.sections[sec], tick)}
                      groups={triaged.sections[sec]}
                      count={sectionSize(triaged, sec)}
                      views={views}
                      selectedId={activeTodo?.id}
                      onSelect={openFromList}
                      onRestore={onRestore}
                      onSeen={seenOn ? onSeen : undefined}
                    />
                  ),
                )}
              </div>
              ) : (
              <div className="flex flex-col gap-1">
                {projectGroups.map((g) => (
                  <TodoProjectSection
                    key={g.key}
                    group={g}
                    views={sectionViews.get(g.key)!}
                    selectedId={activeTodo?.id}
                    onSelect={openFromList}
                    onSeen={seenOn ? onSeen : undefined}
                  />
                ))}

                {/* Completed Fold */}
                {filteredCompleted.length > 0 && (
                  <div className="mt-2 border-t border-line/60 pt-2">
                    <button
                      type="button"
                      onClick={() => setShowDone((v) => !v)}
                      className="flex h-8 w-full items-center justify-between rounded-control px-2.5 text-[12px] text-ink-3 transition-colors hover:bg-hover hover:text-ink-2"
                    >
                      <span>{t("todos.done_section", { count: filteredCompleted.length })}</span>
                      <Icon
                        name="chevron"
                        size={12}
                        className={cx("transition-transform duration-200", showDone ? "rotate-180" : "")}
                      />
                    </button>
                    {showDone && (
                      <div className="flex flex-col gap-1 pt-1">
                        {filteredCompleted.map((todo) => (
                          <TodoListItem
                            key={todo.id}
                            todo={todo}
                            view={views.get(todo.id)!}
                            selected={activeTodo?.id === todo.id}
                            onSelect={openFromList}
                          />
                        ))}
                      </div>
                    )}
                  </div>
                )}
                {/* Cancelled Fold: its own outcome and reason, never inside Completed (#5732) */}
                {filteredCancelled.length > 0 && (
                  <div className="mt-2 border-t border-line/60 pt-2" data-testid="todos-cancelled-fold">
                    <button
                      type="button"
                      onClick={() => setShowCancelled((v) => !v)}
                      className="flex h-8 w-full items-center justify-between rounded-control px-2.5 text-xs text-ink-3 transition-colors hover:bg-hover hover:text-ink-2"
                    >
                      <span>{t("todos.cancelled_section", { count: filteredCancelled.length })}</span>
                      <Icon name="chevron" size={12} className={cx("transition-transform duration-200", showCancelled ? "rotate-180" : "")} />
                    </button>
                    {showCancelled && (
                      <div className="flex flex-col gap-1 pt-1">
                        {filteredCancelled.map((todo) => (
                          <TodoListItem key={todo.id} todo={todo} view={views.get(todo.id)!} selected={activeTodo?.id === todo.id} onSelect={openFromList} />
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
              )
            )}
          </TodoListScroller>
          )}
        </div>

        {/* Right Column: Todo Detail */}
        <div
          ref={detailColumn}
          data-testid="todos-detail-column"
          data-narrow-shown={detailOnNarrow || undefined}
          className={cx("min-h-0 flex-1 flex-col overflow-y-auto bg-surface", requestMode && !activeTodo ? "md:hidden" : "md:flex", requestMode && "md:w-[440px] md:flex-none lg:w-[520px]", detailOnNarrow ? "flex" : "hidden")}
        >
          {requestMode && (
            <div className="hidden shrink-0 justify-end px-2 pt-2 md:flex">
              <IconButton icon="x" label={t("todos.calendar_close")} onClick={closeDetail} />
            </div>
          )}
          {detailOnNarrow && (
            <div className="flex shrink-0 items-center px-2 pt-2 md:hidden" data-testid="todos-back">
              <Button tone="quiet" icon="chevronLeft" onClick={backToList}>
                {t("todos.back_to_list")}
              </Button>
            </div>
          )}
          {/* taking turns, the detail is there only while shown: a to-do opened before and left keeps no hidden page */}
          {activeTodo && (split || detailOnNarrow) ? (
            <TodoDetail
              key={`${owner}/${activeTodo.id}`}
              owner={owner}
              todo={activeTodo}
              activeAgents={activeAgents}
              releasedAgents={releasedAgents}
              lookup={lookup}
              machines={machines}
              interactions={interactions}
              showReleased={showReleased}
              onToggleReleased={() => setShowReleased((v) => !v)}
              open={checksAsOpen(activeTodo, sectionOf.get(activeTodo.id))}
              onOpenTodo={openFromList}
              todos={todos}
            />
          ) : (
            <div className="flex flex-1 items-center justify-center text-[13px] text-ink-3">
              {t("todos.pick_hint")}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/** The to-dos a status section's rows list, a merged row's related ones too: the number its title shows (#3712). */
function sectionSize(r: TriageResult, sec: TriageSection): number {
  return r.sections[sec].reduce((n, g) => n + g.members.length, 0);
}

/** The list's scroller: where it was scrolled comes back on a return to the page (Back from another page, audit item
 *  88), as on Overview (lib/scroll-memory.ts). */
function TodoListScroller({ box, children }: { box: RefObject<HTMLDivElement | null>; children: ReactNode }) {
  useScrollMemory(box, "/todos");
  return (
    <div ref={box} data-testid="todos-list-scroller" className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
      {children}
    </div>
  );
}

// ── Steady values ────────────────────────────────────────────

/** The last value while `steady` says the new one is the same, so memoized children see the same props. */
function useSteady<T>(value: T, steady: (prev: T, next: T) => T): T {
  const last = useRef(value);
  if (last.current !== value) last.current = steady(last.current, value);
  return last.current;
}

/** The same items in the same order. */
function sameItems<T>(a: readonly T[], b: readonly T[]): boolean {
  return a.length === b.length && a.every((x, i) => Object.is(x, b[i]));
}

const sameList = <T,>(prev: T[], next: T[]): T[] => (sameItems(prev, next) ? prev : next);

/** Two objects with the same fields, each the same value or object. */
function sameFields(a: object, b: object): boolean {
  const ka = Object.keys(a), kb = Object.keys(b);
  return ka.length === kb.length && ka.every((k) => Object.is((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]));
}

/** Each group that has the same title, state and todos stays the last one; so does the list when they all do. */
function steadyGroups(prev: TodoProjectGroup[], next: TodoProjectGroup[]): TodoProjectGroup[] {
  const byKey = new Map(prev.map((g) => [g.key, g]));
  const out = next.map((g) => {
    const old = byKey.get(g.key);
    return old && old.name === g.name && old.state === g.state && old.folded === g.folded && sameItems(old.todos, g.todos) ? old : g;
  });
  return sameItems(prev, out) ? prev : out;
}

/** Each list that has the same items stays the last one; so does the map when they all do. */
function steadyLists<T>(prev: ReadonlyMap<string, T[]>, next: ReadonlyMap<string, T[]>): ReadonlyMap<string, T[]> {
  let same = prev.size === next.size;
  const out = new Map<string, T[]>();
  for (const [k, list] of next) {
    const old = prev.get(k);
    const kept = old && sameItems(old, list) ? old : list;
    if (kept !== old) same = false;
    out.set(k, kept);
  }
  return same ? prev : out;
}

/** Each todo's view that has the same fields stays the last one; so does the map when they all do. */
function steadyViews(prev: ReadonlyMap<string, TodoRowView>, next: ReadonlyMap<string, TodoRowView>): ReadonlyMap<string, TodoRowView> {
  let same = prev.size === next.size;
  const out = new Map<string, TodoRowView>();
  for (const [id, v] of next) {
    const old = prev.get(id);
    const kept = old && sameFields(old, v) ? old : v;
    if (kept !== old) same = false;
    out.set(id, kept);
  }
  return same ? prev : out;
}

// ── Project Section ──────────────────────────────────────────

const TREE_TONE: Record<TreeState, string> = { needs_you: "text-attention", working: "text-orange", bg_waiting: "text-status-waiting", waiting: "text-ink-2", done: "text-ink-3" };

/** One project's open todos under a folding title with its Needs you / Working / Done (#2526). */
const TodoProjectSection = memo(function TodoProjectSection({
  group,
  views,
  selectedId,
  onSelect,
  onSeen,
}: {
  group: TodoProjectGroup;
  /** Its todos' row views, in the same order as group.todos. */
  views: readonly TodoRowView[];
  selectedId: string | undefined;
  onSelect: (id: string) => void;
  /** Got it on a row in Just finished (one closed in the last day stays in its folder until it is seen). */
  onSeen?: (id: string) => void;
}) {
  const holds = group.todos.some((td) => td.id === selectedId);
  const [folded, toggle] = useFolded(group.key, group.folded, { follow: true, holds });
  return (
    <div className="mt-1 first:mt-0" data-testid="todo-group" data-group={group.key}>
      <button type="button" aria-expanded={!folded} onClick={toggle} className="flex h-7 w-full min-w-0 items-center gap-1.5 rounded-control px-2 text-left text-[12px] text-ink-2 hover:bg-hover">
        <Icon name="chevron" size={10} stroke={2.4} className="shrink-0 opacity-60 transition-transform" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        <Icon name="folder" size={12} stroke={2} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate font-semibold">{group.name}</span>
        {folded && <OwnerMarkCount count={views.filter((v) => v.owner).length} />}
        <span className="shrink-0 text-[11px] text-ink-3 tabular-nums">{group.todos.length}</span>
        {!(group.area && group.state === "done") && (
          <span className={cx("shrink-0 text-[11px] font-semibold whitespace-nowrap", TREE_TONE[group.state])}>{treeStateLabel(group.state)}</span>
        )}
      </button>
      {!folded && (
        <div className="flex flex-col gap-1 pl-2 pt-0.5">
          {group.todos.map((todo, i) => (
            <TodoListItem
              key={todo.id}
              todo={todo}
              view={views[i]}
              selected={selectedId === todo.id}
              onSelect={onSelect}
              onSeen={onSeen}
            />
          ))}
        </div>
      )}
    </div>
  );
});

// ── Status sections (#3156) ──────────────────────────────────

const SECTION_TONE: Record<TriageSection, string> = {
  waiting: "text-attention",
  working: "text-orange",
  parked: "text-ink-2",
  just_done: "text-green",
  other: "text-ink-2",
  shelved: "text-ink-3",
  done: "text-ink-3",
  cancelled: "text-ink-3",
};

/** A section's title: agent states share the state names; only a section made entirely of unaccepted deliveries uses
 *  the awaiting-review wording. */
function sectionTitle(section: TriageSection, rules: readonly TriageRule[]): string {
  if (section === "waiting") return stateName("needs_you");
  if (section === "working") return stateName("working");
  if (section === "just_done" && rules.length > 0 && rules.every((rule) => rule === "finished_unseen")) {
    return t("todos.section_awaiting_review");
  }
  if (section === "just_done") return stateName("just_finished");
  return t(`todos.section_${section}`);
}

/** One status section: a folding title with its count, a row per group of related
 *  to-dos. Every section starts open but Shelved and Done (#5707). */
const TodoTriageSection = memo(function TodoTriageSection({
  section,
  folded: foldedFirst,
  groups,
  count,
  views,
  selectedId,
  onSelect,
  onRestore,
  onSeen,
}: {
  section: TriageSection;
  /** Whether it starts folded: Shelved and Done. */
  folded: boolean;
  groups: TriageGroup[];
  /** To-dos its rows list (a merged row holds several): the number its title shows (#3712, the same on iOS). */
  count: number;
  views: ReadonlyMap<string, TodoRowView>;
  selectedId: string | undefined;
  onSelect: (id: string) => void;
  onRestore: (id: string) => void;
  /** Got it on a Just finished row; unset while the hub keeps no seen marks. */
  onSeen?: (id: string) => void;
}) {
  const holds = groups.some((g) => g.members.some((m) => m.id === selectedId));
  const [folded, toggle] = useFolded(`todos-section:${section}`, foldedFirst, { follow: true, holds });
  const title = sectionTitle(section, groups.flatMap((g) => [...g.triage.values()].map((t) => t.rule)));
  return (
    <section className="mt-1 first:mt-0" data-testid="todo-section" data-section={section} data-count={groups.length} data-todos={count}>
      <button type="button" aria-expanded={!folded} onClick={toggle} className="flex h-8 w-full min-w-0 items-center gap-1.5 rounded-control px-2 text-left text-[12px] hover:bg-hover">
        <Icon name="chevron" size={10} stroke={2.4} className="shrink-0 text-ink-3 opacity-60 transition-transform" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        <span className={cx("min-w-0 flex-1 truncate font-semibold whitespace-nowrap", SECTION_TONE[section])}>{title}</span>
        {folded && <OwnerMarkCount count={ownerCount(groups.flatMap((g) => g.members), views)} />}
        <span className="shrink-0 text-[11px] text-ink-3 tabular-nums">{count}</span>
      </button>
      {!folded && section === "shelved" && <p className="px-2.5 pb-1 text-[11.5px] text-ink-3">{t("todos.shelved_hint")}</p>}
      {!folded && (
        <div className="flex flex-col gap-1 pt-0.5">
          {groups.map((g) => (
            <TodoTriageRow key={g.key} group={g} views={views} selectedId={selectedId} onSelect={onSelect} onRestore={onRestore} onSeen={onSeen} />
          ))}
        </div>
      )}
    </section>
  );
});

/** Keeps the pinned display order of still-present members while appending newly joined members at the end. */
function mergePinnedOrder(pinned: readonly string[], members: readonly Todo[]): string[] {
  const present = new Set(members.map((m) => m.id));
  const out = pinned.filter((id) => present.has(id));
  const seen = new Set(out);
  for (const m of members) if (!seen.has(m.id)) out.push(m.id);
  return out;
}

/** A section's row: the group's lead, and its related to-dos under "N related" (same PR, issue, title or plan). */
function TodoTriageRow({
  group,
  views,
  selectedId,
  onSelect,
  onRestore,
  onSeen,
}: {
  group: TriageGroup;
  views: ReadonlyMap<string, TodoRowView>;
  selectedId: string | undefined;
  onSelect: (id: string) => void;
  onRestore: (id: string) => void;
  onSeen?: (id: string) => void;
}) {
  // When selection lands on a non-lead member (deep link, click, or a live tick demoting the selected lead),
  // latch the row open so it stays expanded even if that member later becomes lead again (#12417).
  const selectedOtherId = group.members.slice(1).some((m) => m.id === selectedId) ? selectedId : undefined;
  const [opened, setOpened] = useState(selectedOtherId !== undefined);
  const [seenSelectedOtherId, setSeenSelectedOtherId] = useState(selectedOtherId);
  // While expanded, pin member positions so live activity ticks that swap `group.lead` never jump rows under the cursor.
  const [pinnedOrder, setPinnedOrder] = useState<string[] | null>(() => (selectedOtherId !== undefined ? group.members.map((m) => m.id) : null));

  if (selectedOtherId !== seenSelectedOtherId) {
    setSeenSelectedOtherId(selectedOtherId);
    if (selectedOtherId !== undefined && !opened) {
      setOpened(true);
      setPinnedOrder(group.members.map((m) => m.id));
    }
  }

  if (opened && pinnedOrder) {
    const nextOrder = mergePinnedOrder(pinnedOrder, group.members);
    if (nextOrder.length !== pinnedOrder.length || nextOrder.some((id, i) => id !== pinnedOrder[i])) {
      setPinnedOrder(nextOrder);
    }
  }

  const displayMembers = useMemo(() => {
    if (!opened || !pinnedOrder) return group.members;
    const byId = new Map(group.members.map((m) => [m.id, m]));
    const ordered: Todo[] = [];
    for (const id of pinnedOrder) {
      const m = byId.get(id);
      if (m) {
        ordered.push(m);
        byId.delete(id);
      }
    }
    for (const m of byId.values()) ordered.push(m);
    return ordered;
  }, [opened, pinnedOrder, group.members]);

  const lead = displayMembers[0] ?? group.lead;
  const others = displayMembers.slice(1);
  const toggleOpen = () => {
    if (opened) {
      setOpened(false);
      setPinnedOrder(null);
    } else {
      setOpened(true);
      setPinnedOrder(group.members.map((m) => m.id));
    }
  };
  const item = (td: Todo) => (
    <TodoListItem
      key={td.id}
      todo={td}
      view={views.get(td.id)!}
      selected={selectedId === td.id}
      onSelect={onSelect}
      section={group.triage.get(td.id)?.section}
      onRestore={onRestore}
      onSeen={onSeen}
    />
  );
  return (
    <div data-testid="todo-triage-row" data-id={lead.id} data-group-key={group.key} data-members={displayMembers.length}>
      {item(lead)}
      {others.length > 0 && (
        <button
          type="button"
          aria-expanded={opened}
          onClick={toggleOpen}
          className="ml-9 flex h-7 items-center gap-1 rounded-control px-1.5 text-[11.5px] whitespace-nowrap text-ink-3 hover:bg-hover hover:text-ink-2"
        >
          <Icon name="chevron" size={10} stroke={2.4} className="transition-transform" style={{ transform: opened ? "none" : "rotate(-90deg)" }} />
          {t("todos.related", { count: others.length })}
          {!opened && <OwnerMarkCount count={ownerCount(others, views)} />}
        </button>
      )}
      {opened && others.length > 0 && <div className="ml-6 flex flex-col gap-1 border-l border-line pl-2">{others.map(item)}</div>}
    </div>
  );
}

// ── The badge's mark (#11838) ────────────────────────────────

/** On a row the sidebar's To-dos badge counts (lib/todo-owner-needs.ts): the badge's look (its accent tint,
 *  SidebarParts BADGE_CLASS.accent) and its words, why it counts in the tooltip and said after them. */
function OwnerMark({ need }: { need: OwnerNeed }) {
  const why = t(`todos.owner_why_${need}`);
  return (
    <span data-testid="todo-owner-mark" data-need={need} title={t("todos.owner_mark_title", { why })} className="inline-flex shrink-0">
      <Tag tone="accent">
        {t("todos.owner_mark")}
        <span className="sr-only">{t("todos.owner_mark_said", { why })}</span>
      </Tag>
    </span>
  );
}

/** On a folded heading (a status section, a folder, a pinned direction, "N related"): how many to-dos under it the badge
 *  counts, so none of them is out of sight (DESIGN_SYSTEM 3.4 "Sessions that need the user"). Nothing at none. */
function OwnerMarkCount({ count }: { count: number }) {
  if (!count) return null;
  return (
    <span data-testid="todo-owner-count" data-count={count} className="inline-flex shrink-0">
      <Tag tone="accent">{t("todos.owner_mark_count", { count })}</Tag>
    </span>
  );
}

/** How many of these the badge counts (their views' `owner`). */
function ownerCount(todos: readonly Todo[], views: ReadonlyMap<string, TodoRowView>): number {
  return todos.reduce((n, td) => n + (views.get(td.id)?.owner ? 1 : 0), 0);
}

// ── Todo List Item ───────────────────────────────────────────

/** What a todo's row draws from its agents and their cards: plain values, so a push that leaves them alone leaves an
 *  equal view and the row skips its render. */
export type TodoRowView = {
  status: TodoStatus;
  dotState: AgentState;
  /** How long since it changed ("3 minutes ago"): here so a push that moves the text still redraws it, as before. */
  age: string;
  /** Its agents not released. */
  activeCount: number;
  /** Whether the most urgent agent's line shows (open, with a session, something to say). */
  urgentLine: boolean;
  /** That line's red lead ("<agent> Needs you<kicker>"), empty unless the agent waits on you. */
  urgentLead: string;
  /** What the agent asks (its card) or does / last said. */
  urgentText: string;
  /** The line says nobody is working on it (#3618): its lead is in the caution color, the reason after it. */
  orphaned: boolean;
  /** One of its agents runs with ultracode on (#12): the Agent list row's mark after its title. */
  ultracode: boolean;
  /** Its status section (sectionOf), in every grouping: its done mark reads open or done by it (checksAsOpen). */
  section?: TriageSection;
  /** What it waits on while its status is Waiting (#11090, lib/todo-wait-reason.ts waitOf): its label says so. */
  wait?: WaitReason;
  /** Why the sidebar's To-dos badge counts it (#11838, lib/todo-owner-needs.ts), unset when it does not: its mark. */
  owner?: OwnerNeed;
};

const DOT_STATE: Record<TodoStatus, AgentState> = {
  needs_you: "needs_you",
  interrupted: "needs_you",
  usage_limit: "idle",
  unread: "unread",
  working: "working",
  waiting: "working",
  follow_up: "idle",
  done: "offline",
  not_started: "offline",
  failed: "offline",
};

/** The view of one todo's row (TodoRowView). Every open row says its latest progress: what its agent asks or does,
 *  else what its agent last reported to its supervisor (#3156). */
function todoRowView(todo: Todo, lookup: SessionLookup, interactions: Interaction[]): TodoRowView {
  const view = agentRowView(todo, lookup, interactions);
  // Nobody is working on it (#3618): the hub's finding goes ahead of what a stopped agent last said (Interrupted,
  // Follow up). A card that waits for the user still says its question.
  const orphan = view.status !== "needs_you" ? orphanedLine(todo) : undefined;
  if (orphan) {
    view.urgentLine = true;
    view.orphaned = true;
    view.urgentLead = orphan.lead;
    view.urgentText = orphan.reason ? ` · ${orphan.reason}` : "";
    return view;
  }
  if (!view.urgentLine && !todo.done_at) {
    const said = parkedLine(todo) ?? progressLine(todo.supervision);
    if (said) {
      view.urgentLine = true;
      view.urgentText = said;
    }
  }
  return view;
}

/** How long since it or any of its agents last moved (#3156): the time the status sections go by. */
function lastMoved(todo: Todo, lookup: SessionLookup): string {
  const last = lastActivity(todo, lookup);
  return last ? ago(new Date(last).toISOString()) : ago(todo.updated_at || todo.created_at);
}

function agentRowView(todo: Todo, lookup: SessionLookup, interactions: Interaction[]): TodoRowView {
  const status = todoStatus(todo, lookup);
  const urgent = mostUrgentAgent(todo, lookup);
  const activeCount = (todo.agents ?? []).filter((a) => !a.released_at).length;
  const view: TodoRowView = { status, dotState: DOT_STATE[shownStatus(todo, status)], age: lastMoved(todo, lookup), activeCount, urgentLine: false, urgentLead: "", urgentText: "", orphaned: false, ultracode: todoUltracode(todo, lookup), wait: waitOf(todo, status, lookup) };
  if (todo.done_at || !urgent?.session) return view;
  const limit = status === "usage_limit" ? usageLimitLabel(urgent.session) : undefined;
  if (limit) {
    view.urgentLine = true;
    view.urgentText = limit;
    return view;
  }
  const urgentNow = taskNow(urgent.session, foldedState(urgent.session) === "working");
  // its own card, else one of its child sessions' (#5120: their cards wait on its page)
  const urgentIx = sessionCard(interactions, urgent.agent.machine_id, urgent.agent.session_id);
  // what it waits on, from its card (#4111): the question, the command; not its last words
  const urgentAsk = urgent.status === "needs_you" ? askText(urgentIx) : "";
  if (!urgentAsk && !urgentNow) return view;
  const kicker = urgentIx?.kind === "plan" ? t("todos.kicker_plan") : urgentIx?.kind === "permission" ? t("todos.kicker_permission") : urgentIx?.kind === "screen" ? t("todos.kicker_screen") : urgentIx?.kind === "elicitation" ? t("todos.kicker_elicitation") : t("todos.kicker_question");
  view.urgentLine = true;
  if (urgent.status === "needs_you") view.urgentLead = t("todos.urgent_line", { name: urgent.agent.title || urgent.session.title || "Agent", kicker });
  view.urgentText = urgentAsk || (urgentNow ? (urgentNow.kind === "words" ? plainMarkdown(urgentNow.text) : urgentNow.text) : "");
  return view;
}

/** Every todo's row view by id, with its status section and why the sidebar's badge counts it (`needs`, its own set). */
function todoRowViews(todos: Todo[], lookup: SessionLookup, interactions: Interaction[], sections: ReadonlyMap<string, TriageSection>, needs: ReadonlyMap<string, OwnerNeed>): ReadonlyMap<string, TodoRowView> {
  return new Map(todos.map((td) => [td.id, { ...todoRowView(td, lookup, interactions), section: sections.get(td.id), owner: needs.get(td.id) }]));
}

const TONE_CLASS = TODO_TONE_CLASS;

const TodoListItem = memo(function TodoListItem({
  todo,
  view,
  selected,
  onSelect,
  section,
  onRestore,
  onSeen,
}: {
  todo: Todo;
  view: TodoRowView;
  selected: boolean;
  onSelect: (id: string) => void;
  /** Its status section (#3156): a PR to merge shows its link, a shelved one its Restore, a Just finished one Got it. */
  section?: TriageSection;
  onRestore?: (id: string) => void;
  /** Got it (#5707), on a row in Just finished (`section`, else the view's). */
  onSeen?: (id: string) => void;
}) {
  const { status, dotState, activeCount } = view;
  const showWait = (section ?? view.section) === "parked";
  // the PR it waits on: its current one, or an earlier one still open (#7935)
  const openPR = (section ?? view.section) === "parked" ? openMergePRs(todo, Date.now())[0] : undefined;
  // its state word and tone; a waiting one's say what it waits on, attention only on the owner (#11090)
  const look = todoLook(todo, status, view.wait);
  // its steps as the hub counts them (#11858, lib/todo-steps.ts): in place of Not started, or after its Agent count
  const steps = stepsWords(todo, status);
  const held = heldDelivery(todo);
  const responsible = responsibleOf(todo);
  const thread = useStore((s) => todoThreadsOn(s.features)) ? boundThread(todo) : undefined;
  // a failed start still waiting on the owner reads open, not done, with why it did not start (#3713), in every grouping
  const open = checksAsOpen(todo, section ?? view.section);
  const failedOpen = open && Boolean(todo.start_error);

  return (
    <div
      role="button"
      tabIndex={0}
      data-todo={todo.id}
      aria-current={selected || undefined}
      onClick={() => onSelect(todo.id)}
      // Enter / Space on the row itself (not on its own buttons) opens it, as a click does
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget || (e.key !== "Enter" && e.key !== " ")) return;
        e.preventDefault();
        onSelect(todo.id);
      }}
      className={cx(
        "group/item flex cursor-pointer gap-2.5 rounded-control border p-2.5 transition-colors focus-visible:outline-2 focus-visible:outline-accent",
        selected ? "border-accent/30 bg-accent-tint/70" : "border-transparent hover:bg-hover",
      )}
    >
      {/* Done or open, as a mark of a fixed size, so every row's title starts in the same place. It changes nothing: the
          agents close and reopen to-dos. The status line says the state in words. */}
      <span
        aria-hidden
        data-testid="todo-done-mark"
        data-done={!open || undefined}
        className={cx(
          "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border",
          !open ? "border-accent bg-accent text-white" : "border-line-strong text-transparent",
        )}
      >
        <Icon name="check" size={10} stroke={2.6} />
      </span>

      {/* Body */}
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          {view.ultracode || todo.kind === "concern" || view.owner ? (
            <span className="flex min-w-0 items-center gap-1">
              <span className={cx("truncate text-[13.5px] font-semibold text-ink", !view.ultracode && !open && "text-ink-3 font-normal", !view.ultracode && strikesThrough(todo, open) && "line-through")} data-testid="todo-title">{todo.title}</span>
              {view.ultracode && <UltraMark size={11} />}
              {todo.kind === "concern" && <Tag><span data-testid="todo-concern">{t("todos.concern")}</span></Tag>}
              {view.owner && <OwnerMark need={view.owner} />}
            </span>
          ) : (
            <span className={cx("truncate text-[13.5px] font-semibold text-ink", !open && "text-ink-3 font-normal", strikesThrough(todo, open) && "line-through")} data-testid="todo-title">
              {todo.title}
            </span>
          )}
          <span className="flex shrink-0 items-center gap-1.5 text-[11px] text-ink-3" data-testid="todo-age">
            {thread && (
              <span data-testid="todo-thread-mark" role="img" aria-label={t("todos.thread_mark")} title={threadLine(thread)} className="inline-flex">
                <Icon name="chat" size={12} stroke={2} />
              </span>
            )}
            {view.age}
          </span>
        </div>

        {/* Status Line */}
        <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[12px]" data-testid="todo-status-line">
          {open && <StatusMark state={dotState} size="sm" />}
          <span className={cx("shrink-0 whitespace-nowrap font-medium", TONE_CLASS[look.tone])} data-testid="todo-status">{steps?.label ?? look.label}</span>
          {!todo.done_at && activeCount > 0 && (
            <>
              <span className="text-ink-3">·</span>
              <span className="text-ink-3">{t("todos.agent_count", { count: activeCount })}</span>
            </>
          )}
          {steps?.after && (
            <>
              <span className="text-ink-3">·</span>
              <span className="shrink-0 whitespace-nowrap text-ink-3" data-testid="todo-steps">{steps.after}</span>
            </>
          )}
          {responsible && (
            <>
              <span className="text-ink-3">·</span>
              <span
                data-testid="todo-responsible"
                data-kind={responsible.kind}
                title={t("todos.responsible_label", { name: responsible.name })}
                className="inline-flex min-w-0 items-center gap-1 truncate text-ink-2"
              >
                <Icon name={responsible.kind === "person" ? "person" : "terminal"} size={11} stroke={2} className="shrink-0 text-ink-3" />
                <span className="truncate">{responsible.name}</span>
              </span>
            </>
          )}
          {/* its steps were handed out: what is next once they are all closed, else nothing about handing it over */}
          {!todo.done_at && activeCount === 0 && (steps?.tail !== undefined ? (
            steps.tail && <span className="truncate text-ink-3" data-testid="todo-steps-next">· {steps.tail}</span>
          ) : !responsible ? (
            <span className="truncate text-ink-3">· {t("todos.not_handed")}</span>
          ) : null)}
          {(section ?? view.section) === "just_done" && seenCanFile(todo) && onSeen ? (
            <span className="ml-auto flex shrink-0 items-center gap-1.5">
              {!todo.done_at && <ReviewBadge todo={todo} />}
              <SeenButton onClick={() => onSeen(todo.id)} />
            </span>
          ) : (
            !todo.done_at && <ReviewBadge todo={todo} className="ml-auto" />
          )}
        </div>

        {/* What it waits on from the user (#2056), to read: it is answered in the conversation it came from */}
        <TodoAskLine todo={todo} readOnly className="mt-1" />

        {showWait && <div className="mt-1" data-testid={openPR ? "todo-pr-line" : undefined}><TaskWaitInfo todo={todo} merges={openMergePRs(todo, Date.now())} /></div>}

        {/* Why its agent did not start, while it waits on the owner (DESIGN_SYSTEM 5.6: what failed, in red) */}
        {failedOpen && <div className="mt-1 truncate text-[11.5px] text-red" data-testid="todo-start-failed">{t("todos.start_failed", { error: todo.start_error ?? "" })}</div>}

        {/* Why it was cancelled, or why the legacy hub auto-closed it */}
        {isCancelled(todo) && cancellationReason(todo) && (
          <div className="mt-1 truncate text-xs text-ink-3" data-testid="todo-cancelled-reason">{cancellationReason(todo)}</div>
        )}
        {isAutoDone(todo) && todo.done_reason && (
          <div className="mt-1 truncate text-[11.5px] text-ink-3">{todo.done_reason}</div>
        )}

        {/* Where its finished work is held short of live */}
        {held && (
          <div className="mt-1 truncate text-[11.5px] text-ink-3">
            <span className="font-medium text-orange">{t(`todos.delivery_${held.state}`)}</span>
            {held.gate && <> · {t("todos.delivery_gate", { gate: held.gate })}</>}
            {held.unblock && ["user", "agent", "other", "supervisor"].includes(held.unblock) && <> · {t(`todos.unblock_${held.unblock}`)}</>}
          </div>
        )}

        {/* Most Urgent Agent Line */}
        {view.urgentLine && !showWait && (
          <div className="mt-1 truncate text-[11.5px] text-ink-3" data-orphaned={view.orphaned || undefined}>
            {view.urgentLead && <span className={cx("font-medium", view.orphaned ? "text-orange" : "text-attention")}>{view.urgentLead}</span>}
            {view.urgentText}
          </div>
        )}

        {/* Shelved: one tap brings it back (#3156); nothing is deleted */}
        {section === "shelved" && onRestore && (
          <div className="mt-1.5" onClick={(e) => e.stopPropagation()}>
            <Button tone="secondary" icon="undo" onClick={() => onRestore(todo.id)} className="h-7 text-[12px] whitespace-nowrap">
              {t("todos.restore")}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
});

/** Got it on an ordinary, unsupervised Delivered · awaiting review row (#5707): files it under Done at once. A small
 * capsule at the end of the status line, under the time (#6578), so the row is as tall as any other: its 20 px hang
 * over the 18 px line (the negative margin keeps them out of the line's height), and `touch-hit` gives a finger 44 px.
 * Its press stays its own and never opens the row. Supervised work and unaccepted deliveries get no such receipt. */
function SeenButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      data-testid="todo-seen"
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className="touch-hit -my-1 inline-flex h-5 shrink-0 items-center gap-1 rounded-full bg-surface px-2 text-[11.5px] font-medium whitespace-nowrap text-ink-2 shadow-btn transition-colors hover:bg-hover hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
    >
      <Icon name="check" size={11} stroke={2.2} />
      {t("todos.seen")}
    </button>
  );
}

/** A to-do whose decision is held for the briefing (#4930, #5803):
 *  nothing was pushed for it, and it comes with the next briefing. The to-do's page writes out what that means under
 *  its ask; a row's ask line carries the quiet mark itself (views/TodoAsk AskLine), as the decision's card does
 *  (views/assistant/BriefingMark). Nothing for any other to-do, and for a hub that does not say. */
export function HeldForBriefing({ todo }: { todo: Pick<Todo, "ask"> }) {
  const ask = todo.ask;
  if (!ask?.verdict || !ask.asked_at || ask.for_briefing !== true) return null;
  return (
    <p data-for-briefing="caption" className="mt-2 flex min-w-0 items-start gap-1.5 px-1 text-xs text-ink-2 [overflow-wrap:anywhere]">
      <Icon name="clock" size={13} className="mt-0.5 shrink-0" />
      <span>
        <span className="font-medium">{t("todos.ask_held")}</span>
        {t("assistant-decisionrow.separator")}
        {t("todos.ask_held_hint")}
      </span>
    </p>
  );
}

/** What a waiting agent's card asks, as one line of plain text; empty for a plan (its kicker says it) or no card. */
function askText(ix: Interaction | undefined): string {
  if (!ix || ix.kind === "plan") return "";
  return plainMarkdown(interactionAsk(ix)).replace(/\s+/g, " ").trim();
}

// ── Detail parts ─────────────────────────────────────────────

// A task's brief (#3822) comes with the first to-do that has one, not with the page.
const TodoRequestGraph = lazy(() => import("./TodoRequestGraph"));
const TaskBriefPanel = lazy(() => import("./assistant/TaskBrief").then((m) => ({ default: m.TaskBriefPanel })));
const TaskBriefHistory = lazy(() => import("./assistant/TaskBrief").then((m) => ({ default: m.TaskBriefHistory })));
const BriefResultExtras = lazy(() => import("./assistant/TaskBrief").then((m) => ({ default: m.BriefResultExtras })));
const TodoImageViewer = lazyPage(() => import("./chat/ImageViewer").then((m) => m.ImageViewer));

/** The task's criteria, each as its latest report or review found it, and what the supervisor said last. */
export function ReviewChecklist({ todo, lookup }: { todo: Todo; lookup?: SessionLookup }) {
  const sup = todo.supervision;
  if (!sup) return null;
  const list = checklist(sup);
  const review = latestReview(sup);
  const parked = triage(todo, lookup, Date.now()).section === "parked";
  // What this list cannot say of finished work (#3822): the criteria a scope change dropped, and what was done beyond the brief.
  const extras = todo.done_at ? resultExtras(todo) : undefined;
  return (
    <section data-testid="review-checklist" className="mt-4 rounded-control bg-inset p-3.5 shadow-hairline">
      <div className="flex items-center gap-2">
        <span className="text-[12.5px] font-semibold text-ink-2">{t("assistant-tasks.review_title")}</span>
        <ReviewBadge todo={todo} />
        <span className="flex-1" />
        {sup.expect_minutes ? <span className="text-[12px] text-ink-3">{t("assistant-tasks.review_expect", { minutes: sup.expect_minutes })}</span> : null}
      </div>
      {list.length === 0 ? (
        <p className="mt-2 text-[12.5px] text-ink-3">{t("assistant-tasks.review_none")}</p>
      ) : (
        <ul className="mt-2 flex flex-col gap-1.5">
          {list.map((c) => {
            return (
              <li key={c.criterion} className="flex items-start gap-2 text-[13px]">
                <CriterionMark mark={c.mark} className="mt-0.5" />
                <span className="flex min-w-0 flex-col">
                  <span className="text-ink">{c.criterion}</span>
                  {c.evidence && <span className="text-[12px] text-ink-3 select-text">{c.evidence}</span>}
                  {(c.at || c.by) && (
                    <span className="flex flex-wrap gap-x-1.5 text-[11px] text-ink-3">
                      {c.by && <span>{c.by}</span>}
                      {c.at && <time dateTime={c.at} title={c.at}>{new Date(c.at).toLocaleString()}</time>}
                    </span>
                  )}
                </span>
              </li>
            );
          })}
        </ul>
      )}
      {extras && (
        <Suspense fallback={null}>
          <BriefResultExtras dropped={extras.dropped} alsoDone={extras.alsoDone} />
        </Suspense>
      )}
      {parked && <div data-testid="review-parked" className="mt-2 select-text"><TaskWaitInfo todo={todo} merges={openMergePRs(todo, Date.now())} expanded /></div>}
      {review?.note && <p className="mt-2 text-[12.5px] leading-[1.6] text-ink-2 select-text">{t("assistant-tasks.review_note", { note: review.note })}</p>}
      <AutoReviewResult sup={sup} />
    </section>
  );
}

/** The independent reviewer's result (#2173) under the criteria: a line, and its report on a click. Nothing when no
 *  review ran, so a task without one looks as it did. */
function AutoReviewResult({ sup }: { sup: Supervision }) {
  const [open, setOpen] = useState(false);
  const line = autoReviewLine(sup);
  if (!line) return null;
  const findings = sup.auto_review?.findings;
  return (
    <div data-testid="auto-review" className="mt-2 flex flex-col gap-1 text-[12.5px] text-ink-2">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <Icon name="target" size={12} className="shrink-0" />
        <span className="shrink-0 whitespace-nowrap font-semibold">{t("assistant-task-actions.auto_review_title")}</span>
        <span className="min-w-0">· {line}</span>
        {findings && (
          <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="touch-hit ml-auto shrink-0 whitespace-nowrap text-ink-3 hover:text-ink-2">
            {t(open ? "assistant-task-actions.auto_review_hide" : "assistant-task-actions.auto_review_show")}
          </button>
        )}
      </div>
      {open && findings && (
        <div data-testid="auto-review-findings" className="leading-[1.55] whitespace-pre-wrap text-ink select-text [overflow-wrap:anywhere]">
          {findings}
        </div>
      )}
    </div>
  );
}

const TODO_IMAGE_LIMIT = 50;
const TODO_IMAGE_PARALLEL = 3;

type TodoImageEntry = {
  id: string;
  path: string;
  state: "queued" | "reading" | "ready" | "failed";
  url?: string;
};
type TodoImageBook = {
  scope: string;
  owner: string;
  manager: string;
  token: string;
  ac: AbortController;
  entries: Map<string, TodoImageEntry>;
  queue: TodoImageEntry[];
  running: number;
};

/** Only the owning manager's own media route is safe to turn back into apiBlob's path form. */
function storedTodoImagePath(todoId: string, image: TodoImage): string | undefined {
  const expected = `/api/v1/todos/${encodeURIComponent(todoId)}/images/${encodeURIComponent(image.id)}`;
  return image.url === expected ? image.url.slice("/api/v1".length) : undefined;
}

/** Read each listed private image once, at most three at a time. A refresh reconciles metadata without replacing
 *  unchanged object URLs; a credential, manager or to-do change retires the whole book and every late answer. */
function useStoredTodoImages(todo: Todo, owner: string) {
  const manager = todo.manager ?? defaultManager();
  const scope = `${owner}/${manager}/${todo.id}`;
  const images = useMemo(() => (todo.images ?? []).slice(0, TODO_IMAGE_LIMIT).filter((image) => storedTodoImagePath(todo.id, image)), [todo.id, todo.images]);
  const book = useRef<TodoImageBook | null>(null);
  const [, setRevision] = useState(0);
  const redraw = useCallback(() => setRevision((n) => n + 1), []);
  const drop = useCallback(() => {
    const b = book.current;
    if (!b) return;
    b.ac.abort();
    b.entries.forEach((entry) => { if (entry.url) URL.revokeObjectURL(entry.url); });
    book.current = null;
  }, []);
  const pump = useCallback((b: TodoImageBook) => {
    while (b.running < TODO_IMAGE_PARALLEL && b.queue.length) {
      const entry = b.queue.shift()!;
      if (b.entries.get(entry.id) !== entry || entry.state !== "queued") continue;
      entry.state = "reading";
      b.running++;
      apiBlob(entry.path, {
        signal: b.ac.signal,
        token: b.token,
        tokenOwner: b.owner,
        isAuthContextCurrent: () => book.current === b && credentialOwner() === b.owner,
        headers: { [MANAGER_HEADER]: b.manager },
      }).then((blob) => {
        if (book.current !== b || b.ac.signal.aborted || b.entries.get(entry.id) !== entry || entry.state !== "reading" || credentialOwner() !== b.owner) return;
        entry.url = URL.createObjectURL(blob);
        entry.state = "ready";
        redraw();
      }).catch(() => {
        if (book.current !== b || b.ac.signal.aborted || b.entries.get(entry.id) !== entry || entry.state !== "reading" || credentialOwner() !== b.owner) return;
        entry.state = "failed";
        redraw();
      }).finally(() => {
        b.running--;
        if (book.current === b) pump(b);
      });
    }
  }, [redraw]);

  useEffect(() => {
    let b = book.current;
    let changed = false;
    if (!b || b.scope !== scope) {
      drop();
      b = { scope, owner, manager, token: getToken(), ac: new AbortController(), entries: new Map(), queue: [], running: 0 };
      book.current = b;
      changed = true;
    }
    const wanted = new Map(images.map((image) => [image.id, storedTodoImagePath(todo.id, image)!] as const));
    b.entries.forEach((entry, id) => {
      if (wanted.get(id) === entry.path) return;
      if (entry.url) URL.revokeObjectURL(entry.url);
      b.entries.delete(id);
      changed = true;
    });
    b.queue = b.queue.filter((entry) => b.entries.get(entry.id) === entry);
    for (const [id, path] of wanted) {
      if (b.entries.has(id)) continue;
      const entry: TodoImageEntry = { id, path, state: "queued" };
      b.entries.set(id, entry);
      b.queue.push(entry);
      changed = true;
    }
    pump(b);
    if (changed) redraw();
  }, [scope, owner, manager, todo.id, images, drop, pump, redraw]);
  useEffect(() => () => drop(), [drop]);

  const retry = useCallback((id: string) => {
    const b = book.current;
    const entry = b?.scope === scope ? b.entries.get(id) : undefined;
    if (!b || !entry || entry.state !== "failed") return;
    entry.state = "queued";
    b.queue.push(entry);
    redraw();
    pump(b);
  }, [scope, pump, redraw]);
  return { images, entries: book.current?.scope === scope ? book.current.entries : undefined, retry };
}

function TodoImages({ todo, owner }: { todo: Todo; owner: string }) {
  const { images, entries, retry } = useStoredTodoImages(todo, owner);
  const [open, setOpen] = useState<string>();
  const shown = images.map((image, index) => {
    const entry = entries?.get(image.id);
    return { image, entry, name: image.name?.trim() || t("todos.image_label", { n: index + 1 }) };
  });
  const ready = shown.flatMap(({ image, entry, name }) => entry?.url ? [{ id: image.id, name, url: entry.url, mime: image.mime }] : []);
  const failed = shown.filter(({ entry }) => entry?.state === "failed");
  const openAt = ready.findIndex((image) => image.id === open);
  if (!images.length) return null;
  return (
    <section className="mt-4" aria-label={t("todos.images_section", { count: images.length })}>
      <div className="mb-2 text-submeta font-medium text-ink-3">{t("todos.images_section", { count: images.length })}</div>
      <AttachmentTray attachments={ready} onOpen={setOpen} openLabel={(name) => t("todos.image_open", { name })} />
      {ready.length === 0 && failed.length === 0 && <p className="text-submeta text-ink-3">{t("todos.images_loading")}</p>}
      {failed.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-2">
          {failed.map(({ image, name }) => (
            <Button key={image.id} tone="quiet" icon="retry" className="touch-hit whitespace-nowrap" onClick={() => retry(image.id)}>
              {t("todos.image_retry", { name })}
            </Button>
          ))}
        </div>
      )}
      {openAt >= 0 && (
        <Suspense fallback={null}>
          <TodoImageViewer
            images={ready.map((image) => ({ id: image.id, src: image.url, mime: image.mime, label: image.name, name: image.name }))}
            start={openAt}
            onClose={() => setOpen(undefined)}
          />
        </Suspense>
      )}
    </section>
  );
}
// ── Detail sections ──────────────────────────────────────────

/** One of the to-do page's sections: its heading, with a count or a tally beside it and its own view controls at the
 *  right, over what the main conversation and the agents wrote there. */
function DetailSection({ title, aside, right, testId, children }: { title: string; aside?: ReactNode; right?: ReactNode; testId: string; children: ReactNode }) {
  const heading = useId();
  return (
    <section aria-labelledby={heading} data-testid={testId} data-todo-section className="mt-8">
      <div className="flex min-h-7 flex-wrap items-center gap-x-2 gap-y-1">
        <h3 id={heading} className="text-sm font-semibold text-ink">{title}</h3>
        {aside}
        {right && <div className="ml-auto flex shrink-0 items-center gap-1">{right}</div>}
      </div>
      {children}
    </section>
  );
}

/** How many a section's heading counts, beside it. */
function SectionCount({ count, testId }: { count: number; testId?: string }) {
  return <span data-testid={testId} className="text-meta text-ink-3 tabular-nums">{count}</span>;
}

/** What a section says while nothing has been written into it yet. */
function SectionEmpty({ children }: { children: ReactNode }) {
  return (
    <p data-testid="todo-section-empty" className="mt-2 rounded-control border border-dashed border-line px-3 py-3 text-meta leading-[1.6] text-ink-3">
      {children}
    </p>
  );
}

/** The to-do's notes as its agents wrote them, Markdown rendered. Nothing while there are none. */
function TodoNotes({ notes }: { notes: string }) {
  if (!notes.trim()) return null;
  return (
    <section data-testid="todo-notes" className="mt-4 rounded-control bg-inset p-3.5 shadow-hairline">
      <div className="flex items-center gap-2 text-[12.5px] text-ink-2">
        <Icon name="file" size={12} className="shrink-0" />
        <h4 className="min-w-0 flex-1 truncate font-semibold">{t("todos.notes_title")}</h4>
      </div>
      <div className="mt-2 min-w-0 select-text text-ink [overflow-wrap:anywhere]">
        <Markdown text={notes} small lineBreaks />
      </div>
    </section>
  );
}

/** A task's criteria while nobody reviews them yet: the brief's, none checked. A supervised task has its checklist. */
function BriefCriteria({ criteria }: { criteria: string[] }) {
  return (
    <ul data-testid="todo-brief-criteria" className="mt-3 flex flex-col gap-1.5 rounded-control bg-inset p-3.5 shadow-hairline">
      {criteria.map((c, i) => (
        <li key={i} className="flex items-start gap-2 text-meta">
          <CriterionMark mark="open" className="mt-0.5" />
          <span className="min-w-0 text-ink select-text [overflow-wrap:anywhere]">{c}</span>
        </li>
      ))}
    </ul>
  );
}

// ── Todo Detail ──────────────────────────────────────────────

/** The open to-do, to read: its header, then the brief, the acceptance criteria, its agent sessions, its repositories
 *  and its decisions. The main conversation and the agents write all of it; nothing here changes the to-do. */
function TodoDetail({
  todo,
  owner,
  activeAgents,
  releasedAgents,
  lookup,
  machines,
  interactions,
  showReleased,
  onToggleReleased,
  open,
  onOpenTodo,
  todos,
}: {
  todo: Todo;
  owner: string;
  activeAgents: TodoAgent[];
  releasedAgents: TodoAgent[];
  lookup: ReturnType<typeof makeSessionLookup>;
  machines: Machine[];
  interactions: Interaction[];
  showReleased: boolean;
  onToggleReleased: () => void;
  /** Whether it reads open, as its row's mark does (checksAsOpen): a failed start waiting on the owner is (#3713). */
  open: boolean;
  onOpenTodo: (id: string) => void;
  /** The list's to-dos: a supervised to-do's workers, or a worker's root, are among them. */
  todos: readonly Todo[];
}) {
  const allSessions = useAgentSessions();
  const spacesSupported = useStore((s) => s.spacesSupported);

  const hasLiveAgent = (mid: string, spaceName: string) => {
    return allSessions.some(
      (s) =>
        s.machine_id === mid &&
        (s.space_name || s.space_id) === spaceName &&
        stateOf(s) !== "offline" &&
        machines.some((m) => m.id === mid && m.online),
    );
  };

  // A task whose agent never started, waiting on the owner (#3713, audit item 13): open, as its row reads, with why it
  // did not start.
  const failedStart = open && Boolean(todo.start_error);
  const status = todoStatus(todo, lookup);
  // its state word and tone; a waiting one's say what it waits on, attention only on the owner (#11090)
  const look = todoLook(todo, status, waitOf(todo, status, lookup));
  // its steps as the hub counts them (#11858), as its row says them
  const steps = stepsWords(todo, status);
  const responsible = responsibleOf(todo);
  // The to-do's own PR, parsed once here (the repository section's fallback for a hub without the store).
  const legacyPR = prLink(todo.pr_url);
  const toneClass = {
    red: "bg-red-tint text-red",
    green: "bg-green-tint text-green",
    amber: status === "needs_you" ? "bg-field text-attention" : "bg-orange-tint text-orange",
    idle: "bg-hover text-ink-2",
    off: "bg-hover text-ink-3",
    attention: "bg-field text-attention",
  }[look.tone];

  const roster = activeAgents.map((agent) => {
    const session = lookup(agent.machine_id, agent.session_id);
    const state = session && !agent.start_error ? foldedState(session) : "offline";
    const waiting = session && state === "working" && !session.children?.working && backgroundWaiting(session);
    return { agent, session, state, waiting };
  });
  const counts: FoldedCounts = { needs_you: 0, working: 0, bg_waiting: 0, unread: 0 };
  for (const { state, waiting } of roster) {
    if (waiting) counts.bg_waiting++;
    else if (state === "needs_you" || state === "working" || state === "unread") counts[state]++;
  }
  // a supervisor Agent's to-do, with the workers it handed out, or a worker of one with its root (lib/todo-supervisor.ts)
  const role = useMemo(() => supervisionRole(todo, todos), [todo, todos]);
  const leader = supervisorOf(todo);
  const workerRoot = role?.kind === "worker" ? role.root : undefined;
  // the supervisor first (its workers hang under it), then what needs the owner
  const attentionFirst = [...roster].sort((a, b) => Number(b.agent === leader) - Number(a.agent === leader) || Number(b.state === "needs_you") - Number(a.state === "needs_you"));

  const shown = shownStatus(todo, status);
  const dotState =
    shown === "needs_you" || shown === "interrupted"
      ? "needs_you"
      : shown === "usage_limit"
        ? "idle"
      : shown === "unread"
        ? "unread"
        : shown === "working" || shown === "waiting"
          ? "working"
          : shown === "follow_up"
            ? "idle"
            : "offline";

  const criteria = todo.brief ? briefCriteria(todo.brief) : [];
  const spaces = spacesSupported !== false ? todo.spaces ?? [] : [];

  return (
    // a page's reading width (DESIGN_SYSTEM 2.5 "Widths", as the inbox's detail): a wide window centers it (audit item 75)
    <div className="mx-auto flex min-h-full w-full max-w-[760px] flex-col p-6 sm:p-8" data-testid="todo-detail">
      {/* Header: the title, its state and who has it, and what explains that state */}
      <h2 data-testid="todo-detail-title" className="min-w-0 text-[22px] leading-snug font-bold text-ink select-text [overflow-wrap:anywhere]">
        {todo.title}
      </h2>

      {/* Meta Line */}
      <div className="mt-2.5 flex flex-wrap items-center gap-2.5 text-[12.5px] text-ink-2">
        <span className={cx("inline-flex h-5.5 items-center gap-1.5 rounded-md px-2 text-[12px] font-semibold", toneClass)}>
          <StatusMark state={dotState} size="sm" />
          <span data-testid="todo-detail-status">{steps?.label ?? look.label}</span>
        </span>
        {steps?.tail && (
          <>
            <span className="text-ink-3">·</span>
            <span data-testid="todo-detail-steps-next">{steps.tail}</span>
          </>
        )}
        {responsible && (
          <>
            <span className="text-ink-3">·</span>
            <span
              data-testid="todo-detail-responsible"
              data-kind={responsible.kind}
              title={t("todos.responsible_label", { name: responsible.name })}
              className="inline-flex min-w-0 items-center gap-1 text-ink-2"
            >
              <Icon name={responsible.kind === "person" ? "person" : "terminal"} size={12} stroke={2} className="shrink-0 text-ink-3" />
              <span className="truncate">{responsible.name}</span>
            </span>
          </>
        )}
        {steps?.after && (
          <>
            <span className="text-ink-3">·</span>
            <span data-testid="todo-detail-steps">{steps.after}</span>
          </>
        )}
        <span className="text-ink-3">·</span>
        <span>{t("todos.created_at", { time: clock(todo.created_at) })}</span>
      </div>
      {isCancelled(todo) && cancellationReason(todo) && (
        <p className="mt-2 text-xs leading-[1.6] text-ink-3" data-testid="todo-detail-cancelled-reason">
          {t("todos.cancelled_reason", { reason: cancellationReason(todo) })}
        </p>
      )}
      {failedStart && (
        <p className="mt-2 text-meta leading-[1.6] text-red" data-testid="todo-detail-start-failed">
          {t("todos.start_failed", { error: todo.start_error ?? "" })}
        </p>
      )}
      {isAutoDone(todo) && (
        <p className="mt-2 text-[12.5px] leading-[1.6] text-ink-3">
          {todo.done_reason ? t("todos.auto_done_reason", { reason: todo.done_reason }) : t("todos.auto_done")}
        </p>
      )}

      {/* Code task stages from the hub. Its PR link is not drawn again here: the repository section is the one place
          a task's issues and PRs are shown (legacy ones included). */}
      <TodoStageBar stage={todo.stage} stageKind={todo.stage_kind} done={todo.stage_done} className="mt-4" />

      {/* What it waits on from the user (#2056), and where it is answered */}
      <TodoAskPanel todo={todo} />
      <HeldForBriefing todo={todo} />

      {/* 1. The brief its work is held to (#3822): who asked and in what words, the scope changes and who worked on it,
          the notes its agents keep, their latest report, and the pictures it came with. */}
      <DetailSection title={t("todos.detail_brief")} testId="todo-section-brief">
        {todo.brief ? (
          <Suspense fallback={null}>
            <TaskBriefPanel todo={todo} />
            <TaskBriefHistory todo={todo} onOpenTodo={onOpenTodo} />
          </Suspense>
        ) : (
          <SectionEmpty>{t("todos.brief_empty")}</SectionEmpty>
        )}
        <TodoNotes notes={todo.notes ?? ""} />
        <LatestReport todo={todo} />
        <TodoImages todo={todo} owner={owner} />
      </DetailSection>

      {/* 2. What it is held to: the supervisor's checklist, else the brief's criteria, none checked yet */}
      <DetailSection title={t("todos.detail_criteria")} testId="todo-section-criteria">
        {todo.supervision ? (
          <ReviewChecklist todo={todo} lookup={lookup} />
        ) : criteria.length > 0 ? (
          <BriefCriteria criteria={criteria} />
        ) : (
          <SectionEmpty>{t("todos.criteria_empty")}</SectionEmpty>
        )}
      </DetailSection>

      {/* What its supervisor Agent did with the workers, and how the main conversation took the result; only on a
          to-do a supervisor runs and on its workers */}
      {role && <SupervisionSection todo={todo} role={role} onOpenTodo={onOpenTodo} />}
      <AgentLifecycleTimeline key={todo.id} todo={todo} lookup={lookup} />

      {/* 3. The agent sessions on it, each opening its session, and those that took part; a supervisor first, with the
          workers it handed out beneath it; on a worker, the to-do it works for */}
      <DetailSection
        title={t("todos.detail_sessions")}
        aside={<><SectionCount count={activeAgents.length} testId="todo-agent-count" /><StateTally counts={counts} /></>}
        testId="todo-section-sessions"
      >
        {workerRoot && (
          <p data-testid="todo-supervised-by" className="mt-1 min-w-0 truncate text-label text-ink-3">
            {tRich("todos.supervised_by", {
              title: (
                <button type="button" data-open-todo={workerRoot.id} onClick={() => onOpenTodo(workerRoot.id)} className="touch-hit text-accent-ink hover:underline">
                  {workerRoot.title}
                </button>
              ),
            })}
          </p>
        )}
        <div className="mt-3 divide-y divide-line rounded-control border border-line bg-canvas" data-testid="todo-agent-roster">
          {attentionFirst.length === 0 ? (
            <p className="px-3 py-3 text-meta text-ink-3">
              {open ? (responsible ? t("todos.no_agents_responsible", { name: responsible.name }) : t("todos.no_agents")) : t("todos.no_agents_done")}
            </p>
          ) : (
            <ul className="divide-y divide-line">
              {attentionFirst.map(({ agent, session, state, waiting }) => {
                const now = taskNow(session, state === "working");
                const displayTitle = session?.title || agent.title || t("lib-format.untitled_session");
                const machineName = machines.find((m) => m.id === agent.machine_id)?.name;
                const spaceTab = session ? [session.space_name, tabLabel(session)].filter(Boolean).join(" › ") : "";
                const displaySub = [machineName, spaceTab || shortPath(session?.cwd || agent.cwd)].filter(Boolean).join(" · ");
                const ix = sessionCard(interactions, agent.machine_id, agent.session_id);
                const kicker = ix?.kind === "plan" ? t("todos.kicker_plan") : ix?.kind === "permission" ? t("todos.kicker_permission") : ix?.kind === "screen" ? t("todos.kicker_screen") : ix?.kind === "elicitation" ? t("todos.kicker_elicitation") : t("todos.kicker_question");
                const sourceLabel = formatAgentSource(agent, todo.spaces, lookup);
                const stateLabel = agent.start_error ? stateName("failed") : !session ? t("todos.ended") :
                  waiting ? stateName("bg_waiting") : state === "idle" && session.work?.just_finished ? stateName("just_finished") : stateName(state);
                const AgentRow = session ? "button" : "div";
                // the supervisor's row wraps the tree of its workers onto a line of its own beneath it
                const workers = agent === leader && role?.kind === "root" ? role.workers : undefined;

                return (
                  <li key={JSON.stringify([agent.machine_id, agent.session_id])} className={cx("flex items-center gap-2 px-2", workers && "flex-wrap gap-y-0")}>
                    <AgentRow
                      type={session ? "button" : undefined}
                      onClick={session ? () => openFromPage({ name: "session", mid: agent.machine_id, sid: agent.session_id }) : undefined}
                      className={cx("my-1 flex min-w-0 flex-1 items-start gap-2 rounded-control px-1.5 py-2 text-left", session && "cursor-pointer hover:bg-hover focus-visible:outline-2 focus-visible:outline-accent")}
                    >
                      <span className="mt-0.5"><StatusMark state={state} size="sm" label={false} waiting={waiting} /></span>
                      <span className="min-w-0 flex-1">
                        <span className="flex min-w-0 items-center gap-2">
                          {sourceLabel && (
                            <span data-testid="todo-agent-source" title={sourceLabel} className="max-w-[45%] shrink-0 truncate rounded bg-hover px-1.5 py-0.5 text-meta text-ink-3">
                              {sourceLabel}
                            </span>
                          )}
                          <span data-testid="todo-agent-title" title={displayTitle} className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">
                            {displayTitle}
                          </span>
                        </span>
                        <span className="mt-0.5 flex flex-wrap items-baseline gap-x-2 text-meta text-ink-3">
                          <span className={cx("shrink-0", state === "needs_you" && "font-medium text-attention")}>{stateLabel}</span>
                          {displaySub && <span className="min-w-0 truncate">{displaySub}</span>}
                          {!!session?.children?.total && <span>{t("todos.linked_subagents", { count: session.children.total })}</span>}
                        </span>
                        {agent.start_error ? (
                          <span className="mt-1 block truncate text-meta text-red">{t("todos.start_failed", { error: agent.start_error })}</span>
                        ) : now || (state === "needs_you" && askText(ix)) ? (
                          <span className="mt-1 line-clamp-2 text-meta text-ink-2">
                            {state === "needs_you" && <span className="font-medium text-attention">{t("todos.kicker_line", { kicker })}</span>}
                            {(state === "needs_you" && askText(ix)) || (now && (now.kind === "words" ? plainMarkdown(now.text) : now.text))}
                          </span>
                        ) : null}
                      </span>
                    </AgentRow>
                    <span data-testid="todo-agent-time" className="shrink-0 pr-1.5 whitespace-nowrap text-meta text-ink-3">
                      {ago(session?.updated_at || agent.linked_at)}
                    </span>
                    {workers && <SupervisorWorkers workers={workers} lookup={lookup} machines={machines} onOpenTodo={onOpenTodo} />}
                  </li>
                );
              })}
            </ul>
          )}
          {/* Released Agents (Participated) */}
          {releasedAgents.length > 0 && (
            <div className="px-3 py-2.5">
              <button
                type="button"
                onClick={onToggleReleased}
                aria-expanded={showReleased}
                className="flex items-center gap-1 text-[12px] text-ink-3 hover:text-ink-2"
              >
                <Icon
                  name="chevron"
                  size={11}
                  className={cx("transition-transform duration-200", showReleased ? "rotate-180" : "")}
                />
                <span>{t("todos.released_section", { count: releasedAgents.length })}</span>
              </button>
              {showReleased && (
                <div className="mt-2 flex flex-col gap-1.5">
                  {releasedAgents.map((agent) => {
                    const mName = machines.find((m) => m.id === agent.machine_id)?.name || agent.machine_id;
                    const displaySub = `${mName} · ${shortPath(agent.cwd)}`;
                    return (
                      <div
                        key={`${agent.machine_id}/${agent.session_id}`}
                        className="flex items-center justify-between rounded-control border border-line/60 bg-hover/40 px-3 py-2 text-[12px] text-ink-3"
                      >
                        <div className="min-w-0 flex-1">
                          <span className="truncate font-medium text-ink-2">{agent.title || t("lib-format.untitled_session")}</span>
                          <div className="text-[11px] text-ink-3">{displaySub}</div>
                        </div>
                        <span className="shrink-0 text-[11px]">{t("todos.left_at", { time: ago(agent.released_at ?? undefined) })}</span>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          )}
        </div>
      </DetailSection>

      {/* 4. The repositories its agents bound, with their issues and PRs, and the projects it is linked to */}
      <DetailSection title={t("todos.detail_repositories")} testId="todo-section-repositories">
        <TodoRepositoryBindings key={`${owner}:${todo.manager ?? defaultManager()}:${todo.id}`} manager={todo.manager ?? defaultManager()} owner={owner} todoId={todo.id}
          legacyPRs={legacyPR ? [legacyPR] : []} version={todo.updated_at} />
        {spaces.length > 0 && (
          <div className="mt-4" data-testid="todo-spaces">
            <div className="mb-2 text-[11.5px] font-medium tracking-[0.04em] text-ink-3 uppercase">
              {t("todos.spaces_section", { count: spaces.length })}
            </div>
            <div className="flex flex-col gap-2">
              {spaces.map((sp) => {
                const mName = machines.find((m) => m.id === sp.machine_id)?.name || sp.machine_id;
                const live = hasLiveAgent(sp.machine_id, sp.space);
                return (
                  <div key={`${sp.machine_id}/${sp.space}`} className="flex min-w-0 items-center gap-2 rounded-control border border-line bg-canvas p-3">
                    <span className="truncate text-[13px] font-semibold text-ink">
                      {sp.space} · {mName}
                    </span>
                    {!live && (
                      <span className="shrink-0 rounded bg-hover px-1.5 py-0.5 text-[10.5px] text-ink-3">
                        {t("todos.space_no_agent")}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </DetailSection>

      {/* 5. The calls made on it */}
      <DecisionsSection todo={todo} lookup={lookup} />
    </div>
  );
}

// ── Supervision: a supervisor Agent and its workers ─────────

/** The workers a to-do's supervisor handed out, as a tree under its row (lib/todo-supervisor.ts): each worker to-do on
 *  a line with where it stands and the computer its Agent runs on, opening that to-do. The open ones are listed; the
 *  closed ones fold under Closed, as the Agents that left fold under Took part. */
function SupervisorWorkers({ workers, lookup, machines, onOpenTodo }: { workers: Todo[]; lookup: SessionLookup; machines: Machine[]; onOpenTodo: (id: string) => void }) {
  const [showClosed, setShowClosed] = useState(false);
  if (!workers.length) return null;
  const closed = workers.filter((w) => w.done_at);
  const line = (w: Todo) => {
    const agent = workerAgent(w);
    const session = agent && !agent.start_error ? lookup(agent.machine_id, agent.session_id) : undefined;
    const step = workerStep(w);
    const machine = agent && machines.find((m) => m.id === agent.machine_id)?.name;
    return (
      <li key={w.id}>
        <button
          type="button"
          data-open-todo={w.id}
          data-worker-step={step}
          onClick={() => onOpenTodo(w.id)}
          className="flex min-h-9 w-full min-w-0 items-start gap-2 rounded-control px-1.5 py-2 text-left hover:bg-hover focus-visible:outline-2 focus-visible:outline-accent [@media(pointer:coarse)]:min-h-11"
        >
          <span className="mt-px flex size-4 shrink-0 items-center justify-center">
            {w.done_at ? (
              <Icon name={step === "accepted" || step === "done" ? "check" : "x"} size={12} className="text-ink-3" />
            ) : (
              <StatusMark state={session ? foldedState(session) : "offline"} size="sm" label={false} />
            )}
          </span>
          {/* where it stands goes under the title when the line is too narrow for both */}
          <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
            <span data-testid="todo-worker-title" className={cx("max-w-full min-w-0 truncate text-meta", w.done_at ? "text-ink-3" : "text-ink")}>{w.title}</span>
            <span data-testid="todo-worker-state" className="text-label text-ink-3 sm:ml-auto">{[workerStepLabel(w), machine].filter(Boolean).join(" · ")}</span>
          </span>
        </button>
      </li>
    );
  };
  return (
    <div data-testid="todo-workers" className="mb-2 ml-3 min-w-0 basis-full border-l border-line pl-2">
      <ul>{workers.filter((w) => !w.done_at).map(line)}</ul>
      {closed.length > 0 && (
        <>
          <button
            type="button"
            onClick={() => setShowClosed((v) => !v)}
            aria-expanded={showClosed}
            className="flex min-h-8 items-center gap-1 px-1.5 text-label text-ink-3 hover:text-ink-2 [@media(pointer:coarse)]:min-h-11"
          >
            <Icon name="chevron" size={11} className={cx("transition-transform duration-200", showClosed ? "rotate-180" : "")} />
            <span>{t("todos.workers_closed", { count: closed.length })}</span>
          </button>
          {showClosed && <ul>{closed.map(line)}</ul>}
        </>
      )}
    </div>
  );
}

/**
 * Supervision: what a supervisor did with its workers, newest first, by the owner's local day (lib/todo-supervisor.ts):
 * the workers it handed out, its verdicts on them, their latest reports and how they closed; on the root also its report
 * to the main conversation and that conversation's verdicts. Today is open and earlier days are folded; an open day
 * shows its first lines with Show all, and the heading's fold-all button folds or opens every day, as Decisions do.
 */
function SupervisionSection({ todo, role, onOpenTodo }: { todo: Todo; role: SupervisionRole; onOpenTodo: (id: string) => void }) {
  const days = useMemo(() => supervisionDays(supervisionLog(todo, role)), [todo, role]);
  // a day the owner opened or folded, and one whose every line is shown; neither is kept past the page
  const [folds, setFolds] = useState<Record<string, boolean>>({});
  const [whole, setWhole] = useState<Record<string, boolean>>({});
  const isOpen = (d: SupervisionDay) => folds[d.key] ?? d.today;
  const anyOpen = days.some(isOpen);
  return (
    <DetailSection
      title={t("todos.detail_supervision")}
      aside={<SectionCount count={days.reduce((n, d) => n + d.entries.length, 0)} testId="todo-supervision-count" />}
      right={days.length > 0 && (
        <FoldAllButton
          label={anyOpen ? t("todos.decisions_fold") : t("todos.decisions_expand")}
          expand={!anyOpen}
          onClick={() => setFolds(Object.fromEntries(days.map((d) => [d.key, !anyOpen])))}
          className={SESSION_LIST_STYLE.foldButton}
          testId="todo-supervision-fold"
        />
      )}
      testId="todo-section-supervision"
    >
      {days.length === 0 ? (
        <SectionEmpty>{t("todos.supervision_empty")}</SectionEmpty>
      ) : (
        <ol className="mt-1 flex flex-col">
          {days.map((d) => {
            const open = isOpen(d);
            const all = whole[d.key] === true;
            return (
              <li key={d.key} data-supervision-day={d.key}>
                <button
                  type="button"
                  aria-expanded={open}
                  onClick={() => setFolds((f) => ({ ...f, [d.key]: !open }))}
                  className="flex min-h-8 items-center gap-1.5 text-label text-ink-3 hover:text-ink-2 [@media(pointer:coarse)]:min-h-11"
                >
                  <Icon name="chevron" size={11} className={cx("transition-transform duration-200", open ? "rotate-180" : "")} />
                  <span>{dayLabel(d.date)}</span>
                  <span className="tabular-nums">{d.entries.length}</span>
                </button>
                {open && (
                  <ul className="flex flex-col gap-2.5 rounded-control bg-inset p-3.5 shadow-hairline">
                    {(all ? d.entries : d.entries.slice(0, DECISIONS_FIRST)).map((e) => (
                      <SupervisionLine key={e.key} entry={e} page={todo.id} onOpenTodo={onOpenTodo} />
                    ))}
                  </ul>
                )}
                {open && d.entries.length > DECISIONS_FIRST && (
                  <Button tone="quiet" expanded={all} className="mt-1 whitespace-nowrap px-2 py-1 text-xs" onClick={() => setWhole((w) => ({ ...w, [d.key]: !all }))}>
                    {all ? t("todos.decisions_show_first", { count: DECISIONS_FIRST }) : t("todos.decisions_show_all", { count: d.entries.length })}
                  </Button>
                )}
              </li>
            );
          })}
        </ol>
      )}
    </DetailSection>
  );
}

const VERDICT_ICON: Record<TaskReview["verdict"], IconName> = { accepted: "check", rework: "retry", needs_user: "person", parked: "pause", cancelled: "x" };

function entryIcon(e: SupervisionEntry): IconName {
  switch (e.kind) {
    case "planned":
      return "plus";
    case "review":
      return VERDICT_ICON[e.review.verdict] ?? "info";
    case "report":
      return "chat";
    case "closed":
      return e.outcome === "done" ? "check" : e.outcome === "cancelled" ? "x" : "alertCircle";
    case "reported":
      return "send";
  }
}

/** A line of the Supervision log: what happened, the worker it is about (opening its to-do; not on that to-do's own
 *  page), the time of day, and what was said with it, clipped. */
function SupervisionLine({ entry: e, page, onOpenTodo }: { entry: SupervisionEntry; page: string; onOpenTodo: (id: string) => void }) {
  const { label, detail } = entryWords(e);
  return (
    <li className="flex min-w-0 items-start gap-2" data-supervision-entry={e.kind}>
      <Icon name={entryIcon(e)} size={13} className="mt-0.5 shrink-0 text-ink-3" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 items-baseline gap-2 text-meta">
          <span className="min-w-0 truncate select-text">
            <span className="text-ink">{label}</span>
            {e.todo.id !== page && (
              <>
                <span className="text-ink-3"> · </span>
                <button type="button" data-open-todo={e.todo.id} onClick={() => onOpenTodo(e.todo.id)} className="text-left text-accent-ink hover:underline">
                  {e.todo.title}
                </button>
              </>
            )}
          </span>
          <span className="ml-auto shrink-0 text-label text-ink-3 tabular-nums">{formatDate(e.at, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" })}</span>
        </div>
        {detail && <p className="line-clamp-2 text-label text-ink-2 select-text [overflow-wrap:anywhere]">{detail}</p>}
      </div>
    </li>
  );
}

// ── Decisions (#1618) ────────────────────────────────────────

/**
 * Decisions: the calls made on this todo, in effect first-to-last; "full history" shows the replaced ones struck
 * through. Refetched whenever the todo changes (the hub bumps it when a decision is written, also from an answered
 * card). A hub without decisions (404 on the route) has none to show.
 * Only the first five show at first, with Show all under them; the heading's fold-all button folds the list away and
 * opens it whole (#6447, `shownDecisions`). Nothing is stored: each to-do opens with the first five.
 */
function DecisionsSection({ todo, lookup }: { todo: Todo; lookup: ReturnType<typeof makeSessionLookup> }) {
  const [all, setAll] = useState<TodoDecision[] | null>(null);
  const [failed, setFailed] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const [view, setView] = useState<DecisionsView>("first");
  // Only the newest load lands: a slow earlier one must not put back an older list.
  const loads = useRef(0);

  const load = useCallback(async () => {
    const n = ++loads.current;
    try {
      const list = await fetchTodoDecisions(todo.id);
      if (n !== loads.current) return;
      setAll(list);
      setFailed("");
      setUnsupported(false);
    } catch (e) {
      if (n !== loads.current) return;
      if (isLegacySpacesRouteError(e)) setUnsupported(true); // a hub from before decisions: no route
      else setFailed(errorText(e));
    }
  }, [todo.id]);

  useEffect(() => {
    void load();
  }, [load, todo.updated_at]);

  useEffect(() => {
    setAll(null);
    setFailed("");
    setShowAll(false);
    setView("first");
  }, [todo.id]);

  const effective = (all ?? []).filter((d) => !d.superseded_by);
  const replaced = (all ?? []).length - effective.length;
  const listed = showAll ? (all ?? []) : effective;
  const shown = shownDecisions(listed, view);
  const folded = view === "folded" && listed.length > 0;
  const byId = new Map((all ?? []).map((d) => [d.id, d]));

  return (
    <DetailSection
      title={t("todos.detail_decisions")}
      aside={all !== null && <SectionCount count={effective.length} testId="todo-decisions-count" />}
      right={!unsupported && (
        <>
          {replaced > 0 && !folded && (
            <Button tone="quiet" className="whitespace-nowrap px-2 py-1 text-[12px]" onClick={() => setShowAll((v) => !v)}>
              {showAll ? t("todos.show_effective") : t("todos.show_history", { count: replaced })}
            </Button>
          )}
          {/* the Agents title row's fold-all button and meaning: Collapse all while any is shown, Expand all once folded */}
          {all !== null && listed.length > 0 && (
            <FoldAllButton
              label={folded ? t("todos.decisions_expand") : t("todos.decisions_fold")}
              expand={folded}
              onClick={() => setView(folded ? "all" : "folded")}
              className={SESSION_LIST_STYLE.foldButton}
              testId="todo-decisions-fold"
            />
          )}
        </>
      )}
      testId="todo-decisions"
    >
      {unsupported ? (
        <SectionEmpty>{t("todos.decisions_empty")}</SectionEmpty>
      ) : failed && all === null ? (
        <div className="mt-2 flex items-center justify-between gap-3 rounded-control border border-dashed border-line p-4 text-[12.5px] text-ink-3">
          <span className="min-w-0 truncate">{t("todos.decisions_failed", { error: failed })}</span>
          <Button tone="quiet" className="shrink-0 whitespace-nowrap px-2 py-1 text-[12px]" onClick={() => void load()}>
            {t("todos.retry")}
          </Button>
        </div>
      ) : all === null || folded ? null : shown.length === 0 ? (
        <SectionEmpty>{t("todos.decisions_empty")}</SectionEmpty>
      ) : (
        <ol className="mt-2 flex flex-col gap-2">
          {shown.map((d) => {
            const off = Boolean(d.superseded_by);
            const replaces = d.supersedes_id ? byId.get(d.supersedes_id) : undefined;
            return (
              <li
                key={d.id}
                className={cx("rounded-control border border-line p-3", off ? "bg-hover/40" : "bg-canvas")}
              >
                <div className={cx("text-[13.5px] font-semibold select-text", off ? "text-ink-3 line-through" : "text-ink")}>{d.question}</div>
                <div className={cx("mt-1 text-[13px] whitespace-pre-wrap select-text", off ? "text-ink-3 line-through" : "text-ink")}>
                  <span className="text-ink-3">{t("todos.chose")}</span>
                  {d.choice}
                </div>
                {d.reason && (
                  <div className={cx("mt-0.5 text-[12.5px] whitespace-pre-wrap text-ink-2 select-text", off && "line-through")}>
                    <span className="text-ink-3">{t("todos.reason")}</span>
                    {d.reason}
                  </div>
                )}
                <div className="mt-1.5 flex flex-wrap items-center gap-x-1.5 text-[11.5px] text-ink-3">
                  <span>{clock(d.phase_completed_at ?? d.created_at)}</span>
                  <span>·</span>
                  <span>{decisionSourceLabel(d, todo, lookup)}</span>
                  {off && (
                    <>
                      <span>·</span>
                      <span>{t("todos.superseded")}</span>
                    </>
                  )}
                  {!off && replaces && (
                    <>
                      <span>·</span>
                      <span className="truncate">{t("todos.supersedes", { choice: replaces.choice })}</span>
                    </>
                  )}
                </div>
              </li>
            );
          })}
        </ol>
      )}
      {!unsupported && !folded && listed.length > DECISIONS_FIRST && (
        <Button tone="quiet" testId="todo-decisions-more" expanded={view === "all"} className="mt-1.5 whitespace-nowrap px-2 py-1 text-xs" onClick={() => setView(view === "first" ? "all" : "first")}>
          {view === "first" ? t("todos.decisions_show_all", { count: listed.length }) : t("todos.decisions_show_first", { count: DECISIONS_FIRST })}
        </Button>
      )}
    </DetailSection>
  );
}

function lifecycleLabel(kind: string): string {
  switch (kind) {
    case "session.started": return t("todos.lifecycle_session_started");
    case "session.exited": return t("todos.lifecycle_session_exited");
    case "turn.started": return t("todos.lifecycle_turn_started");
    case "turn.finished": return t("todos.lifecycle_turn_finished");
    case "turn.failed": return t("todos.lifecycle_turn_failed");
    case "child.linked": return t("todos.lifecycle_child_linked");
    case "wait.opened": return t("todos.lifecycle_wait_opened");
    case "wait.resolved": return t("todos.lifecycle_wait_resolved");
    case "background.started": return t("todos.lifecycle_background_started");
    case "background.ended": return t("todos.lifecycle_background_ended");
    case "coverage.gap": return t("todos.lifecycle_gap");
    default: return t("todos.lifecycle_activity");
  }
}

function AgentLifecycleTimeline({ todo, lookup }: { todo: Todo; lookup: SessionLookup }) {
  const supported = useStore((s) => s.features.includes("agent_lifecycle_v1"));
  const [pages, setPages] = useState<AgentLifecyclePage[]>([]);
  const [failure, setFailure] = useState(false);
  const [missing, setMissing] = useState(false);
  const [pending, setPending] = useState(false);
  const [request, setRequest] = useState<{ before?: string }>({});
  useEffect(() => {
    if (!supported) return;
    const controller = new AbortController();
    setPending(true);
    setFailure(false);
    void fetchAgentLifecycle(todo.id, todo.manager, request.before, controller.signal).then((page) => {
      if (!controller.signal.aborted) setPages((old) => request.before ? [...old, page] : [page]);
    }).catch((err: unknown) => {
      if (controller.signal.aborted) return;
      if (err instanceof ApiError && err.status === 404) setMissing(true);
      else setFailure(true);
    }).finally(() => {
      if (!controller.signal.aborted) setPending(false);
    });
    return () => controller.abort();
  }, [supported, todo.id, todo.manager, request]);
  if (!supported || missing) return null;
  const events = pages.flatMap((p) => p.events).sort((a, b) => Date.parse(b.occurred_at) - Date.parse(a.occurred_at));
  const next = pages.at(-1)?.next;
  return (
    <DetailSection title={t("todos.lifecycle_title")} testId="todo-section-lifecycle">
      <ol className="flex flex-col gap-2">
        {events.map((e) => {
          const actor = lookup(e.machine_id, e.session_id)?.title ||
            todo.agents?.find((agent) => agent.machine_id === e.machine_id && agent.session_id === e.session_id)?.title;
          return (
          <li key={e.revision} className="flex flex-wrap items-baseline gap-x-2 text-label text-ink-2">
            <span>{lifecycleLabel(e.kind)}</span>
            {actor && <span className="text-ink-3">{actor}</span>}
            <time dateTime={e.occurred_at} className="text-ink-3">{ago(e.occurred_at)}</time>
            {e.confidence === "observed" && e.kind !== "coverage.gap" && (
              <span className="text-ink-3">{t("todos.lifecycle_observed")}</span>
            )}
          </li>
          );
        })}
      </ol>
      {pending && <SectionEmpty>{t("todos.lifecycle_loading")}</SectionEmpty>}
      {!pending && failure && (
        <Button tone="quiet" onClick={() => setRequest({ ...request })}>{t("todos.lifecycle_retry")}</Button>
      )}
      {!pending && !failure && events.length === 0 && <SectionEmpty>{t("todos.lifecycle_empty")}</SectionEmpty>}
      {!pending && !failure && next && (
        <Button tone="quiet" onClick={() => setRequest({ before: next })}>{t("todos.lifecycle_more")}</Button>
      )}
    </DetailSection>
  );
}
