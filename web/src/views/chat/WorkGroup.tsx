// A stretch of an exchange's work: one quiet collapsed row that says in words what was done, opening by hand to its
// steps on one rail (#2602). The tail stretch of a live exchange is the exchange's one live indicator: spinner,
// "Working…" and the clock of the current run of work.
import { lazy, memo, Suspense, useContext, useEffect, useLayoutEffect, useMemo, useState } from "react";
import { childSessionOf, isChildSessionStep } from "../../lib/child-cards";
import { useSessionLive } from "../../lib/live";
import { useStore } from "../../lib/store";
import type { Item } from "../../lib/types";
import { groupOpens, groupTitle, workLineOpen, isAgentStep, isPlanStep, isRowStep, isWorkflowStep, planTodos, workDuration, type AgentStatus, type ToolStep, type WorkGroupNode, type WorkNode } from "../../lib/work-groups";
import type { WorkflowCalls } from "../../lib/workflows";
import type { Step } from "../../lib/turns";
import { Collapse, cx, Spinner, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ActionRow } from "./ActionRow";
import { LiveClockContext } from "./chat-context";
import { inOpenedFold, openFoldRange, useExpanded, useLiveOverride } from "./expanded";
import { useReveal } from "./search-reveal";
import { stepWeight, useStepBudget, withinBudget } from "./work-budget";
import { nodeItemIds } from "../../lib/chat-search";
import { Answer } from "./SegmentView";
import { NoticeRow, Thumb } from "./Steps";
import { WorkflowCallCard } from "./WorkflowCard";
import { LiveThinking as LiveThinkingLine } from "./ThinkingCard";
import { t } from "../../i18n";

// Subagent cards sit inside the folded work (super rule 2), drawn only once a reader opens it: off the first screen.
const SubagentCard = lazy(() => import("./SubagentCard").then((m) => ({ default: m.SubagentCard })));
// the plan's card loads with an opened summary, off the first screen (#5156)
const PlanCard = lazy(() => import("./PlanCard").then((m) => ({ default: m.PlanCard })));
// Thought rows too (#5155), with their emphasis parser: only an opened summary draws them.
const ThinkingRow = lazy(() => import("./ThinkingRow").then((m) => ({ default: m.ThinkingRow })));
// A child session's card (#5120) the same way: drawn only once the work summary it sits in is opened.
const ChildSessionCard = lazy(() => import("./ChildSessionCard").then((m) => ({ default: m.ChildSessionCard })));
// A Codex visualization fragment (#10321) the same way: its runtime is a lazy chunk off the first screen.
const VisualizationCard = lazy(() => import("./VisualizationCard"));

export interface WorkProps {
  mid: string;
  sid: string;
  /** Keys the reader's open / closed state (expanded.ts) to this chat. */
  scope: string;
  expandFold: (f: Item) => Promise<boolean | void>;
  agentStatus: (s: ToolStep) => AgentStatus;
  /** The Workflow calls whose run the hub lists (#3434): those draw the run's card. */
  workflowCalls?: WorkflowCalls;
  /** Inside an open stretch. */
  nested?: boolean;
  /** Inside an open stretch: whether it is the live one, whose edits show inline. */
  rowsLive?: boolean;
  /** The exchange these steps are of (its segment's id): what of it the reader opened is kept under it. */
  segment?: string;
  /** Show every tool's original input/result in place in an opened supervision trace. */
  trace?: boolean;
  /** The exchange these steps are of is still running. */
  turnLive?: boolean;
  /** The step that draws the exchange's plan card (work-groups.ts planCardStep; "" for none): its other plan updates are
   *  rows. Unset: every plan update draws a card. */
  plan?: string;
}

/** A Workflow call that draws its run's card (the hub lists the run); one that does not is the row it always was. */
const workflowCard = (s: ToolStep, p: WorkProps) => isWorkflowStep(s) && Boolean(s.use.tool_use_id && p.workflowCalls?.known.has(s.use.tool_use_id));

