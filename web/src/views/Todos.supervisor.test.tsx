// @vitest-environment jsdom
// A to-do a supervisor Agent runs (lib/todo-supervisor.ts): its supervisor first among the linked sessions with the
// workers it handed out beneath it, the Supervision log between the criteria and the sessions, and a worker's way back
// to its root. The hub is faked; the data made up.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Todo, TodoAgent } from "../lib/todos";
import type { Session } from "../lib/types";

vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia;
});

const api = vi.fn(async (_path: string, _init?: unknown): Promise<unknown> => ({}));
vi.mock("../lib/api", async (orig) => ({
  ...(await orig<typeof import("../lib/api")>()),
  api: (path: string, init?: unknown) => api(path, init),
}));

const { getState } = await import("../lib/store");
const { dayLabel } = await import("../lib/feed");
const { TodosView } = await import("./Todos");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
// the clock is held at noon, so "today" is never cut by midnight
const NOW = new Date(2026, 10, 28, 12);
const iso = (agoMs: number) => new Date(NOW.getTime() - agoMs).toISOString();
const DAY = 86_400_000;

const agent = (mid: string, sid: string, patch: Partial<TodoAgent> = {}): TodoAgent => ({ machine_id: mid, session_id: sid, linked_at: iso(DAY * 3), ...patch });
const todo = (id: string, title: string, patch: Partial<Todo> = {}): Todo => ({ id, title, created_at: iso(DAY * 3), updated_at: iso(60_000), ...patch });
const session = (mid: string, sid: string, title: string, state: Session["state"]) => ({ machine_id: mid, session_id: sid, title, status: state, state }) as Session;

const lead = "m1/s-lead";
const release = todo("root", "Ship the release notes", { agents: [agent("m1", "s-lead", { source: "leader" })], supervision: { supervisor: "main" } });
const tests = todo("w-tests", "Fix the flaky tests", {
  created_at: iso(30_000),
  agents: [agent("m2", "s-tests", { source: "worker", via: "root" })],
  supervision: { supervisor: lead, state: "rework", round: 2, reviews: [{ at: iso(20_000), verdict: "rework", round: 2, note: "The retry loop is still there." }] },
});
const parser = todo("w-parser", "Write the parser", {
  agents: [agent("m2", "s-parser", { source: "worker", via: "root" })],
  supervision: { supervisor: lead, state: "accepted", reviews: [{ at: iso(DAY * 2), verdict: "accepted" }] },
  done_at: iso(DAY * 2),
  done_by: "review",
});

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };
const sections = () => [...document.querySelectorAll<HTMLElement>("[data-todo-section]")];
const open = async (id: string) => {
  history.replaceState(null, "", `/todos?id=${id}`);
  await act(async () => root.render(<TodosView selectedId={id} />));
  await flush();
};

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: NOW });
  api.mockReset();
  api.mockImplementation(async (path: string) => {
    if (path.endsWith("/decisions?all=1")) return { decisions: [] };
    const repositories = /^\/todos\/([^/]+)\/repositories$/.exec(path);
    if (repositories) return { schema_version: 1, manager: "assistant", todo_id: repositories[1], revision: 0, repositories: [] };
    return {};
  });
  seed({
    todos: [release, tests, parser],
    todosLoaded: true,
    machines: [{ id: "m1", name: "Desk computer", online: true }, { id: "m2", name: "Build box", online: true }],
    sessions: { m1: [session("m1", "s-lead", "Supervise the release", "working")], m2: [session("m2", "s-tests", "Tests worker", "working")] },
    interactions: [],
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  document.body.innerHTML = "";
  seed(before);
  history.replaceState(null, "", "/");
  vi.useRealTimers();
});

