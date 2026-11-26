// The session overview's scrollable contents, loaded when the user turns it on.
import "./SessionOverview.css";
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { t } from "../../i18n";
import { agentId, agentName } from "../../lib/agent-kind";
import { credentialOwner } from "../../lib/api";
import { contextLabel, contextTier, contextTooltip, contextUsage } from "../../lib/context-usage";
import { finishedBackgroundTasks, type FinishedTask } from "../../lib/finished-tasks";
import { baseName, sessionTitle, shortPath, stateOf } from "../../lib/format";
import { modelName } from "../../lib/launcher-models";
import { sessionLauncher } from "../../lib/model-switch";
import { openPanel } from "../../lib/panel";
import { launcherTitle } from "../../lib/provider-names";
import { effortTitle } from "../../lib/session-actions";
import { aggregateSessionTokens, normalizeContextBreakdown } from "../../lib/session-insights";
import { currentModelRow, machineLauncherList, modelLabel, modelList, sessionModelHost, useModelLists } from "../../lib/session-models";
import { getSession, useStore } from "../../lib/store";
import { followChildSession, followSubagentLink } from "../../lib/subagent-link";
import { subagentTitle, taskLabel, taskPill, type TaskIndex } from "../../lib/tasks";
import { formatTokens } from "../../lib/tokens";
import type { Item, Session, Subagent } from "../../lib/types";
import { cx } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { ProviderMark } from "../../ui/model-panel/ProviderMark";
import { StatusMark } from "../../ui/StatusMark";
import { BackgroundList, backgroundSections } from "./BackgroundPanel";
import { SessionInfoDialog } from "./SessionDialogs";
import { BreakdownCard, StackedBreakdownBar } from "./SessionInsightsPopover";

interface OverviewProps {
  session: Session;
  machineName: string;
  items: Item[];
  tasks: TaskIndex;
  id: string;
  children: readonly Session[];
  onJumpToCall?: (call: string) => void;
  hidden?: boolean;
}