/** A step drawn as an action row, sitting with the rows next to it. */
const drawnAsRow = (s: Step, p: WorkProps): s is ToolStep =>
  (p.trace && s.kind === "tool") || isRowStep(s) || (isWorkflowStep(s) && !workflowCard(s, p)) || (isPlanStep(s) && p.plan !== undefined && s.id !== p.plan);

/** Every fold under a group, nested ones too: opening it fetches their steps. */
function foldsOf(g: WorkGroupNode): Item[] {
  return g.children.flatMap((c) => (c.kind === "group" ? foldsOf(c) : c.step.kind === "fold" ? [c.step.item] : []));
}

/** The screen's status line while this exchange is live (contract §3): a thinking line at the live group's end. Read
 *  here, not handed down: it changes about once a second while the agent works, and only this line shows it (#5417). */
function useThinkingNow(mid: string, sid: string) {
  const status = useSessionLive(mid, sid);
  return status?.thinking ? <LiveThinkingLine /> : null;
}

function LiveThinking({ mid, sid }: { mid: string; sid: string }) {
  return useThinkingNow(mid, sid);
}

/** `limit`: inside an open group whose budget (work-budget.ts) does not reach all of this one, how many of its steps it
 *  draws for now. */
export const WorkGroup = memo(function WorkGroup({ g, p, limit }: { g: WorkGroupNode; p: WorkProps; limit?: number }) {
  const turnLive = g.live && !p.nested;
  const key = `${p.scope}${g.id}`;
  // a stretch inside a fold the reader opened starts open once its steps are in; one nested in an open one is open
  const [remembered, setRemembered] = useExpanded(key, p.nested ? true : inOpenedFold(p.scope, g.seq));
  // a choice made while it runs lasts while it runs; once done it is closed again unless the reader opens it
  const [liveOverride, setLiveOverride] = useLiveOverride(key, turnLive);
  const setOpen = turnLive ? setLiveOverride : setRemembered;
  const heading = useMemo(() => groupTitle(g), [g]);
  const [loading, setLoading] = useState(false);
  const opens = groupOpens(g);
  const open = workLineOpen({ turnLive, remembered, liveOverride }) && opens;
  const inside = useMemo(() => ({ ...p, nested: true, rowsLive: g.live }), [p, g.live]);
  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next) for (const f of foldsOf(g)) if (f.fold) openFoldRange(p.scope, f.seq, f.fold.through_seq);
  };
  // a long trace is drawn a screenful at a time (work-budget.ts): the outermost group keeps the budget, nested ones draw
  // what it leaves them; a jump to a call in it (jump.ts) draws it whole
  const budget = useStepBudget(p.nested ? undefined : key, open && !p.nested, stepWeight(g) - 1, g.live);
  const drawn = p.nested ? limit : budget.drawn;
  // a search hit inside it (search-reveal.ts): it opens, with every step drawn so the hit's row is there to land on,
  // and "Back to where you were" closes it again
  const ids = useMemo(() => nodeItemIds(g.children), [g.children]);
  useReveal(p.scope, ids, () => {
    budget.showAll();
    if (open || !opens) return;
    toggle();
    return () => setOpen(false);
  });
  // an open group's folded steps are fetched: opened just now, or remembered open when the chat comes back (#1996: it
  // stayed at "Loading N steps" then); once in, the folds are gone and this has nothing left to do.
  const folds = useMemo(() => (open && !p.nested ? foldsOf(g) : []), [open, p.nested, g]);
  const foldKey = folds.map((f) => f.id).join(" ");
  const expandFold = p.expandFold;
  // a fetch that failed (the hub restarting, the network): the group says so, and "Retry" runs it again
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    // every fold is in: done. A run cut short by the last fold coming in (its key changed) does not get to say so, and
    // the header's spinner kept spinning (#1614)
    if (!foldKey) return setLoading(false);
    let live = true;
    setLoading(true);
    setFailed(false);
    folds
      // each fold that comes in runs this again with the rest: this run stops there
      .reduce<Promise<boolean | void>>((done, f) => done.then(() => (live ? expandFold(f) : undefined)), Promise.resolve())
      .catch(() => live && setFailed(true))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
    // the folds by their ids: a new `g` with the same folds is not a new fetch
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [foldKey, expandFold, attempt]);
  return (
    <div className="flex w-full min-w-0 flex-col" data-work-group={g.live ? "live" : "done"} data-group-id={g.id}>
      <button
        type="button"
        onClick={opens ? toggle : undefined}
        disabled={!opens}
        aria-expanded={opens ? open : undefined}
        title={heading}
        // on a touch screen 40 tall, the margin taking back what it adds, so nothing around it moves
        className={cx(
          "group inline-flex max-w-full self-start items-center gap-1.5 rounded-[6px] text-left select-none [@media(pointer:coarse)]:-my-[7.5px] [@media(pointer:coarse)]:min-h-10",
          p.nested ? "px-3.5 py-2" : g.live ? "pt-1 pb-1.5" : "py-1",
          g.live && "work-live-head",
          opens
            ? "cursor-pointer text-tool-ink transition-colors duration-150 ease-(--ease-out-strong) hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent motion-reduce:transition-none"
            : "cursor-default text-tool-ink",
        )}
      >
        {g.live && (
          <span className="inline-flex h-[1lh] shrink-0 items-center text-accent">
            <Spinner size={13} className="text-accent" />
          </span>
        )}
        <span className={cx("min-w-0 truncate text-[length:var(--chat-secondary)]", g.live ? "text-ink/90" : "text-tool-ink", opens && "transition-colors duration-150 ease-(--ease-out-strong) group-hover:text-ink motion-reduce:transition-none")}>{heading}</span>
        {loading && <Spinner size={11} className="text-tool-ink" />}
        {g.live && g.startedAt !== undefined && <Elapsed since={g.startedAt} />}
        {opens && <Icon name="chevronRight" size={13} stroke={2.1} className="shrink-0 text-tool-ink/80 transition-[transform,color] duration-200 ease-(--ease-out-strong) group-hover:text-ink motion-reduce:transition-none" style={{ transform: open ? "rotate(90deg)" : "none" }} />}
      </button>
      {open && failed && !loading && (
        <div className="mt-1 flex items-center gap-2 pl-0.5 text-[12px] text-ink-3" role="status">
          {t("chat-workgroup.load_failed")}
          <button type="button" onClick={() => setAttempt((n) => n + 1)} className="touch-hit rounded-control px-1.5 py-0.5 font-medium whitespace-nowrap text-accent-ink transition-colors duration-150 hover:text-ink">{t("chat-workgroup.retry")}</button>
        </div>
      )}
      <Collapse open={open} lazy className="motion-safe:duration-[240ms]">
        <div
          className={cx(
            "flex min-w-0 flex-col divide-y divide-line/55",
            p.nested ? "pl-3" : "mt-1.5 overflow-hidden rounded-[12px] border border-tool-rail bg-surface/40",
          )}
          data-work-detail
        >
          {/* a call without its result spins while the exchange runs, in a finished stretch too (it may still be going) */}
          <WorkNodes nodes={g.children} p={inside} live={p.turnLive ?? g.live} limit={drawn} />
          {g.live && <LiveThinking mid={p.mid} sid={p.sid} />}
        </div>
      </Collapse>
    </div>
  );
});

