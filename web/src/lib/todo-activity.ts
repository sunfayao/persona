// Folds a to-do's Agent lifecycle events for display: every background task is one run (its start and end events
// paired by reference), the runs of one session and turn are one group, and every other event stays a row of its own.
// Failed, killed or stopped runs are counted apart in their group and listed on their own line; turn failures, coverage
// gaps and waits are not background events, so they are never folded.
import type { AgentLifecycleEvent } from "./types";

export type RunOutcome = "finished" | "failed" | "cancelled" | "running";

export interface BackgroundRun {
  key: string;
  startedAt: string;
  endedAt?: string;
  durationMs?: number;
  outcome: RunOutcome;
  /** The node's own word for how it ended ("completed", "failed", "killed", "stopped"); "" while it runs. */
  ended: string;
  activity?: string;
}

export interface ActivityGroup {
  kind: "group";
  key: string;
  machine_id: string;
  session_id: string;
  /** Earliest start and latest end (or start) of its runs. */
  from: string;
  to: string;
  runs: BackgroundRun[];
  counts: Record<RunOutcome, number>;
  /** Activity classes with their run counts, most frequent first. */
  classes: [string, number][];
  observed: boolean;
}

export interface ActivityRow {
  kind: "event";
  key: string;
  event: AgentLifecycleEvent;
}

export type ActivityItem = ActivityGroup | ActivityRow;

/** Runs of a session that start within this long of each other, outside any turn, form one group. */
export const GROUP_GAP_MS = 10 * 60_000;

const ms = (iso: string) => Date.parse(iso);

function outcomeOf(ended: string | undefined, hasEnd: boolean): RunOutcome {
  if (!hasEnd) return "running";
  if (!ended || ended === "completed") return "finished";
  return ended === "failed" ? "failed" : "cancelled";
}

interface Located extends BackgroundRun {
  machine_id: string;
  session_id: string;
  incarnation: string;
  turn: string;
  startMs: number;
  endMs: number;
  observed: boolean;
}

interface Window {
  turn: string;
  incarnation: string;
  from: number;
  to: number;
}

/** Each turn's span in one session: from its start to its end, or to the next turn's start while no end is known. */
function windowsOf(events: AgentLifecycleEvent[]): Map<string, Window[]> {
  const ends = new Map<string, number>();
  for (const e of events) {
    if ((e.kind === "turn.finished" || e.kind === "turn.failed") && e.turn_id) ends.set(`${e.machine_id}|${e.session_id}|${e.turn_id}`, ms(e.occurred_at));
  }
  const bySession = new Map<string, AgentLifecycleEvent[]>();
  for (const e of events) {
    if (e.kind !== "turn.started" || !e.turn_id) continue;
    const k = `${e.machine_id}|${e.session_id}`;
    bySession.set(k, [...(bySession.get(k) ?? []), e]);
  }
  const out = new Map<string, Window[]>();
  for (const [k, starts] of bySession) {
    starts.sort((a, b) => ms(a.occurred_at) - ms(b.occurred_at));
    out.set(k, starts.map((e, i) => {
      const next = starts[i + 1];
      const end = ends.get(`${k}|${e.turn_id}`) ?? (next ? ms(next.occurred_at) : Infinity);
      return { turn: e.turn_id!, incarnation: e.incarnation ?? "", from: ms(e.occurred_at), to: end };
    }));
  }
  return out;
}

