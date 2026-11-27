// @vitest-environment jsdom
// The To-dos page is read only: the main conversation and the agents write every to-do, and the owner reads it. The
// detail is its header and five sections in a fixed order (brief, acceptance criteria, agent sessions, repositories,
// decisions), each saying so while nothing is written there yet; nothing on the page edits, closes, hands over or
// deletes a to-do, and no due date, reminder or priority is shown. The hub is faked; the data made up.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Todo } from "../lib/todos";

vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false, media: query, onchange: null,
    addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false,
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
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
const iso = (agoMs: number) => new Date(Date.now() - agoMs).toISOString();
const todo = (id: string, title: string, patch: Partial<Todo> = {}): Todo => ({ id, title, created_at: iso(7_200_000), updated_at: iso(600_000), ...patch });

const planned = todo("planned", "Renew the certificate", {
  notes: "Checked the **staging** host first.",
  due_at: iso(86_400_000),
  priority: "high",
  remind_at: iso(-3_600_000),
});
const briefed = todo("briefed", "Fix the login redirect", {
  brief: { id: "b1", created_at: iso(7_200_000), started_by: "owner", goal: "Users land on the page they asked for", done_when: ["The redirect keeps the path", "A test covers it"] },
});
const plain = todo("plain", "Water the plants");

const seed = (patch: Record<string, unknown>) => Object.assign(getState() as unknown as Record<string, unknown>, patch);
const before = { ...getState() };
const detail = () => host.querySelector<HTMLElement>("[data-testid=todo-detail]")!;
const sections = () => [...detail().querySelectorAll<HTMLElement>("[data-todo-section]")];
const heading = (section: HTMLElement) => section.querySelector("h3")?.textContent;
const writes = () => api.mock.calls.filter(([, init]) => (init as { method?: string } | undefined)?.method);
const open = async (id: string) => {
  await act(async () => root.render(<TodosView selectedId={id} />));
  await act(async () => { await vi.dynamicImportSettled(); });
  await flush();
};

beforeEach(() => {
  api.mockReset();
  api.mockImplementation(async (path: string) => {
    const repositories = /^\/todos\/([^/]+)\/repositories$/.exec(path);
    if (repositories) return { schema_version: 1, manager: "assistant", todo_id: repositories[1], revision: 0, repositories: [] };
    if (path.endsWith("/decisions?all=1")) return { decisions: [] };
    return {};
  });
  seed({ todos: [planned, briefed, plain], todosLoaded: true, machines: [], sessions: {}, interactions: [], features: [] });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  document.body.innerHTML = "";
  seed(before);
});

describe("a to-do's page", () => {
  it("is its header and the five sections, in order", async () => {
    await open("plain");
    expect(detail().querySelector("[data-testid=todo-detail-title]")?.textContent).toBe("Water the plants");
    expect(sections().map(heading)).toEqual(["任务简报", "验收标准", "关联的 Agent 会话", "仓库与关联事项", "决策"]);
  });

  it("says in each section the agents have not written yet that nothing is there", async () => {
    await open("plain");
    const empty = (i: number) => sections()[i].querySelector("[data-testid=todo-section-empty]")?.textContent;
    expect(empty(0)).toBe("主对话还没有写任务简报。");
    expect(empty(1)).toBe("主对话还没有设定验收标准。");
    expect(sections()[2].textContent).toContain("还没有关联的 Agent 会话。");
    expect(sections()[3].textContent).toContain("尚未关联仓库");
    expect(empty(4)).toContain("还没有记录决策。");
  });

  it("puts the brief's criteria under Acceptance criteria, not in the brief", async () => {
    await open("briefed");
    expect(sections()[0].querySelector("[data-testid=todo-section-empty]")).toBeNull();
    expect(sections()[0].textContent).not.toContain("The redirect keeps the path");
    const criteria = sections()[1].querySelector("[data-testid=todo-brief-criteria]")!;
    expect([...criteria.querySelectorAll("li")].map((li) => li.textContent)).toEqual(["The redirect keeps the path", "A test covers it"]);
  });

  it("shows the title and the notes as text, with no field, menu or action that changes the to-do", async () => {
    await open("planned");
    const page = detail();
    expect(page.querySelectorAll("input, textarea, select, [contenteditable=true]")).toHaveLength(0);
    // the notes, rendered as Markdown under the brief
    const notes = page.querySelector("[data-testid=todo-notes]")!;
    expect(notes.querySelector("strong")?.textContent).toBe("staging");
    // the old page's ⋯ menu, Hand to agent, Complete, Link existing agents, Add repository, Record one
    expect(page.querySelector("[aria-label='更多选项']")).toBeNull();
    const labels = [...page.querySelectorAll("button")].map((b) => b.textContent?.trim());
    for (const gone of ["交给 Agent", "完成", "重新打开", "关联已有 Agent", "添加仓库", "记一条", "清除", "解绑"]) expect(labels).not.toContain(gone);
    expect(writes()).toEqual([]);
  });

  it("shows no due date, reminder or priority", async () => {
    await open("planned");
    expect(detail().querySelector("[data-testid=todo-plan]")).toBeNull();
    expect(detail().textContent).not.toMatch(/截止|提醒|优先级/);
  });
});

describe("the list", () => {
  it("marks each row done or open without a control, and shows no due date or priority on it", async () => {
    await open("plain");
    const row = host.querySelector<HTMLElement>("[data-todo=planned]")!;
    const mark = row.querySelector<HTMLElement>("[data-testid=todo-done-mark]")!;
    expect(mark.tagName).toBe("SPAN");
    expect(mark.getAttribute("aria-hidden")).toBe("true");
    expect(row.querySelector("button")).toBeNull();
    expect(row.querySelector("[data-testid=todo-plan-line]")).toBeNull();
    expect(row.textContent).not.toMatch(/截止|高优先级/);
    // a right click is the browser's: no menu that completes, plans or deletes it
    await act(async () => row.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    expect(document.querySelector("[role=menu]")).toBeNull();
    expect(writes()).toEqual([]);
  });

  it("keeps search and the groupings, with nothing that creates a to-do or switches to a calendar", async () => {
    await open("plain");
    const header = host.querySelector("header")!;
    expect(header.querySelectorAll("button")).toHaveLength(0);
    const column = host.querySelector<HTMLElement>("[data-testid=todos-list-column]")!;
    expect(column.querySelector("input")).toBeTruthy(); // the search field
    const labels = [...host.querySelectorAll("button")].map((b) => b.textContent?.trim());
    expect(labels).toEqual(expect.arrayContaining(["状态", "目录"]));
    for (const gone of ["新建待办", "日历", "列表"]) expect(labels).not.toContain(gone);
    expect(host.querySelector("[aria-label='筛选与排序']")).toBeNull();
  });
});