/** A stretch's steps in order; tool calls next to each other sit together without a gap, like cindy's tool segments;
 *  each subagent is a card of its own, in the order it was launched (#5030). `limit`: only the first that many steps
 *  for now (an open group's budget, work-budget.ts); absent, all of them. */
function WorkNodes({ nodes, p, live, limit }: { nodes: WorkNode[]; p: WorkProps; live?: boolean; limit?: number }) {
  const { shown, limits } = withinBudget(nodes, limit);
  const runs: (WorkNode | ToolStep[])[] = [];
  for (const n of shown) {
    const last = runs[runs.length - 1];
    if (n.kind === "step" && drawnAsRow(n.step, p)) {
      if (Array.isArray(last)) last.push(n.step);
      else runs.push([n.step]);
    } else runs.push(n);
  }
  let stepIndex = 0;
  return (
    <>
      {runs.map((run) => {
        const startIdx = stepIndex;
        if (Array.isArray(run)) {
          stepIndex += run.length;
          return (
            <div key={run[0].id} className="flex min-w-0 flex-col divide-y divide-line/55">
              {run.map((s, i) => (
                <ActionRow
                  key={s.id}
                  step={s}
                  running={live && !s.result}
                  mid={p.mid}
                  sid={p.sid}
                  trace={p.trace}
                  inlineDiff={Boolean(p.nested && p.rowsLive)}
                  enterIndex={startIdx + i}
                />
              ))}
            </div>
          );
        }
        stepIndex += 1;
        if (run.kind === "group") {
          return <WorkGroup key={run.id} g={run} p={p} limit={limits?.get(run)} />;
        }
        return (
          <div key={run.step.id} className="min-w-0 work-step-enter" style={{ animationDelay: `${Math.min(startIdx, 6) * 24}ms` }}>
            <StepView step={run.step} p={p} live={live} />
          </div>
        );
      })}
    </>
  );
}