describe("a to-do a supervisor Agent runs", () => {
  it("pins the supervisor first among the linked sessions, its workers beneath it", async () => {
    await open("root");
    const first = document.querySelector("[data-testid=todo-agent-roster] li")!;
    expect(first.querySelector("[data-testid=todo-agent-source]")?.textContent).toBe("督办");
    expect(first.querySelector("[data-testid=todo-agent-title]")?.textContent).toBe("Supervise the release");
    const tree = first.querySelector("[data-testid=todo-workers]")!;
    // the open worker is listed; the accepted one folds under Closed
    expect([...tree.querySelectorAll("[data-testid=todo-worker-title]")].map((x) => x.textContent)).toEqual(["Fix the flaky tests"]);
    expect(tree.querySelector("[data-testid=todo-worker-state]")?.textContent).toBe("返工第 2 轮 · Build box");
    const closed = [...tree.querySelectorAll("button")].find((b) => b.textContent === "已结束 · 1")!;
    await act(async () => closed.click());
    expect(tree.querySelector('[data-worker-step="accepted"]')?.textContent).toContain("Write the parser");
  });

  it("keeps the worker hierarchy under the former supervisor after release", async () => {
    seed({ todos: [{ ...release, done_at: iso(1000), agents: [{ ...release.agents![0], released_at: iso(1000) }] }, tests, parser] });
    await open("root");
    const first = document.querySelector("[data-testid=todo-agent-roster] li")!;
    expect(first.querySelector("[data-testid=todo-agent-title]")?.textContent).toBe("Supervise the release");
    expect(first.querySelector("[data-testid=todo-workers]")).not.toBeNull();
    await act(async () => first.querySelector<HTMLButtonElement>("button")!.click());
    expect(location.pathname).toBe("/s/m1/s-lead");
  });

  it("opens the worker's conversation from the supervisor's to-do", async () => {
    await open("root");
    const workerChat = document.querySelector<HTMLButtonElement>('[aria-label="查看 Tests worker 的 Agent 会话"]')!;
    await act(async () => workerChat.click());
    expect(location.pathname).toBe("/s/m2/s-tests");
    expect(api.mock.calls.some(([, init]) => (init as { method?: string } | undefined)?.method === "POST")).toBe(false);
  });

  it("logs the supervision between the criteria and the sessions, today open and newest first", async () => {
    await open("root");
    expect(sections().map((s) => s.querySelector("h3")?.textContent)).toEqual(["任务简报", "验收标准", "督办记录", "关联的 Agent 会话", "仓库与关联事项", "决策"]);
    const log = document.querySelector("[data-testid=todo-section-supervision]")!;
    expect(log.querySelector("[data-testid=todo-supervision-count]")?.textContent).toBe("4");
    const days = [...log.querySelectorAll("[data-supervision-day] > button")];
    expect(days.map((d) => [d.getAttribute("aria-expanded"), d.textContent])).toEqual([
      ["true", `${dayLabel(new Date(2026, 10, 28))}2`],
      ["false", `${dayLabel(new Date(2026, 10, 26))}1`],
      ["false", `${dayLabel(new Date(2026, 10, 25))}1`],
    ]);
    expect(days[0].textContent).toMatch(/^今天 · /);
    expect([...log.querySelectorAll("[data-supervision-entry]")].map((e) => e.textContent)).toEqual([
      "返工第 2 轮 · Fix the flaky tests11:59The retry loop is still there.",
      "派出 · Fix the flaky tests11:59",
    ]);
  });

  it("opens a worker's to-do from its line", async () => {
    await open("root");
    await act(async () => (document.querySelector('[data-testid=todo-workers] [data-open-todo="w-tests"]') as HTMLElement).click());
    await flush();
    expect(location.search).toBe("?id=w-tests");
    expect(document.querySelector("[data-testid=todo-detail-title]")?.textContent).toBe("Fix the flaky tests");
  });

  it("leads from a worker's page back to the to-do it works for", async () => {
    await open("w-tests");
    expect(document.querySelector("[data-testid=todo-agent-source]")?.textContent).toBe("执行");
    const back = document.querySelector("[data-testid=todo-supervised-by]")!;
    expect(back.textContent).toBe("由 Ship the release notes 督办");
    expect(document.querySelector("[data-testid=todo-section-supervision] [data-supervision-entry]")?.textContent).toMatch(/^返工第 2 轮\d\d:\d\d/);
    await act(async () => (back.querySelector("button") as HTMLElement).click());
    await flush();
    expect(location.search).toBe("?id=root");
  });
});


it("shows persisted Agent activity and an explicit coverage gap across pages", async () => {
  seed({ features: ["agent_lifecycle_v1"] });
  api.mockImplementation(async (path: string) => {
    if (path === "/todos/root/lifecycle") return {
      events: [{ revision: "42", machine_id: "m1", session_id: "s-lead", kind: "turn.finished", confidence: "confirmed", occurred_at: iso(2000) }],
      next: "42",
    };
    if (path === "/todos/root/lifecycle?before=42") return {
      events: [{ revision: "40", machine_id: "m1", session_id: "", kind: "coverage.gap", confidence: "observed", occurred_at: iso(3000) }],
    };
    if (path.endsWith("/decisions?all=1")) return { decisions: [] };
    return {};
  });
  await open("root");
  const activity = document.querySelector("[data-testid=todo-section-lifecycle]")!;
  expect(activity.textContent).toContain("轮次已结束");
  expect(activity.textContent).toContain("Supervise the release");
  const earlier = [...activity.querySelectorAll("button")].find((button) => button.textContent === "显示更早的活动")!;
  await act(async () => earlier.click());
  await flush();
  expect(activity.textContent).toContain("部分活动记录缺失");
  expect(activity.textContent).toContain("轮次已结束");
  expect(api).toHaveBeenCalledWith("/todos/root/lifecycle?before=42", expect.anything());
});
