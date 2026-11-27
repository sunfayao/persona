// An exchange as one continuous answer: the agent's words in order, each stretch of work between them one group (C),
// nothing replaced while it works (B); the time worked is the exchange's, said once (C).
import { describe, expect, it } from "vitest";
import { exchangeSteps, segments } from "./turns";
import type { Item } from "./types";
import {
  workLineOpen,
  workLineStyle,
  groupsHolding,
  groupOpens,
  groupTitle,
  isRowStep,
  isWorkflowStep,
  planCardStep,
  planProgress,
  planTodos,
  workBlocks,
  workDuration,
  workTime,
  toolDurationMs,
  toolDurationLabel,
  TICKING_DURATION,
  type AgentStatus,
  type ToolStep,
  type WorkGroupNode,
  type WorkNode,
} from "./work-groups";

const T0 = Date.parse("2026-07-01T00:00:00.000Z");
const iso = (offsetMs: number) => new Date(T0 + offsetMs).toISOString();
let seq = 0;
const mk = (role: Item["role"], kind: Item["kind"], at: number | undefined, extra: Partial<Item> = {}): Item => {
  seq++;
  return { id: `${extra.id ?? `i${seq}`}#0`, seq, role, kind, ...(at !== undefined ? { ts: iso(at) } : {}), ...extra, ...(extra.id ? { id: `${extra.id}#0` } : {}) };
};
const user = (id: string, at?: number, text = "巡检待放行的 PR，给一份产品决策简报") => mk("user", "text", at, { id, text });
const say = (id: string, text: string, at?: number) => mk("assistant", "text", at, { id, text });
const think = (id: string, at?: number, text = "Reviewing the repository structure…") => mk("assistant", "thinking", at, { id, text });
const tool = (id: string, name = "Bash", at?: number, resultAt?: number, input: Record<string, unknown> = { command: "gh pr diff 1111" }, result = "ok"): Item[] => [
  mk("assistant", "tool_use", at, { id, tool_name: name, tool_use_id: `tu-${id}`, tool_input: input }),
  mk("user", "tool_result", resultAt ?? at, { id: `${id}-r`, tool_use_id: `tu-${id}`, text: result }),
];

function blocks(items: Item[], live = false, agentStatus?: (s: ToolStep) => AgentStatus): WorkNode[] {
  const seg = segments(items)[0];
  void agentStatus;
  return workBlocks(exchangeSteps(seg), { live, turnStart: seg.user?.ts ?? seg.start });
}