/** A step on its own: in the open, or inside an open group. Memo: its props stay the same objects while the reply
 *  streams (SegmentView memoizes steps and p on the segment), so a delta skips every step of the turn. */
export const StepView = memo(function StepView({ step, p, live }: { step: Step; p: WorkProps; live?: boolean }) {
  switch (step.kind) {
    case "tool":
      if (p.trace) return <ActionRow step={step} running={live && !step.result} mid={p.mid} sid={p.sid} trace />;
      if (isChildSessionStep(step)) return <Suspense fallback={null}><ChildSessionCard mid={p.mid} sid={childSessionOf(step)} scope={p.scope} /></Suspense>;
      if (isAgentStep(step)) return <Suspense fallback={null}><SubagentCard step={step} status={p.agentStatus(step)} mid={p.mid} sid={p.sid} scope={p.scope} /></Suspense>;
      if (isPlanStep(step)) return <Suspense fallback={null}><PlanCard id={step.id} todos={planTodos(step)} live={live} /></Suspense>;
      if (workflowCard(step, p)) return <WorkflowCallCard step={step} mid={p.mid} sid={p.sid} scope={p.scope} />;
      return <ActionRow step={step} running={live && !step.result} mid={p.mid} sid={p.sid} />;
    case "thinking":
      return <Suspense fallback={null}><ThinkingRow item={step.item} scope={p.scope} /></Suspense>;
    case "text":
      return <Answer item={step.item} mid={p.mid} sid={p.sid} />;
    case "notice":
      return <NoticeRow item={step.item} mid={p.mid} sid={p.sid} />;
    case "image":
      return (
        <div className="py-1">
          <Thumb item={step.item} mid={p.mid} sid={p.sid} />
        </div>
      );
    case "visualization":
      return (
        <div className="py-1">
          <Suspense fallback={null}><VisualizationCard item={step.item} mid={p.mid} sid={p.sid} /></Suspense>
        </div>
      );
    case "fold":
      return (
        <div className="flex h-7 items-center gap-2 px-2 text-[13px] text-tool-ink">
          <Spinner size={11} /> {t("chat-workgroup.loading_steps", { count: step.item.fold?.items ?? 0 })}
        </div>
      );
  }
});

/** A live group's clock. Its own leaf redraws while the rest of the work summary stays still. */
function Elapsed({ since }: { since: number }) {
  // stands still with the hub out of reach (the running bar says so): nothing can be known to go on
  const now = useNow(500, useStore((s) => s.conn === "online"));
  const count = useContext(LiveClockContext);
  // before paint: the bar's time goes in the same frame this one comes in
  useLayoutEffect(() => {
    count?.(1);
    return () => count?.(-1);
  }, [count]);
  return <span data-work-elapsed className="shrink-0 font-mono text-[12px] text-tool-ink tabular-nums">{workDuration(Math.max(0, now - since))}</span>;
}