export function SessionOverview({ id, session, machineName, items, tasks, children, onJumpToCall, hidden }: OverviewProps) {
  const [infoOpen, setInfoOpen] = useState(false);
  const finished = useMemo(() => finishedBackgroundTasks(items, tasks), [items, tasks]);
  const outputs = useMemo(() => {
    const paths = (session.background ?? []).map((task) => task.output_file);
    for (const { task } of finished) paths.push(task.output_file);
    return [...new Set(paths.filter((path): path is string => Boolean(path)))];
  }, [finished, session.background]);
  // Codex child threads are standalone sessions. Use the known rows from this machine and manager.
  const listed = useStore((s) => (s.sessions[session.machine_id] ?? []).filter((row) =>
    row.session_id !== session.session_id && row.manager === session.manager && (
      row.parent_session_id === session.session_id || row.root_session_id === session.session_id ||
      (!row.parent_session_id && session.source_thread_id && row.parent_thread_id === session.source_thread_id)
    ),
  ));
  const agents = useMemo(() => {
    const embedded = session.subagents ?? [];
    const ids = new Set(embedded.map((a) => a.session_id));
    const rows = new Map(listed.map((child) => [child.session_id, child]));
    for (const child of children) {
      if (!child.folded_as && (child.is_subagent || child.parent_session_id || child.parent_thread_id)) rows.set(child.session_id, child);
    }
    const threads = [...rows.values()].filter((child) => !ids.has(child.session_id)).map((child): Subagent => ({
      id: child.session_id,
      session_id: child.session_id,
      parent_id: child.parent_session_id,
      description: sessionTitle(child),
      running: stateOf(child) === "working" || stateOf(child) === "needs_you" || Boolean(child.background_working),
      started_at: child.started_at ?? child.updated_at,
    }));
    return [...embedded, ...threads];
  }, [children, listed, session.subagents]);
  const running = agents.filter((a) => a.running).length;
  const hasBgContent = useMemo(() => {
    const sections = backgroundSections(session, Date.now());
    const subagentIds = new Set(session.subagents?.map((a) => a.id));
    const bgTasks = sections.tasks.filter((task) => task.kind !== "agent" || !subagentIds.has(task.id));
    const active = new Set(session.background?.map((task) => task.id));
    const ended = finished.filter((task) => !active.has(task.id));
    return Boolean(sections.loop || sections.running.length || bgTasks.length || sections.completed.length || sections.completedTasks.length || ended.length);
  }, [finished, session]);
  const mid = session.machine_id;
  const sid = session.session_id;
  const owner = credentialOwner();
  const current = () => {
    const live = getSession(mid, sid);
    return owner === credentialOwner() && live && live.manager === session.manager;
  };

  return (
    <aside id={id} data-session-overview aria-labelledby={`${id}-title`} hidden={hidden}
      className="session-overview flex min-h-0 shrink-0 flex-col bg-surface">
      <header className="flex h-11 shrink-0 items-center justify-between gap-2 border-b border-line/80 px-4">
        <h2 id={`${id}-title`} className="text-meta font-semibold tracking-[-0.01em] text-ink">{t("chat-chatview.session_overview")}</h2>
        <button
          type="button"
          onClick={() => setInfoOpen(true)}
          className="inline-flex items-center gap-1 rounded-control px-2 py-1 text-submeta font-medium text-ink-3 transition-colors hover:bg-hover hover:text-ink"
        >
          <Icon name="info" size={13} />
          <span>{t("chat-sessioninfo.info_title")}</span>
        </button>
      </header>
      <div className="min-h-0 flex-1 divide-y divide-line/70 overflow-y-auto overscroll-contain">
        <RuntimeAndEnvironment session={session} machineName={machineName} />
        <OverviewContext session={session} items={items} />
        {agents.length > 0 && <Subagents agents={agents} running={running} open={(agent) => {
          if (!current()) return;
          if (session.subagents?.some((entry) => entry.session_id === agent.session_id)) {
            followSubagentLink(mid, `${sid}@${agent.id}`);
          } else {
            followChildSession(mid, agent.session_id, sid);
          }
        }} />}
        {hasBgContent && (
          <div className="px-2 py-1">
            <BackgroundList session={session} mid={mid} sid={sid} inOverview
              onOpenRun={(task) => { if (current()) openPanel({ kind: "background", mid, sid, run: task, from: "overview" }); }}
              onOpenTask={(task) => { if (current()) openPanel({ kind: "background", mid, sid, task: task.id, from: "overview" }); }}
            />
            <FinishedTasks finished={finished} session={session} onJumpToCall={onJumpToCall} />
          </div>
        )}
        {outputs.length > 0 && <Outputs paths={outputs}
          onOpen={(path) => { if (current()) openPanel({ kind: "file", mid, path, cwd: session.cwd }); }} />}
      </div>
      <SessionInfoDialog session={session} machineName={machineName} open={infoOpen} onClose={() => setInfoOpen(false)} />
    </aside>
  );
}