const idOf = (n: WorkNode) => (n.kind === "step" ? n.step.id.replace(/#\d+$/, "") : n.id);
/** The exchange as drawn: a piece of words (or a step in the open) by its id, a stretch as the ids of its steps. */
const shape = (ns: WorkNode[]) => ns.map((n): string | string[] => (n.kind === "step" ? idOf(n) : n.children.map(idOf)));
const groups = (ns: WorkNode[]) => ns.filter((n): n is WorkGroupNode => n.kind === "group");
const S = 1000;
const MIN = 60 * S;

describe("one answer: the words in the order they were written, each stretch of work between them a group", () => {
  it("keeps every piece of words in the open, intermediate ones too, however long or shaped", () => {
    const long = `本轮 7 条有活动，3 条首次亮相。${"逐条核对了改动落在哪些产品面。".repeat(50)}`;
    for (const body of [long, "## 产品决策简报\n\n本轮 7 条有活动。", "我已经读完所有必要信息，现在写简报。"]) {
      const b = blocks([user("u1"), think("th1"), ...tool("diff"), say("brief", body), ...tool("notify", "mcp__scheduler__notify"), say("wrap", "已触发通知。")]);
      expect(shape(b)).toEqual([["th1", "diff"], "brief", ["notify"], "wrap"]);
    }
  });

  it("a finished exchange: each stretch keeps the steps of its own run, keyed by its first step", () => {
    const b = blocks([user("u1", 0), think("th1", 1000), ...tool("t1", "Read", 2000, 2500, { file_path: "/a.ts" }), say("n1", "Found it.", 3000),
      ...tool("t2", "Bash", 4000, 6000), think("th2", 6500), ...tool("t3", "Bash", 7000, 9000), say("final", "All done.", 10_000)]);
    expect(shape(b)).toEqual([["th1", "t1"], "n1", ["t2", "th2", "t3"], "final"]);
    expect(groups(b).map((g) => g.live)).toEqual([false, false]);
    expect(new Set(groups(b).map((g) => g.id)).size).toBe(2);
    expect(groups(b).every((g) => g.id.startsWith("work-"))).toBe(true);
  });

  it("words one after another are each in the open; text without words is no boundary", () => {
    expect(shape(blocks([user("u1", 0), say("n1", "First part.", 1000), say("n2", "Second part.", 2000)]))).toEqual(["n1", "n2"]);
    const seg = segments([user("u1", 0), ...tool("t1", "Bash", 1000), ...tool("t2", "Bash", 2000), say("final", "Done.", 5000)])[0];
    const steps = exchangeSteps(seg);
    const blank = say("blank", "", 1500);
    steps.splice(1, 0, { kind: "text", id: blank.id, item: blank });
    expect(shape(workBlocks(steps, { live: false }))).toEqual([["t1", "blank", "t2"], "final"]);
  });

  it("thoughts without words are no steps: no stretch of them alone, and they split no stretch", () => {
    expect(shape(blocks([user("u1", 0), think("blank", 1000, ""), say("final", "Done.", 3000)]))).toEqual(["final"]);
    expect(blocks([user("u1", 0), think("blank", 1000, "  ")], true)).toEqual([]);
    const b = blocks([user("u1", 0), ...tool("t1", "Read", 1000), think("x1", 1500, ""), ...tool("t2", "Grep", 2000, undefined, { pattern: "x" }),
      say("n1", "First.", 3000), think("x2", 3500, ""), say("n2", "Second.", 4000), ...tool("t3", "Bash", 5000), say("final", "Done.", 6000)]);
    expect(shape(b)).toEqual([["t1", "t2"], "n1", "n2", ["t3"], "final"]);
    expect(groupsHolding(b, "tu-t2")).toEqual([groups(b)[0].id]);
  });

  it("a compaction divider stands in the open between stretches", () => {
    const compact = mk("system", "notice", 9500, { id: "cmp", text: "Compacted", compact: { trigger: "auto" } });
    const b = blocks([user("u1", 0), ...tool("t1", "Bash", 1000), say("n1", "Saved.", 9200), compact, ...tool("t2", "Bash", 10_000), say("final", "Done.", 12_000)]);
    expect(shape(b)).toEqual([["t1"], "n1", "cmp", ["t2"], "final"]);
  });

  it("a subagent, a plan update and a workflow call stay in the stretch they ran in", () => {
    const todos = { todos: [{ content: "Read", status: "completed" }, { content: "Edit", status: "in_progress" }] };
    const b = blocks([user("u1", 0), ...tool("pre", "Bash", 500), ...tool("task1", "Agent", 1000, 9000, { description: "回归", prompt: "跑回归" }),
      ...tool("p", "TodoWrite", 9500, undefined, todos), ...tool("wf", "Workflow", 9600, 9600, { name: "audit" }), say("final", "都做完了。", 10_000)], false, () => "running");
    expect(shape(b)).toEqual([["pre", "task1", "p", "wf"], "final"]);
  });
});

describe("Codex: every message stays its own paragraph, while it works and after (#5853)", () => {
  // a Codex rollout's shape: commentary between commands, then the final answer, each message its own item
  const codex = () => [user("u1", 0, "Merge the fix into main."), say("c1", "I'll check the branch first.", 1000), ...tool("git", "Bash", 1500, 2000),
    say("c2", "The branch is clean; running the focused tests.", 3000), ...tool("test", "Bash", 3500, 60_000), say("c3", "217 tests pass; the build is next.", 61_000), ...tool("build", "Bash", 62_000)];

  it("while working: the earlier messages stay, in order, with the commands between them", () => {
    const b = blocks(codex(), true);
    expect(shape(b)).toEqual(["c1", ["git"], "c2", ["test"], "c3", ["build"]]);
    expect(groups(b).map((g) => g.live)).toEqual([false, false, true]);
  });

  it("a new message is added after the others, nothing is replaced, and each stretch keeps its key", () => {
    const base = codex();
    const running = blocks(base, true);
    const more = blocks([...base, say("c4", "Built; merging.", 90_000), ...tool("merge", "Bash", 91_000)], true);
    expect(shape(more)).toEqual(["c1", ["git"], "c2", ["test"], "c3", ["build"], "c4", ["merge"]]);
    expect(groups(more).slice(0, 3).map((g) => g.id)).toEqual(groups(running).map((g) => g.id));
    const done = blocks([...base, say("final", "Merged into main.", 95_000)]);
    expect(shape(done)).toEqual(["c1", ["git"], "c2", ["test"], "c3", ["build"], "final"]);
    expect(groups(done).map((g) => g.id)).toEqual(groups(running).map((g) => g.id));
  });
});

describe("Codex visualization fragments stand in the open where they were written (#10321)", () => {
  const viz = (id: string, at?: number) =>
    mk("assistant", "visualization", at, { id, visualization: { path: `/home/me/.codex/visualizations/2026/10/28/thread/${id}.html`, mode: "wide" } });

  it("after the words, before them, or alone: never inside a stretch", () => {
    expect(shape(blocks([user("u1", 0), ...tool("b1", "Bash", 1000, 1500), say("a1", "Done.", 2000), viz("v1", 2000)]))).toEqual([["b1"], "a1", "v1"]);
    expect(shape(blocks([user("u1", 0), ...tool("b1", "Bash", 1000, 1500), viz("v1", 2000), viz("v2", 2001)]))).toEqual([["b1"], "v1", "v2"]);
  });
});

describe("the live indicator", () => {
  it("is the tail stretch while the agent works, with the exchange's clock from your message", () => {
    const b = blocks([user("u1", 0), ...tool("t1", "Bash", 1000), say("a1", "Progress.", 2000), ...tool("t2", "Bash", 3000)], true);
    expect(groups(b).map((g) => g.live)).toEqual([false, true]);
    expect(groups(b)[1].startedAt).toBe(T0);
    expect(groupTitle(groups(b)[1])).toBe("正在工作… · 运行 1 条命令");
  });

  it("is no stretch when words came after the last one, or are streaming after it", () => {
    expect(groups(blocks([user("u1", 0), ...tool("t1", "Bash", 1000), say("a1", "Progress.", 2000)], true)).map((g) => g.live)).toEqual([false]);
    const seg = segments([user("u1", 0), ...tool("t1", "Bash", 1000)])[0];
    expect(groups(workBlocks(exchangeSteps(seg), { live: true, streaming: true, turnStart: seg.user?.ts })).map((g) => g.live)).toEqual([false]);
  });

  it("the first running tool can be opened", () => {
    const [g] = groups(blocks([user("u1", 0), ...tool("t1", "Bash", 1000)], true));
    expect(groupOpens(g)).toBe(true);
    expect(groupOpens({ ...g, children: [] })).toBe(false);
  });

  it("after a wake its clock counts from the wake, not from the message hours before", () => {
    const items = [user("u1", 0), ...tool("t1", "Bash", 1000, 2000), say("a1", "Started the build.", 3000)];
    items.push(mk("system", "notice", 3 * 3600 * S, { id: "wake", text: "后台命令完成 · build", wake: true }), ...tool("t2", "Bash", 3 * 3600 * S + 1000));
    const b = blocks(items, true);
    expect(shape(b)).toEqual([["t1"], "a1", ["wake", "t2"]]);
    expect(groups(b)[1].startedAt).toBe(T0 + 3 * 3600 * S);
  });
});

describe("a wake is part of the answer, never a row of its own", () => {
  it("a wake alone between two pieces of words draws nothing; with work after it, it is a quiet step of that stretch", () => {
    const alone = [user("u1", 0), say("a", "The check runs in the background.", 500)];
    alone.push(mk("system", "notice", 60_000, { id: "w", text: "后台任务有更新", wake: true }), say("b", "The check is complete.", 61_000));
    expect(shape(blocks(alone))).toEqual(["a", "b"]);
    const worked = [user("u1", 0), say("a", "The check runs in the background.", 500)];
    worked.push(mk("system", "notice", 60_000, { id: "w", text: "后台任务有更新", wake: true }), ...tool("t", "Bash", 60_500), say("b", "The check is complete.", 61_000));
    const b = blocks(worked);
    expect(shape(b)).toEqual(["a", ["w", "t"], "b"]);
    expect(groupTitle(groups(b)[0])).toBe("运行 1 条命令");
  });
});

describe("a stretch's row says in words what it did, never how long", () => {
  it("counts by kind in first-seen order", () => {
    const g = groups(blocks([user("u1", 0), ...tool("r1", "Read", 1000, undefined, { file_path: "/a.ts" }), ...tool("r2", "Read", 2000, undefined, { file_path: "/a.ts" }),
      ...tool("b", "Bash", 3000), ...tool("e", "Edit", 4000, undefined, { file_path: "/b.ts" }), say("a", "好。", 65_000)]))[0];
    expect(groupTitle(g)).toBe("读取 1 个文件 · 运行 1 条命令 · 编辑 1 个文件");
    expect(groupTitle(g)).not.toMatch(/已工作|\d+s/);
  });

  it("without tool calls: a background task's notice in its one line, else that it thought, else Work details", () => {
    const released = [user("u1", 0), say("a", "Waiting.", 500)];
    released.push(mk("system", "notice", 1000, { id: "n", text: "后台命令完成 · Release", task: { kind: "shell", status: "completed", title: "Release" } }), say("b", "Released.", 2000));
    expect(groupTitle(groups(blocks(released))[0])).toBe("后台命令完成 · Release");
    expect(groupTitle(groups(blocks([user("u1", 0), think("th", 1000), say("a", "好。", 2000)]))[0])).toBe("思考");
    expect(groupTitle({ kind: "group", id: "g", children: [], live: false })).toBe("工作过程");
    expect(groupTitle({ kind: "group", id: "g", children: [], live: true })).toBe("正在工作…");
  });
});

describe("the time an exchange worked, said once", () => {
  it("runs from your message to the latest end of any step, its answer included (#5119)", () => {
    const steps = (items: Item[]) => exchangeSteps(segments(items)[0]);
    const items = [user("u1", 0), ...tool("a", "Bash", 2 * S, 492 * S), say("done", "It was closed by the merge.", 497 * S)];
    expect(workTime(steps(items), iso(0))).toEqual({ ms: 497 * S, since: T0 });
    // a tool that ran 40 minutes is work, not a gap
    expect(workTime(steps([user("u1", 0), ...tool("long", "Bash", MIN, 41 * MIN), say("a", "构建好了。", 42 * MIN)]), iso(0)).ms).toBe(42 * MIN);
    // without your message it counts from its first step; parallel calls end at the latest result
    expect(workTime(steps([...tool("long", "Bash", 0, 40 * MIN), ...tool("short", "Bash", MIN, 2 * MIN)])).ms).toBe(40 * MIN);
  });

  it("the finished time is never shorter than the live clock showed", () => {
    const running = [user("u1", 0), ...tool("a", "Bash", 2 * S, 492 * S)];
    const [g] = groups(blocks(running, true));
    const shown = T0 + 497 * S - g.startedAt!;
    const done = workTime(exchangeSteps(segments([...running, say("done", "Done.", 497 * S)])[0]), iso(0)).ms!;
    expect(done).toBe(shown);
    expect(workDuration(done)).toBe("8m 17s");
  });

  it("idle time before a wake, or a gap of over 30 minutes, is not counted; a compaction is not work", () => {
    const steps = (items: Item[]) => exchangeSteps(segments(items)[0]);
    const woken = [user("u1", 0), ...tool("t1", "Bash", 1000, 10 * S), say("a1", "Started.", 20 * S)];
    woken.push(mk("system", "notice", 3600 * S, { id: "wake", text: "后台命令完成 · build", wake: true }), ...tool("t2", "Bash", 3600 * S, 3630 * S), say("a2", "Built.", 3640 * S));
    expect(workTime(steps(woken), iso(0)).ms).toBe(20 * S + 40 * S);
    expect(workTime(steps([user("u1", 0), ...tool("t1", "Read", MIN), say("n", "Earlier.", 2 * MIN), ...tool("t2", "Read", 50 * MIN), say("a", "好了。", 52 * MIN)]), iso(0)).ms).toBe(4 * MIN);
    const idle = mk("system", "notice", 3600 * S, { id: "idle", text: "上下文已压缩", compact: { trigger: "idle" } });
    expect(workTime(steps([user("u1", 0), ...tool("a", "Bash", 2 * S, 492 * S), say("done", "Done.", 497 * S), idle]), iso(0)).ms).toBe(497 * S);
  });
});

describe("plans", () => {
  it("keeps every plan update in its stretch, while planCardStep picks the latest list of two or more for the card", () => {
    const todos = (s: string[]) => ({ todos: s.map((content, i) => ({ content, status: i === 0 ? "completed" : i === 1 ? "in_progress" : "pending" })) });
    const items = [user("u1", 0), ...tool("p1", "TodoWrite", 1000, undefined, todos(["Read", "Edit"])), ...tool("t", "Bash", 2000), ...tool("p2", "TodoWrite", 3000, undefined, todos(["Read", "Edit", "Test"])), ...tool("one", "TodoWrite", 3500, undefined, todos(["One"])), say("a", "Done.", 4000)];
    expect(shape(blocks(items))).toEqual([["p1", "t", "p2", "one"], "a"]);
    const steps = exchangeSteps(segments(items)[0]);
    const card = planCardStep(steps);
    expect(card).toBe("p2#0");
    const selected = steps.find((s) => s.id === card) as ToolStep;
    expect(planProgress(planTodos(selected))).toEqual({ done: 1, total: 3, current: "Edit", step: 2, allDone: false });
  });
});

describe("durations", () => {
  it("formats durations like cindy's formatSessionDuration: short units under an hour, the language's from one", () => {
    expect(workDuration(0)).toBe("1s");
    expect(workDuration(Number.NaN)).toBe("1s");
    expect(workDuration(6062)).toBe("6s");
    expect(workDuration(59_499)).toBe("59s");
    expect(workDuration(65_000)).toBe("1m 5s");
    expect(workDuration(120_000)).toBe("2m");
    expect(workDuration(3_599_000)).toBe("59m 59s");
    expect(workDuration(3_600_000)).toBe("1 小时 0 分");
    expect(workDuration(3_900_000)).toBe("1 小时 5 分");
    expect(workDuration(93_900_000)).toBe("1 天 2 小时 5 分");
    expect(workDuration(86_400_000)).toBe("1 天 0 小时 0 分");
  });

  it("reads as cindy's running status bar with its options: from 0s, the seconds always kept under an hour (#5312)", () => {
    expect(workDuration(0, TICKING_DURATION)).toBe("0s");
    expect(workDuration(59_000, TICKING_DURATION)).toBe("59s");
    expect(workDuration(60_000, TICKING_DURATION)).toBe("1m 0s");
    expect(workDuration(65_000, TICKING_DURATION)).toBe("1m 5s");
    expect(workDuration(3_600_000, TICKING_DURATION)).toBe("1 小时 0 分");
  });
});

describe("planProgress", () => {
  it("names the next pending step when none is in progress, the same one step points at", () => {
    const p = planProgress([
      { content: "A", status: "completed" },
      { content: "B", status: "pending" },
      { content: "C", status: "pending" },
    ]);
    expect(p).toMatchObject({ done: 1, total: 3, step: 2, current: "B", allDone: false });
  });
  it("names the one in progress, else the last when all are done", () => {
    expect(planProgress([{ content: "A", status: "completed" }, { content: "B", status: "in_progress" }]).current).toBe("B");
    expect(planProgress([{ content: "A", status: "completed" }, { content: "B", status: "completed" }])).toMatchObject({ current: "B", step: 2, allDone: true });
  });
});

describe("groupsHolding (#2719: a background task's notice → the call that started it)", () => {
  const items = () => [
    user("u1", 0),
    think("th", 1000),
    ...tool("grep", "Grep", 2000, 2500, { pattern: "x" }),
    ...tool("bg", "Bash", 3000, 3500, { command: "npm test", run_in_background: true }, "Command running in background with ID: b1"),
    ...tool("read", "Read", 4000, 4500, { file_path: "/a.ts" }),
    say("a", "测试在后台跑，跑完我再看。", 5000),
  ];

  it("names the groups to open, outer first, down to the call", () => {
    const b = blocks(items());
    const path = groupsHolding(b, "tu-bg");
    expect(path).not.toBeNull();
    expect(path!.length).toBeGreaterThan(0);
    // every id on the path is a group, each one inside the one before it
    let level: WorkNode[] = b;
    for (const id of path!) {
      const g = groups(level).find((x) => x.id === id);
      expect(g, id).toBeTruthy();
      level = g!.children;
    }
    expect(level.some((n) => n.kind === "step" && n.step.kind === "tool" && n.step.use.tool_use_id === "tu-bg")).toBe(true);
  });

  it("is empty for a call left in the open, null for one not there", () => {
    const b = blocks([user("u1", 0), ...tool("bg", "Bash", 1000, 1500, { command: "npm test" }), say("a", "好了", 2000)], true);
    const open = b.find((n) => n.kind === "step" && n.step.kind === "tool");
    if (open) expect(groupsHolding(b, "tu-bg")).toEqual([]);
    expect(groupsHolding(blocks(items()), "tu-nope")).toBeNull();
    expect(groupsHolding([], "tu-bg")).toBeNull();
  });

  it("looks in the open timeline's inner groups too", () => {
    const g = (id: string, children: WorkNode[]): WorkGroupNode => ({ kind: "group", id, children, live: false });
    const step = blocks(items()).flatMap(function all(n): WorkNode[] { return n.kind === "group" ? n.children.flatMap(all) : [n]; })
      .find((n) => n.kind === "step" && n.step.kind === "tool" && n.step.use.tool_use_id === "tu-bg")!;
    const tree = [g("outer", [g("inner-1", []), g("inner-2", [step])])];
    expect(groupsHolding(tree, "tu-bg")).toEqual(["outer", "inner-2"]);
  });
});

// #3434: a Workflow call is its run's card, not a row; it stays in the stretch it ran in
describe("workflow calls", () => {
  it("is not an ordinary row, and stays in its stretch, live or completed", () => {
    const items = [user("u1", 0), ...tool("grep", "Grep", 1000, 2000, { pattern: "x" }), ...tool("wf", "Workflow", 3000, 3000, { name: "parity-audit" }, "Workflow launched in background"), say("ans", "Started it in the background.", 4000)];
    const [seg] = segments(items);
    const steps = exchangeSteps(seg);
    const wf = steps.find((s) => s.kind === "tool" && s.use.tool_name === "Workflow")!;
    expect(isWorkflowStep(wf)).toBe(true);
    expect(isRowStep(wf)).toBe(false);
    for (const live of [true, false]) expect(shape(workBlocks(steps, { live, turnStart: seg.user?.ts }))).toEqual([["grep", "wf"], "ans"]);
  });
});

describe("workLineOpen (#2602)", () => {
  it("never opens by itself; a choice while it runs lasts while it runs, then the remembered one", () => {
    expect(workLineOpen({ turnLive: true, remembered: false })).toBe(false);
    expect(workLineOpen({ turnLive: true, remembered: false, liveOverride: true })).toBe(true);
    expect(workLineOpen({ turnLive: true, remembered: true, liveOverride: false })).toBe(false);
    expect(workLineOpen({ turnLive: false, remembered: false, liveOverride: true })).toBe(false);
    expect(workLineOpen({ turnLive: false, remembered: true })).toBe(true);
  });
});

describe("workLineStyle (#1614)", () => {
  it("a Persona conversation is assistant style on the assistant page and when opened from the Agents page", () => {
    expect(workLineStyle(true, undefined)).toBe("assistant");
    expect(workLineStyle(false, true)).toBe("assistant");
    expect(workLineStyle(undefined, true)).toBe("assistant");
    expect(workLineStyle(false, false)).toBe("agents");
    expect(workLineStyle(undefined, undefined)).toBe("agents");
  });
});

describe("a tool call's own elapsed time (#8163)", () => {
  const stepOf = (startAt?: number, endAt?: number): ToolStep => {
    const use = mk("assistant", "tool_use", startAt, { id: "c1", tool_name: "Bash", tool_use_id: "tu-c1", tool_input: { command: "npm test" } });
    const result = endAt !== undefined ? mk("user", "tool_result", endAt, { id: "c1-r", tool_use_id: "tu-c1", text: "ok" }) : undefined;
    return { kind: "tool", id: use.id, use, ...(result ? { result } : {}), images: [] };
  };

  it("measures the span purely from the call and result timestamps", () => {
    expect(toolDurationMs(stepOf(1000, 5000))).toBe(4000);
    expect(toolDurationLabel(stepOf(1000, 5000))).toBeNull();
    expect(toolDurationLabel(stepOf(0, 29_999))).toBeNull();
    expect(toolDurationLabel(stepOf(0, 30_000))).toBe("30s");
    expect(toolDurationLabel(stepOf(0, 252_000))).toBe("4m 12s");
    // the same formatter as the group total, including its rounding to whole seconds
    expect(toolDurationLabel(stepOf(0, 65_400))).toBe("1m 5s");
  });

  it("omits a sub-second span and a call without a reliable end", () => {
    expect(toolDurationMs(stepOf(1000, 1400))).toBe(400);
    expect(toolDurationLabel(stepOf(1000, 1400))).toBeNull();
    expect(toolDurationMs(stepOf(1000))).toBeNull();
    expect(toolDurationLabel(stepOf(1000))).toBeNull();
  });

  it("treats an inverted or unparseable pair as no duration, never as a guessed span", () => {
    expect(toolDurationMs(stepOf(5000, 1000))).toBeNull();
    expect(toolDurationLabel(stepOf(5000, 1000))).toBeNull();
    const noStart = stepOf(undefined, 5000);
    expect(toolDurationMs(noStart)).toBeNull();
    expect(toolDurationLabel(noStart)).toBeNull();
  });
});
