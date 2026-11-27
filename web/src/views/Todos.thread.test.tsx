// @vitest-environment jsdom
// The todo page marks the row of a todo bound to a chat thread (#5279), the thread's line "Lark topic · <title>" as its
// tooltip. The main conversation binds and unbinds threads: the page has no section or button for it. A hub from before
// threads (no `todo_threads` feature in hello) shows nothing of it. The hub is faked; the data made up.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Todo } from "../lib/todos";

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
const { TodosView } = await import("./Todos");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const marks = () => [...document.querySelectorAll<HTMLElement>("[data-testid=todo-thread-mark]")];

const iso = (agoMs: number) => new Date(Date.now() - agoMs).toISOString();
const todo = (id: string, title: string, patch: Partial<Todo> = {}): Todo => ({ id, title, created_at: iso(7_200_000), updated_at: iso(3_600_000), ...patch });
const bound = { channel: "feishu", chat_id: "oc_test", thread_id: "omt_test", title: "测试话题" };

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };

const open = async (id: string) => {
  await act(async () => root.render(<TodosView selectedId={id} />));
  await flush();
};

beforeEach(() => {
  api.mockReset();
  api.mockImplementation(async (path: string) => (path.endsWith("/decisions?all=1") ? { decisions: [] } : {}));
  seed({ todos: [todo("t1", "Release checklist", { thread: bound }), todo("t2", "deploy web")], todosLoaded: true, machines: [], sessions: {}, features: ["todo_threads"] });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  document.body.innerHTML = "";
  seed(before);
  vi.restoreAllMocks();
});

describe("a todo's chat thread", () => {
  it("marks only the bound todo's row, the thread named in its tooltip, with nothing to bind or unbind on the page", async () => {
    await open("t1");
    expect(marks().map((m) => m.closest<HTMLElement>("[data-todo]")?.dataset.todo)).toEqual(["t1"]);
    expect(marks()[0].getAttribute("title")).toBe("飞书话题 · 测试话题");
    expect(document.querySelector("[data-testid=todo-thread]")).toBeNull();
    expect([...document.querySelectorAll("button")].some((b) => b.textContent?.trim() === "解绑")).toBe(false);
  });

  it("names the thread by its short id when it has no title", async () => {
    seed({ todos: [todo("t1", "Release checklist", { thread: { ...bound, title: "", thread_id: "omt_1a2b3c4d5e6f" } })] });
    await open("t1");
    expect(marks()[0].getAttribute("title")).toBe("飞书话题 · omt_1a…5e6f");
  });

  it("shows nothing of threads when the hub does not say it has them", async () => {
    seed({ features: ["locale"] });
    await open("t1");
    expect(marks()).toHaveLength(0);
  });
});