function RuntimeAndEnvironment({ session, machineName }: Pick<OverviewProps, "session" | "machineName">) {
  useModelLists();
  const launchers = machineLauncherList(session.machine_id);
  const engine = agentId(session.agent);
  const own = sessionLauncher(session);
  const ownList = modelList(session.machine_id, own, undefined, sessionModelHost(session)).list;
  const runtimeLabel = session.launcher ? launcherTitle(session.launcher, launchers) : agentName(engine, session.agent_label);
  const matchedModel = session.model ? currentModelRow(ownList?.models ?? [], session.model) : undefined;
  const rawModelText = matchedModel ? modelName(matchedModel) : session.model ? modelLabel(session.model) : "";
  const effortText = session.effort ? effortTitle(session.effort) : "";
  const modelText = effortText ? rawModelText.replace(/\s*\((?:low|medium|high|xhigh|max)\)\s*$/i, "").trim() || rawModelText : rawModelText;
  const worktree = Boolean(session.worktree && session.repo_root && session.worktree !== session.repo_root);
  const worktreePath = worktree && (session.worktree !== session.cwd || !session.branch);
  const rows: { label: string; icon: IconName; value: string | undefined; badge?: string; mono?: boolean; full?: string }[] = [
    { label: t("chat-sessioninfo.machine"), icon: "monitor", value: machineName },
    { label: t("chat-sessiondialogs.folder"), icon: "folder", value: shortPath(session.cwd), mono: true, full: session.cwd },
    { label: t("chat-sessioninfo.branch"), icon: "git", value: session.branch, mono: true, badge: worktree && !worktreePath ? t("chat-sessionoverview.worktree") : undefined },
  ];
  if (worktreePath) {
    rows.push({ label: t("chat-sessionoverview.worktree"), icon: "folder", value: shortPath(session.worktree), mono: true, full: session.worktree });
  }
  const visibleRows = rows.filter((row) => row.value);

  return (
    <section className="flex flex-col gap-3 px-4 py-3.5">
      {(runtimeLabel || modelText || effortText) && (
        <div data-overview-runtime className="flex flex-col gap-1.5 rounded-lg bg-inset/60 px-3 py-2.5 ring-1 ring-inset ring-line/60">
          <div className="flex items-center gap-2">
            <ProviderMark engine={engine} label={session.agent_label} size={15} className="shrink-0 text-ink-2" />
            <span className="min-w-0 flex-1 truncate text-submeta font-medium text-ink-2">{runtimeLabel}</span>
            {effortText && (
              <span
                data-overview-effort={session.effort}
                className="shrink-0 rounded-chip bg-hover px-1.5 py-0.5 text-submeta font-medium text-ink-2"
              >
                {effortText}
              </span>
            )}
          </div>
          {modelText && (
            <div className="truncate text-label font-medium text-ink" title={session.model}>
              {modelText}
            </div>
          )}
        </div>
      )}
      {visibleRows.length > 0 && (
        <div className="flex flex-col gap-2">
          <h3 className="text-submeta font-medium text-ink-3">{t("chat-sessionoverview.environment")}</h3>
          <dl className="flex flex-col gap-2">
            {visibleRows.map(({ label, icon, value, badge, mono, full }) => (
              <div key={label} className="flex items-baseline justify-between gap-3 text-label">
                <dt className="flex shrink-0 items-center gap-1.5 text-ink-3">
                  <Icon name={icon} size={13} className="shrink-0 text-ink-3/80" />
                  <span>{label}</span>
                </dt>
                <dd className="flex min-w-0 items-baseline justify-end gap-1.5">
                  <span title={full || value} className={cx("min-w-0 truncate text-right text-ink", mono && "font-mono text-submeta")}>
                    {value}
                  </span>
                  {badge && (
                    <span className="shrink-0 rounded-chip bg-hover px-1.5 py-0.5 text-submeta text-ink-3">{badge}</span>
                  )}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      )}
    </section>
  );
}

function OverviewContext({ session, items }: { session: Session; items: readonly Item[] }) {
  const usage = contextUsage(session);
  const breakdown = normalizeContextBreakdown(session.context_breakdown, usage?.window ?? session.context_window);
  const contextTokens = session.context_tokens && session.context_tokens > 0 ? session.context_tokens : 0;
  const itemTokens = useMemo(() => (breakdown || usage ? null : aggregateSessionTokens(items)), [breakdown, items, usage]);

  if (!breakdown && !usage && !contextTokens && !itemTokens) return null;

  const nearFull = usage ? contextTier(usage) !== "ok" : false;
  return (
    <section className="flex flex-col gap-2.5 px-4 py-3.5" data-overview-context>
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-submeta font-medium text-ink-3">{t("chat-sessioninfo.context")}</h3>
        {usage ? (
          <div className="flex items-baseline gap-1.5 tabular-nums">
            <span className={cx("text-label font-semibold", nearFull ? "text-orange" : "text-ink")}>{usage.used}%</span>
            <span className="text-submeta text-ink-3">
              {t("context.tokens", { used: formatTokens(usage.tokens), window: formatTokens(usage.window) })}
            </span>
          </div>
        ) : contextTokens > 0 ? (
          <span className="text-label font-medium tabular-nums text-ink">
            {t("chat-sessioninfo.tokens", { value: formatTokens(contextTokens) })}
          </span>
        ) : itemTokens ? (
          <span className="text-label font-medium tabular-nums text-ink">
            {t("chat-sessioninfo.tokens", { value: formatTokens(itemTokens.total) })}
          </span>
        ) : null}
      </div>

      {breakdown ? (
        <>
          <StackedBreakdownBar breakdown={breakdown} />
          <BreakdownCard breakdown={breakdown} />
        </>
      ) : usage ? (
        <div
          role="progressbar"
          aria-label={contextTooltip(usage)}
          aria-valuenow={usage.used}
          aria-valuemin={0}
          aria-valuemax={100}
          className="h-1.5 overflow-hidden rounded-full bg-line"
        >
          <div
            className={cx("h-full rounded-full", nearFull ? "bg-orange" : "bg-accent")}
            style={{ width: `${usage.used}%` }}
          />
          <span className="sr-only">{contextLabel(usage)}</span>
        </div>
      ) : null}

      {!breakdown && !usage && itemTokens && itemTokens.output > 0 && (
        <div className="flex items-baseline justify-between gap-2 text-label tabular-nums">
          <span className="text-ink-3">{t("chat-sessioninfo.output")}</span>
          <span className="font-medium text-ink">{t("chat-sessioninfo.tokens", { value: formatTokens(itemTokens.output) })}</span>
        </div>
      )}
    </section>
  );
}

function Subagents({ agents, running, open }: { agents: Subagent[]; running: number; open: (agent: Subagent) => void }) {
  const [expanded, setExpanded] = useState(false);
  const ended = agents.length - running;
  return (
    <section className="px-2 py-2">
      <h3>
        <button type="button" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}
          className="flex min-h-8 w-full items-center gap-2 rounded-lg px-2 py-1 text-left text-label transition-colors hover:bg-hover">
          <Icon name="agent" size={14} className="shrink-0 text-ink-3" />
          <span className="flex-1 font-medium text-ink-2">{t("chat-sessionoverview.subagents")}</span>
          {running > 0 && (
            <span className="rounded-chip bg-hover px-1.5 py-0.5 text-submeta font-medium whitespace-nowrap text-accent-ink tabular-nums">
              {t("chat-sessionoverview.running_count", { count: running })}
            </span>
          )}
          {ended > 0 && (
            <span className="text-submeta whitespace-nowrap text-ink-3 tabular-nums">
              {t("chat-sessionoverview.ended_count", { count: ended })}
            </span>
          )}
          <Icon name="chevron" size={12} className={cx("shrink-0 text-ink-3 transition-transform duration-150", expanded && "rotate-180")} />
        </button>
      </h3>
      {expanded && (
        <ul className="mt-1 flex flex-col gap-0.5">
          {subagentTree(agents).map(({ agent, depth }) => {
            const doing = agent.running ? agent.last_tool?.summary || agent.last_tool?.name : undefined;
            return (
              <li key={agent.id} style={{ paddingInlineStart: `calc(var(--spacing) * ${Math.min(depth, 4) * 3})` }}>
                <button type="button" onClick={() => open(agent)}
                  className="flex min-h-8 w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-label transition-colors hover:bg-hover">
                  {agent.running ? <StatusMark state="working" size="sm" /> : <Icon name="check" size={12} className="shrink-0 text-ink-3" />}
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="truncate text-ink" title={subagentTitle(agent)}>{subagentTitle(agent)}</span>
                    {doing && <span className="truncate text-submeta text-ink-3">{doing}</span>}
                  </span>
                  <span className="shrink-0 text-submeta whitespace-nowrap text-ink-3">
                    {agent.running ? t("chat-subagentcard.status_running") : t("chat-sessionoverview.ended")}
                  </span>
                  <Icon name="chevronRight" size={12} className="shrink-0 text-ink-3/70" />
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

function Outputs({ paths, onOpen }: { paths: readonly string[]; onOpen: (path: string) => void }) {
  return (
    <section className="px-4 py-3.5">
      <h3 className="text-submeta font-medium text-ink-3">{t("chat-sessionoverview.outputs")}</h3>
      <ul className="mt-1.5 -mx-2 flex flex-col gap-0.5">
        {paths.map((path) => (
          <li key={path}>
            <button type="button" onClick={() => onOpen(path)} title={path}
              className="flex min-h-8 w-full min-w-0 items-center gap-2 rounded-lg px-2 text-left text-label transition-colors hover:bg-hover">
              <Icon name="read" size={13} className="shrink-0 text-ink-3" />
              <span className="min-w-0 flex-1 truncate font-mono text-submeta text-ink">{baseName(path) || shortPath(path)}</span>
              <Icon name="chevronRight" size={12} className="shrink-0 text-ink-3/70" />
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function FinishedTasks({ finished, session, onJumpToCall }: Pick<OverviewProps, "session" | "onJumpToCall"> & {
  finished: readonly FinishedTask[];
}) {
  const [open, setOpen] = useState(false);
  const list = useRef<HTMLUListElement>(null);
  const active = new Set(session.background?.map((task) => task.id));
  const ended = finished.filter((task) => !active.has(task.id));
  useLayoutEffect(() => {
    if (open) list.current?.firstElementChild?.scrollIntoView({ block: "nearest" });
  }, [open]);
  if (!ended.length) return null;
  return (
    <section className="py-1">
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}
        className="flex min-h-8 w-full items-center gap-2 rounded-lg px-2 text-left text-label text-ink-2 transition-colors hover:bg-hover">
        <Icon name="check" size={13} className="text-green" />
        <span className="flex-1">{t("chat-sessionoverview.finished_count", { count: ended.length })}</span>
        <Icon name="chevron" size={12} className={cx("text-ink-3 transition-transform duration-150", open && "rotate-180")} />
      </button>
      {open && (
        <ul ref={list} className="mt-1 divide-y divide-line/60 rounded-lg bg-inset/60 ring-1 ring-inset ring-line/60">
          {ended.map(({ id, task, title, call, at, startedAt }) => {
            const pill = taskPill(task, startedAt, at);
            return (
              <li key={id} className="flex min-w-0 flex-col gap-1 px-3 py-2 text-label">
                <div className="flex items-start justify-between gap-2">
                  <span className="min-w-0 break-words text-ink">{title || taskLabel(task)}</span>
                  <span className={cx("shrink-0 text-submeta whitespace-nowrap tabular-nums", pill?.tone === "red" ? "text-red" : "text-ink-3")}>{pill?.text}</span>
                </div>
                {onJumpToCall && call && (
                  <div className="text-submeta text-accent-ink">
                    <button type="button" onClick={() => onJumpToCall(call)} className="hover:underline">
                      {t("chat-sessionoverview.show_in_chat")}
                    </button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

interface SubagentRow {
  agent: Subagent;
  /** 0: the session launched it (or its launcher is not listed); 1: a subagent did; … */
  depth: number;
}

/** Every subagent by who launched whom (`parent_id`), flattened for a list: each right under its launcher, one level
 *  deeper; one whose launcher is not listed (it ended before the node's one-day cutoff) at the top level. Within a
 *  level running ones first, then the newest first (as the node lists them). */
function subagentTree(all: Subagent[] | undefined): SubagentRow[] {
  const list = all ?? [];
  const listed = new Set(list.map((a) => a.id));
  const newest = (a: Subagent) => Date.parse(a.started_at ?? "") || 0;
  const order = (a: Subagent, b: Subagent) => Number(Boolean(b.running)) - Number(Boolean(a.running)) || newest(b) - newest(a);
  const kids = new Map<string, Subagent[]>();
  for (const a of [...list].sort(order)) {
    const p = a.parent_id && a.parent_id !== a.id && listed.has(a.parent_id) ? a.parent_id : "";
    kids.set(p, [...(kids.get(p) ?? []), a]);
  }
  const out: SubagentRow[] = [];
  const placed = new Set<string>();
  const place = (a: Subagent, depth: number) => {
    if (placed.has(a.id)) return;
    placed.add(a.id);
    out.push({ agent: a, depth });
    for (const k of kids.get(a.id) ?? []) place(k, depth + 1);
  };
  for (const a of kids.get("") ?? []) place(a, 0);
  // launchers naming each other (only a runtime could write that) are reached from nowhere: list them at the top
  for (const a of [...list].sort(order)) place(a, 0);
  return out;
}
