import { describe, expect, it } from "vitest";
import { foldActivity, type ActivityGroup } from "./todo-activity";
import type { AgentLifecycleEvent } from "./types";

const T0 = Date.UTC(2026, 10, 28, 9, 0, 0);
const at = (minutes: number, seconds = 0) => new Date(T0 + minutes * 60_000 + seconds * 1000).toISOString();

let revision = 0;
const event = (kind: string, session: string, time: string, patch: Partial<AgentLifecycleEvent> = {}): AgentLifecycleEvent => ({
  revision: String(++revision),
  machine_id: "m1",
  session_id: session,
  kind,
  occurred_at: time,
  confidence: "observed",
  source: "inventory",
  ...patch,
});

/** The pair of rows one background task leaves. */
const run = (session: string, id: string, start: string, end: string | undefined, patch: Partial<AgentLifecycleEvent> = {}, outcome = "completed") => [
  ...(end ? [event("background.ended", session, end, { ref: id, outcome, ...patch })] : []),
  event("background.started", session, start, { ref: id, ...patch }),
];

const groups = (events: AgentLifecycleEvent[]) => foldActivity(events).filter((i): i is ActivityGroup => i.kind === "group");

describe("foldActivity", () => {
  it("folds the 32 rows of one turn's 16 background commands into one group", () => {
    const turn = "t1";
    const events = [
      event("turn.started", "s1", at(0), { turn_id: turn }),
      ...Array.from({ length: 16 }, (_, i) => run("s1", `task-${i}`, at(1 + i), at(1 + i, 30), {}, i % 8 === 7 ? "killed" : "completed")).flat(),
      event("turn.finished", "s1", at(20), { turn_id: turn, confidence: "confirmed" }),
    ];
    expect(events.filter((e) => e.kind.startsWith("background")).length).toBe(32);
    const items = foldActivity(events);
    expect(items.filter((i) => i.kind === "group")).toHaveLength(1);
    expect(items).toHaveLength(3); // the group and the two turn rows
    const [group] = groups(events);
    expect(group.runs).toHaveLength(16);
    expect(group.counts).toEqual({ finished: 14, failed: 0, cancelled: 2, running: 0 });
    expect(group.observed).toBe(true);
    expect(group.from).toBe(at(1));
    expect(group.to).toBe(at(16, 30));
    expect(group.runs.every((r) => r.durationMs === 30_000)).toBe(true);
  });

  it("keeps a failed run visible in the counts and as its own run", () => {
    const events = [...run("s1", "a", at(1), at(2)), ...run("s1", "b", at(3), at(4), {}, "failed"), ...run("s1", "c", at(5), undefined)];
    const [group] = groups(events);
    expect(group.counts).toEqual({ finished: 1, failed: 1, cancelled: 0, running: 1 });
    const failed = group.runs.find((r) => r.outcome === "failed")!;
    expect(failed.ended).toBe("failed");
    expect(failed.endedAt).toBe(at(4));
    expect(group.runs.find((r) => r.outcome === "running")?.endedAt).toBeUndefined();
  });

  it("treats a stopped or an unknown ending as cancelled, never as finished", () => {
    const [group] = groups([...run("s1", "a", at(1), at(2), {}, "stopped"), ...run("s1", "b", at(3), at(4), {}, "something_new")]);
    expect(group.counts.cancelled).toBe(2);
    expect(group.counts.finished).toBe(0);
  });

  it("shows an ended run with an empty or newer outcome as cancelled, not finished", () => {
    const found = groups([
      ...run("s1", "a", at(1), at(2), {}, ""),
      ...run("s1", "b", at(3), at(4), {}, "timed_out"),
      ...run("s1", "c", at(5), at(6), {}, "cancelled"),
      ...run("s1", "d", at(7), at(8), {}, "interrupted"),
    ]);
    expect(found[0].counts).toEqual({ finished: 0, failed: 0, cancelled: 4, running: 0 });
  });

  it("does not merge two sessions", () => {
    const found = groups([...run("s1", "a", at(1), at(2)), ...run("s2", "a", at(1), at(2))]);
    expect(found).toHaveLength(2);
    expect(found.map((g) => g.session_id).sort()).toEqual(["s1", "s2"]);
  });

  it("merges a group that is split across two pages", () => {
    const first = [...run("s1", "c", at(5), at(6), { turn_id: "t1" }), ...run("s1", "d", at(7), undefined, { turn_id: "t1" })];
    const second = [...run("s1", "a", at(1), at(2), { turn_id: "t1" }), ...run("s1", "b", at(3), at(4), { turn_id: "t1" })];
    expect(groups(first)).toHaveLength(1);
    const merged = groups([...first, ...second]);
    expect(merged).toHaveLength(1);
    expect(merged[0].runs).toHaveLength(4);
    expect(merged[0].key).toBe(groups(first)[0].key);
  });

  it("pairs a run whose two rows landed on different pages", () => {
    const ended = event("background.ended", "s1", at(2), { ref: "a", outcome: "completed" });
    const started = event("background.started", "s1", at(1), { ref: "a" });
    expect(groups([ended])[0].runs[0].outcome).toBe("finished");
    const [group] = groups([ended, started]);
    expect(group.runs).toHaveLength(1);
    expect(group.runs[0].durationMs).toBe(60_000);
  });

  it("assigns a run without a turn id to the turn window that contains its start", () => {
    const events = [
      event("turn.started", "s1", at(0), { turn_id: "t1" }),
      event("turn.finished", "s1", at(10), { turn_id: "t1" }),
      event("turn.started", "s1", at(30), { turn_id: "t2" }),
      event("turn.finished", "s1", at(40), { turn_id: "t2" }),
      ...run("s1", "a", at(1), at(2)),
      ...run("s1", "b", at(5), at(6)),
      ...run("s1", "c", at(31), at(32)),
    ];
    expect(groups(events).map((g) => g.runs.length).sort()).toEqual([1, 2]);
  });

  it("joins a run with a turn id and one assigned by window to the same group", () => {
    const events = [event("turn.started", "s1", at(0), { turn_id: "t1" }), ...run("s1", "a", at(1), at(2), { turn_id: "t1" }), ...run("s1", "b", at(3), at(4))];
    expect(groups(events)).toHaveLength(1);
  });

  it("groups runs outside any turn that start within ten minutes of each other", () => {
    const events = [...run("s1", "a", at(0), at(1)), ...run("s1", "b", at(9), at(10)), ...run("s1", "c", at(18), at(19)), ...run("s1", "d", at(60), at(61))];
    expect(groups(events).map((g) => g.runs.length).sort()).toEqual([1, 3]);
  });

  it("keeps the activity classes with counts and leaves unclassified runs out of them", () => {
    const events = [
      ...run("s1", "a", at(1), at(2), { activity: "test" }),
      ...run("s1", "b", at(3), at(4), { activity: "test" }),
      ...run("s1", "c", at(5), at(6), { activity: "build" }),
      ...run("s1", "d", at(7), at(8)),
    ];
    expect(groups(events)[0].classes).toEqual([["test", 2], ["build", 1]]);
  });

  it("leaves every other event as its own row, in time order", () => {
    const events = [
      event("turn.failed", "s1", at(9), { outcome: "interrupted" }),
      event("coverage.gap", "", at(8), { outcome: "queue_overflow" }),
      ...run("s1", "a", at(1), at(7)),
      event("wait.opened", "s1", at(0)),
    ];
    expect(foldActivity(events).map((i) => (i.kind === "group" ? "group" : i.event.kind))).toEqual(["turn.failed", "coverage.gap", "group", "wait.opened"]);
  });
});
