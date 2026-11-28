// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getState } from "../lib/store";
import { boardSectionOf } from "../lib/task-board";
import { awaitsReview, openAsk, sortTodos, type Todo, type TodoAsk } from "../lib/todos";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

const api = vi.hoisted(() => vi.fn());
vi.mock("../lib/api", async (orig) => ({ ...(await orig<typeof import("../lib/api")>()), api }));
const navigate = vi.hoisted(() => vi.fn());
vi.mock("../lib/router", async (orig) => ({ ...(await orig<typeof import("../lib/router")>()), navigate }));
const toast = vi.hoisted(() => vi.fn());
vi.mock("../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../ui/atoms")>()), toast }));

const { TodosView } = await import("./Todos");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const ASKED = "2026-11-09T06:30:00.123456789Z";
const now = new Date().toISOString();
const decide: TodoAsk = { outcome: "decide", verdict: "🤔 需要你决定：两个方案各有取舍", recommend: "先发预发环境", asked_at: ASKED };
const todo = (o: Partial<Todo> = {}): Todo => ({ id: "t1", title: "上线新登录页", created_at: now, updated_at: now, origin: "conv1", ask: decide, ...o });

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };

let root: Root;
let host: HTMLDivElement;
// what GET /todos answers after a take: the to-do with nothing waiting
let listed: Todo[] = [];
beforeEach(() => {
  api.mockReset();
  navigate.mockReset();
  toast.mockReset();
  listed = [todo({ ask: undefined })];
  api.mockImplementation(async (path: string) => (path === "/todos" ? { todos: listed } : {}));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  seed(before);
});

const flush = () => act(async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
});
const drawList = async (todos: Todo[], selectedId?: string) => {
  seed({ todos, todosLoaded: true, sessions: {}, interactions: [] });
  act(() => root.render(<TodosView selectedId={selectedId} />));
  await flush();
};
const askRow = () => host.querySelector<HTMLElement>("[data-testid=todo-ask]");
const askButton = (within: ParentNode = host) => within.querySelector<HTMLButtonElement>("[data-testid=todo-ask] button");

describe("an ask, as the hub sends it", () => {
  it("is open only with a verdict and a round", () => {
    expect(openAsk(todo())).toEqual(decide);
    expect(openAsk(todo({ ask: undefined }))).toBeUndefined();
    expect(openAsk(todo({ ask: { verdict: "", asked_at: ASKED } }))).toBeUndefined();
    expect(openAsk(todo({ ask: { verdict: "✅ 已上线", asked_at: "" } }))).toBeUndefined();
  });

  it("puts a stopped owner decision with Waiting for you, ahead of what runs", () => {
    const asked = todo();
    expect(awaitsReview(asked, "follow_up")).toBe(false);
    expect(awaitsReview(asked, "working")).toBe(false); // its agent is at it again
    expect(awaitsReview(todo({ ask: undefined }), "follow_up")).toBe(false);
    expect(boardSectionOf({ todo: asked, status: "follow_up" })).toBe("waiting");
    const review = todo({ ask: { ...decide, outcome: undefined } });
    expect(awaitsReview(review, "follow_up")).toBe(true);
    expect(boardSectionOf({ todo: review, status: "follow_up" })).toBe("review");
    const working = todo({ id: "t2", ask: undefined, created_at: new Date(Date.now() + 1000).toISOString(), agents: [{ machine_id: "m1", session_id: "s1", linked_at: now }] });
    // its agent working (t2), and the asked one's stopped and seen (t1)
    const lookup = (_m: string, s: string) => ({ machine_id: "m1", session_id: s, status: s === "s1" ? "working" : "idle", state: s === "s1" ? "working" : "idle" }) as never;
    expect(sortTodos([working, { ...asked, agents: [{ machine_id: "m1", session_id: "s0", linked_at: now }] }], lookup).map((t) => t.id)).toEqual(["t1", "t2"]);
  });
});

describe("a to-do's row on the To-dos page", () => {
  it("says what it waits on and the recommendation, with nothing to tap", async () => {
    await drawList([
      // distinct titles: to-dos with the same title share one row (#3156)
      todo(),
      todo({ id: "t2", title: "接入开放接口", ask: { outcome: "cannot", verdict: "❌ 做不了：接口不开放", asked_at: ASKED } }),
      todo({ id: "t3", title: "上线新版本", ask: { verdict: "✅ 做完了，已上线", asked_at: ASKED } }),
    ]);
    const rows = [...host.querySelectorAll<HTMLElement>("[data-testid=todo-ask]")];
    expect(rows.map((r) => r.dataset.ask).sort()).toEqual(["cannot", "decide", "deployed"]);
    expect(askRow()!.textContent).toContain("需要你决定：两个方案各有取舍");
    expect(askRow()!.textContent).toContain("推荐：先发预发环境");
    for (const r of rows) expect(askButton(r)).toBeNull();
    expect(api.mock.calls.some(([, init]) => init?.method && init.method !== "GET")).toBe(false);
  });
});

describe("a to-do's page", () => {
  it("lists parked work outside Done and counts it under the badge's name", async () => {
    await drawList([todo({
      seen_at: new Date(Date.now() + 1000).toISOString(),
      ask: { outcome: "done", verdict: "The previous turn ended.", asked_at: now },
      supervision: {
        supervisor: "conv1", state: "parked",
        parked: { reason: "Waiting on another team", waiting_on: "someone_else", until: new Date(Date.now() + 86400_000).toISOString(), count: 1 },
      },
    })]);
    expect(host.querySelector('[data-section="parked"]')?.textContent).toContain("上线新登录页");
    expect(host.querySelector('[data-section="done"]')?.textContent ?? "").not.toContain("上线新登录页");
    expect(host.querySelector("header")?.textContent).toContain("已搁置 1");
    expect(host.querySelector("header")?.textContent).toContain("暂无进展 0");
  });

  it("shows an open decision as text with no action or composer", async () => {
    await drawList([todo()], "t1");
    const panel = host.querySelector("[data-testid=todo-ask-panel]")!;
    expect(panel.textContent).toContain(todo().ask!.verdict);
    expect(panel.querySelector("button, a, textarea, input")).toBeNull();
    expect(api.mock.calls.some(([, init]) => init?.method && init.method !== "GET")).toBe(false);
  });

  it("shows nothing while the to-do waits on nothing", async () => {
    await drawList([todo({ ask: undefined })], "t1");
    expect(host.querySelector("[data-testid=todo-ask-panel]")).toBeNull();
  });
});
