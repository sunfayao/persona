import { describe, expect, it } from "vitest";
import type { TaskReview } from "./supervision";
import type { Todo, TodoAgent } from "./todos";
import { entryWords, supervisionDays, supervisionLog, supervisionRole, supervisorOf, workerAgent, workerStep, workerStepLabel, type SupervisionEntry } from "./todo-supervisor";

// Times in the local zone, as the owner's days are: 28 Nov 2026 at noon is "now".
const at = (day: number, hour: number, minute = 0) => new Date(2026, 10, day, hour, minute).toISOString();
const NOW = new Date(2026, 10, 28, 12).getTime();

const agent = (sid: string, patch: Partial<TodoAgent> = {}): TodoAgent => ({ machine_id: "m1", session_id: sid, linked_at: at(28, 8), ...patch });
const todo = (id: string, title: string, patch: Partial<Todo> = {}): Todo => ({ id, title, created_at: at(28, 8), updated_at: at(28, 8), ...patch });
const review = (time: string, verdict: TaskReview["verdict"], patch: Partial<TaskReview> = {}): TaskReview => ({ at: time, verdict, ...patch });

const LEAD = "m1/lead";
const root = todo("root", "Ship the release notes", {
  agents: [agent("old-lead", { source: "leader", released_at: at(27, 9), linked_at: at(27, 8) }), agent("lead", { source: "leader" })],
  supervision: { supervisor: "main", state: "pending_review", pending_since: at(28, 11) },
});
// accepted by the supervisor, which closed it
const parser = todo("w-parser", "Write the parser", {
  created_at: at(27, 10),
  agents: [agent("w1", { machine_id: "m2", source: "worker", via: "root" })],
  supervision: { supervisor: LEAD, state: "accepted", reviews: [review(at(28, 9, 30), "accepted", { round: 1, note: "Every criterion met." })] },
  done_at: at(28, 9, 30),
  done_by: "review",
});
// sent back twice
const tests = todo("w-tests", "Fix the flaky tests", {
  created_at: at(28, 8, 5),
  agents: [agent("w2", { source: "worker", via: "root" })],
  supervision: { supervisor: LEAD, state: "rework", round: 2, reviews: [review(at(28, 9), "rework", { round: 1, note: "The retry loop is still there." }), review(at(28, 10), "rework", { round: 2 })] },
});
// found by its ledger alone; says it is done, no review since
const docs = todo("w-docs", "Update the docs", {
  created_at: at(28, 8, 10),
  supervision: { supervisor: LEAD, progress: { kind: "done", text: "Docs updated", at: at(28, 10, 30) } },
});
// cancelled without a verdict
const spike = todo("w-spike", "Try the other parser", {
  created_at: at(25, 16),
  agents: [agent("w4", { source: "worker", via: "root", released_at: at(25, 18) })],
  supervision: { supervisor: LEAD },
  done_at: at(25, 18),
  cancelled_at: at(25, 18),
  done_by: "cancelled",
  cancelled_reason: "Not needed after all.",
});
const unrelated = todo("other", "Water the plants", { supervision: { supervisor: "main" }, agents: [agent("x", { source: "manual" })] });
const list = [unrelated, docs, root, spike, tests, parser];