function runsOf(events: AgentLifecycleEvent[]): Located[] {
  const byRef = new Map<string, { started?: AgentLifecycleEvent; ended?: AgentLifecycleEvent }>();
  for (const e of events) {
    if (e.kind !== "background.started" && e.kind !== "background.ended") continue;
    // A row without a reference cannot be paired: it is a run of its own.
    const k = `${e.machine_id}|${e.session_id}|${e.ref || e.revision}`;
    const pair = byRef.get(k) ?? {};
    if (e.kind === "background.started") pair.started = e; else pair.ended = e;
    byRef.set(k, pair);
  }
  const runs: Located[] = [];
  for (const [key, { started, ended }] of byRef) {
    const any = (started ?? ended)!;
    const durationMs = ended?.duration_ms && ended.duration_ms > 0 ? ended.duration_ms
      : started && ended ? Math.max(0, ms(ended.occurred_at) - ms(started.occurred_at)) : undefined;
    const startedAt = started?.occurred_at ?? new Date(ms(ended!.occurred_at) - (durationMs ?? 0)).toISOString();
    const endedAt = ended?.occurred_at;
    runs.push({
      key,
      startedAt,
      endedAt,
      durationMs,
      outcome: outcomeOf(ended?.outcome, !!ended),
      ended: ended?.outcome ?? "",
      activity: started?.activity || ended?.activity || undefined,
      machine_id: any.machine_id,
      session_id: any.session_id,
      incarnation: started?.incarnation || ended?.incarnation || "",
      turn: started?.turn_id || ended?.turn_id || "",
      startMs: ms(startedAt),
      endMs: ms(endedAt ?? startedAt),
      observed: [started, ended].some((e) => e?.confidence === "observed"),
    });
  }
  return runs;
}

function groupOf(key: string, runs: Located[]): ActivityGroup {
  const counts: Record<RunOutcome, number> = { finished: 0, failed: 0, cancelled: 0, running: 0 };
  const classes = new Map<string, number>();
  for (const r of runs) {
    counts[r.outcome]++;
    if (r.activity) classes.set(r.activity, (classes.get(r.activity) ?? 0) + 1);
  }
  const sorted = [...runs].sort((a, b) => b.startMs - a.startMs || (a.key < b.key ? 1 : -1));
  const last = runs.reduce((a, r) => (r.endMs > a.endMs ? r : a));
  return {
    kind: "group",
    key,
    machine_id: runs[0].machine_id,
    session_id: runs[0].session_id,
    from: runs.reduce((a, r) => (r.startMs < a.startMs ? r : a)).startedAt,
    to: last.endedAt ?? last.startedAt,
    runs: sorted.map(({ key: k, startedAt, endedAt, durationMs, outcome, ended, activity }) => ({ key: k, startedAt, endedAt, durationMs, outcome, ended, activity })),
    counts,
    classes: [...classes].sort((a, b) => b[1] - a[1] || (a[0] < b[0] ? -1 : 1)),
    observed: runs.some((r) => r.observed),
  };
}

/**
 * The display rows of every loaded page, newest first. Recomputed from all pages each time, so a group that straddles a
 * page boundary is one group once the next page has loaded.
 */
export function foldActivity(events: AgentLifecycleEvent[]): ActivityItem[] {
  const windows = windowsOf(events);
  const grouped = new Map<string, Located[]>();
  const loose = new Map<string, Located[]>();
  for (const run of runsOf(events)) {
    const session = `${run.machine_id}|${run.session_id}`;
    let turn = run.turn;
    if (!turn) {
      const w = (windows.get(session) ?? []).filter((x) => x.from <= run.startMs && run.startMs <= x.to && (!x.incarnation || !run.incarnation || x.incarnation === run.incarnation));
      turn = w.at(-1)?.turn ?? "";
    }
    const bucket = turn ? grouped : loose;
    const k = turn ? `${session}|turn|${turn}` : session;
    bucket.set(k, [...(bucket.get(k) ?? []), run]);
  }
  const groups: ActivityGroup[] = [...grouped].map(([k, runs]) => groupOf(k, runs));
  for (const [session, runs] of loose) {
    runs.sort((a, b) => a.startMs - b.startMs);
    let cluster: Located[] = [];
    const flush = () => {
      if (cluster.length) groups.push(groupOf(`${session}|near|${cluster.at(-1)!.key}`, cluster));
      cluster = [];
    };
    for (const run of runs) {
      if (cluster.length && run.startMs - cluster.at(-1)!.startMs > GROUP_GAP_MS) flush();
      cluster.push(run);
    }
    flush();
  }
  const rows: ActivityRow[] = events
    .filter((e) => e.kind !== "background.started" && e.kind !== "background.ended")
    .map((event) => ({ kind: "event", key: event.revision, event }));
  const at = (item: ActivityItem) => ms(item.kind === "group" ? item.to : item.event.occurred_at);
  return [...groups, ...rows].sort((a, b) => at(b) - at(a));
}
