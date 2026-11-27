// @vitest-environment jsdom
// #3713: a to-do whose agent never started (the hub closes it, with its start error) waits on the owner in Needs you. Its
// row reads open, not done, and says why in red. One whose work finished in a related to-do (the same plan) is history:
// Done, nothing asked. Its page agrees with its row (audit item 13): why it did not start, and nothing that changes it:
// the main conversation retries it or closes it. The hub is faked; the data made up.
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
// The unrelated association section has a valid empty store, so its error Retry cannot shadow the task's Retry.
const defaultReply = (path: string) => {
  const repositoryRead = path.match(/^\/todos\/([^/]+)\/repositories(?:\?|$)/);
  return repositoryRead
    ? { schema_version: 1, manager: "assistant", todo_id: repositoryRead[1], revision: 0, repositories: [] }
    : {};
};
vi.mock("../lib/api", async (orig) => ({
  ...(await orig<typeof import("../lib/api")>()),
  api: (path: string, init?: unknown) => api(path, init),
}));

const { getState } = await import("../lib/store");
const { TodosView } = await import("./Todos");
const { t } = await import("../i18n");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};

const iso = (agoMs: number) => new Date(Date.now() - agoMs).toISOString();
const failed = (id: string, title: string, plan?: string, closedAgo = 3_600_000): Todo =>
  ({ id, title, plan_id: plan, origin: "conv", created_at: iso(closedAgo + 3_600_000), updated_at: iso(closedAgo), done_at: iso(closedAgo), start_error: "machine offline", agents: [] }) as Todo;
const finished: Todo = { id: "done", title: "Verify the panel", plan_id: "p", origin: "conv", created_at: iso(7_200_000), updated_at: iso(1_800_000), done_at: iso(1_800_000), done_by: "review", agents: [] } as Todo;

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };

beforeEach(async () => {
  api.mockReset();
  api.mockImplementation(async (path) => defaultReply(path));
  seed({
    todos: [failed("waits", "Rotate the keys"), failed("gone", "Deploy the panel", "p"), finished, failed("again", "Renew the certificate", "p2"), failed("old", "Clean the build cache", undefined, 3 * 86_400_000)],
    todosLoaded: true, sessions: {}, interactions: [], answered: {}, machines: [{ id: "m1", name: "Example box", online: true }],
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => root.render(<TodosView />));
  await flush();
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  document.body.innerHTML = "";
  seed(before);
});

const section = (name: string) => host.querySelector(`[data-testid=todo-section][data-section=${name}]`);
/** Every request that would change something on the hub. */
const writes = () => api.mock.calls.filter(([, init]) => (init as { method?: string } | undefined)?.method && (init as { method?: string }).method !== "GET");
const show = async (id: string) => {
  await act(async () => root.render(<TodosView selectedId={id} />));
  await flush();
};
const row = (id: string) => host.querySelector<HTMLElement>(`[data-testid=todo-triage-row][data-id=${id}]`);

describe("a to-do whose agent never started", () => {
  it("waits in Needs you reading open, says why in red, and its mark changes nothing", async () => {
    const waits = row("waits")!;
    expect(section("waiting")?.contains(waits)).toBe(true);
    expect(waits.querySelector(".line-through")).toBeNull();
    expect(waits.querySelector("[data-testid=todo-start-failed]")?.textContent).toContain("machine offline");
    const mark = waits.querySelector<HTMLElement>("[data-testid=todo-done-mark]")!;
    expect(mark.tagName).toBe("SPAN");
    expect(mark.hasAttribute("data-done")).toBe(false);
    await act(async () => mark.click());
    await flush();
    expect(writes()).toEqual([]);
  });

  it("goes with the work that finished in its row, behind it and immediately Done", async () => {
    expect(section("waiting")?.querySelector("[data-id=gone]")).toBeNull();
    const done = section("done")!;
    expect(done.getAttribute("data-todos")).toBe("3");
    await act(async () => done.querySelector("button")!.click());
    const r = row("done")!;
    expect(done.contains(r)).toBe(true);
    expect(r.getAttribute("data-members")).toBe("2");
  });
});

describe("the page of a to-do whose agent never started (audit item 13)", () => {
  it("says why it did not start, and offers nothing that retries, hands over or closes it", async () => {
    await show("waits");
    const detail = host.querySelector("[data-testid=todos-detail-column]")!;
    expect(detail.querySelector("[data-testid=todo-detail-start-failed]")?.textContent).toContain("machine offline");
    expect(detail.textContent).toContain(t("todos.no_agents"));
    const labels = [...detail.querySelectorAll("button")].map((b) => b.textContent?.trim());
    // the old page's Retry, Hand to agent, Complete and Reopen
    for (const gone of ["重试", "交给 Agent", "完成", "重新打开"]) expect(labels).not.toContain(gone);
    expect(writes()).toEqual([]);
  });

  it("reads done once it left Needs you (48 hours on): no start failure is said", async () => {
    await show("old");
    expect(host.querySelector("[data-testid=todo-detail-start-failed]")).toBeNull();
    expect(host.querySelector("[data-testid=todo-detail]")!.textContent).toContain(t("todos.no_agents_done"));
  });
});