describe("a supervisor and its workers", () => {
  it("finds the supervisor on it now, and its workers by their worker row or their ledger, oldest first", () => {
    expect(supervisorOf(root)?.session_id).toBe("lead");
    expect(supervisorOf({ agents: [agent("gone", { source: "leader", dismissed_at: at(28, 9) })] })).toBeUndefined();
    const role = supervisionRole(root, list);
    expect(role?.kind === "root" && role.workers.map((w) => w.id)).toEqual(["w-spike", "w-parser", "w-tests", "w-docs"]);
  });

  it("knows a worker's root, by its worker row or by the supervisor its ledger names, and leaves other to-dos alone", () => {
    expect(supervisionRole(parser, list)).toEqual({ kind: "worker", root });
    expect(supervisionRole(docs, list)).toEqual({ kind: "worker", root });
    expect(supervisionRole({ ...parser, id: "w-alone" }, [])).toEqual({ kind: "worker", root: undefined });
    expect(supervisionRole(unrelated, list)).toBeUndefined();
  });

  it("takes a worker's own Agent: its worker row first", () => {
    expect(workerAgent({ agents: [agent("a", { source: "manual" }), agent("w", { source: "worker" }), agent("b")] })?.session_id).toBe("w");
    expect(workerAgent({ agents: [agent("a"), agent("b", { released_at: at(28, 9) })] })?.session_id).toBe("a");
  });

  it("says where each worker stands", () => {
    const steps = (w: Todo) => [workerStep(w), workerStepLabel(w)];
    expect(steps(parser)).toEqual(["accepted", "已验收"]);
    expect(steps(tests)).toEqual(["rework", "返工第 2 轮"]);
    expect(steps(docs)).toEqual(["review", "待验收"]);
    expect(steps(spike)).toEqual(["cancelled", "已取消"]);
    expect(steps(todo("w", "Plain", { supervision: { supervisor: LEAD } }))).toEqual(["working", "工作中"]);
    expect(steps(todo("w", "Waits", { supervision: { supervisor: LEAD, state: "pending_review" } }))).toEqual(["review", "待验收"]);
    expect(steps(todo("w", "Closed", { done_at: at(28, 9), done_by: "auto" }))).toEqual(["done", "已完成"]);
    expect(steps(todo("w", "No start", { done_at: at(28, 9), start_error: "no such directory" }))).toEqual(["failed", "没开起来"]);
    // a report answered by a later review waits for nothing
    expect(workerStep({ ...docs, supervision: { ...docs.supervision!, reviews: [review(at(28, 11), "rework", { round: 1 })], state: "rework" } })).toBe("rework");
  });
});

describe("the Supervision log", () => {
  const line = (e: SupervisionEntry) => [e.kind, e.todo.id, ...Object.values(entryWords(e))].join(" | ");

  it("lists a root's workers and the main conversation's side, newest first, a verdict that closed one only once", () => {
    const log = supervisionLog(root, supervisionRole(root, list));
    expect(log.map(line)).toEqual([
      "reported | root | 已向主对话报告完成 | ",
      "report | w-docs | 最新汇报 | 说做完了：Docs updated",
      "review | w-tests | 返工第 2 轮 | ",
      "review | w-parser | 已验收 | Every criterion met.",
      "review | w-tests | 返工第 1 轮 | The retry loop is still there.",
      "planned | w-docs | 派出 | ",
      "planned | w-tests | 派出 | ",
      "planned | w-parser | 派出 | ",
      "closed | w-spike | 已取消 | Not needed after all.",
      "planned | w-spike | 派出 | ",
    ]);
  });

  it("adds the main conversation's verdicts on the root, and how it closed when no verdict did", () => {
    const accepted = { ...root, done_at: at(28, 13), done_by: "review", supervision: { supervisor: "main", state: "accepted" as const, reviews: [review(at(28, 13), "accepted")] } };
    const log = supervisionLog(accepted, supervisionRole(accepted, list));
    expect(log.slice(0, 2).map(line)).toEqual(["review | root | 主对话 · 已验收 | ", "report | w-docs | 最新汇报 | 说做完了：Docs updated"]);
    const closed = { ...root, supervision: undefined, done_at: at(28, 13), done_by: "auto", done_reason: "Every worker is accepted." };
    expect(line(supervisionLog(closed, supervisionRole(closed, list))[0])).toBe("closed | root | 已完成 | Every worker is accepted.");
  });

  it("is a worker's own story on its page, and nothing for other to-dos", () => {
    expect(supervisionLog(tests, supervisionRole(tests, list)).map((e) => e.kind)).toEqual(["review", "review", "planned"]);
    expect(supervisionLog(unrelated, supervisionRole(unrelated, list))).toEqual([]);
  });

  it("goes by the owner's local day, newest first, today marked", () => {
    const days = supervisionDays(supervisionLog(root, supervisionRole(root, list)), NOW);
    expect(days.map((d) => [d.key, d.today, d.entries.length])).toEqual([
      ["2026-11-28", true, 7],
      ["2026-11-27", false, 1],
      ["2026-11-25", false, 2],
    ]);
    expect(days[1].date).toEqual(new Date(2026, 10, 27));
    // a line just before midnight stays on its own day
    const late = { ...docs, created_at: at(27, 23, 59), supervision: { supervisor: LEAD } };
    expect(supervisionDays(supervisionLog(late, { kind: "worker" }), NOW).map((d) => [d.key, d.today])).toEqual([["2026-11-27", false]]);
  });
});
