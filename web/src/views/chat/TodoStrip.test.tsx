// @vitest-environment jsdom
// The to-do strip in an Agent chat: its one line for a to-do in progress, one that needs the owner, one with no
// criteria, a finished one and one the hub sent no row for; and its details, which prepare a message in this Agent's
// composer instead of sending one.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextCriterionMessage } from "../../lib/todo-steer";
import { todoStrip, type TodoStripModel } from "../../lib/todo-strip";
import type { Todo } from "../../lib/todos";
import type { Session } from "../../lib/types";
import { TodoStrip, TodoStripView } from "./TodoStrip";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

const drafts = vi.hoisted(() => ({ sent: [] as [string, unknown][] }));
vi.mock("../../lib/drafts", async (original) => ({
  ...(await original<typeof import("../../lib/drafts")>()),
  sendDraft: (key: string, e: unknown) => {
    drafts.sent.push([key, e]);
    return true;
  },
}));
vi.mock("../../lib/todos", async (original) => ({
  ...(await original<typeof import("../../lib/todos")>()),
  fetchTodoDecisions: async () => [],
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const NOW = Date.parse("2026-11-20T12:00:00Z");
const session: Session = { machine_id: "mac", session_id: "s1", title: "Widget fix", cwd: "/home/me/app", status: "running", todo: { id: "todo_1", title: "Fix the widget" } };
const todo = (patch: Partial<Todo> = {}): Todo => ({
  id: "todo_1",
  title: "Fix the widget",
  created_at: "2026-11-20T08:00:00Z",
  updated_at: "2026-11-20T11:00:00Z",
  agents: [{ machine_id: "mac", session_id: "s1", linked_at: "2026-11-20T08:00:00Z" }],
  ...patch,
});
const criteria = {
  supervisor: "conv_main",
  done_when: ["The fix is merged", "A snapshot test covers landscape"],
  criteria: [{ criterion: "The fix is merged", state: "met" as const }],
};

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  drafts.sent = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const render = (model: TodoStripModel, canSteer = true) => act(() => root.render(<TodoStripView model={model} session={session} canSteer={canSteer} />));
const strip = () => host.querySelector<HTMLElement>("[data-todo-strip]");
const text = (sel: string) => host.querySelector(sel)?.textContent ?? null;
const button = (label: string) => [...host.querySelectorAll("button")].find((b) => b.textContent === label);

describe("the to-do strip", () => {
  it("takes no space for a session with no to-do", () => {
    act(() => root.render(<TodoStrip session={{ ...session, todo: undefined }} canSteer />));
    expect(host.innerHTML).toBe("");
  });

  it("in progress: its title, its stage and the first criterion not met, quiet, folded", () => {
    render(todoStrip(session.todo!, todo({ stage: "develop", supervision: criteria }), undefined, session, NOW));
    expect(strip()!.dataset.todoStrip).toBe("open");
    expect(text("[data-todo-strip-title]")).toBe("Fix the widget");
    expect(text("[data-todo-strip-next]")).toBe("下一步：A snapshot test covers landscape");
    expect(host.querySelector("[data-todo-strip-needs]")).toBeNull();
    expect(host.querySelector('[data-stage="develop"]')!.getAttribute("data-state")).toBe("current");
    expect(host.querySelector('[data-stage="plan"]')!.getAttribute("data-state")).toBe("done");
    const toggle = host.querySelector<HTMLButtonElement>("button[aria-expanded]")!;
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(host.querySelector("[data-todo-strip-detail]")).toBeNull();
  });

  it("needs you: says so in words beside the mark, with what it is asked", () => {
    render(todoStrip(session.todo!, todo({ ask: { outcome: "decide", verdict: "Two ways", question: "Ship behind a flag?", asked_at: "2026-11-20T11:00:00Z" } }), "owner_decides", session, NOW));
    expect(text("[data-todo-strip-needs]")).toBe("需要你");
    expect(text("[data-todo-strip-next]")).toBe("等你决定：Ship behind a flag?");
  });

  it("no criteria: its title and stage only", () => {
    render(todoStrip(session.todo!, todo({ stage: "review" }), undefined, session, NOW));
    expect(text("[data-todo-strip-title]")).toBe("Fix the widget");
    expect(text("[data-todo-strip-next]")).toBe("进行中");
    expect(host.querySelector('[data-stage="review"]')!.getAttribute("data-state")).toBe("current");
  });

  it("done: the finished form, with one action that opens the to-do", () => {
    render(todoStrip(session.todo!, todo({ done_at: "2026-11-20T11:30:00Z" }), undefined, session, NOW));
    expect(strip()!.dataset.todoStrip).toBe("finished");
    expect(strip()!.textContent).toContain("已完成");
    expect(host.querySelectorAll("button")).toHaveLength(1);
    expect(button("打开待办")).toBeTruthy();
  });

  it("no detail from the hub: the title and a link", () => {
    render(todoStrip(session.todo!, undefined, undefined, session, NOW));
    expect(strip()!.dataset.todoStrip).toBe("missing");
    expect(strip()!.textContent).toContain("Fix the widget");
    expect(button("打开待办")).toBeTruthy();
  });

  it("opens its details on a click and prepares, never sends, a message in this Agent's composer", async () => {
    const model = todoStrip(session.todo!, todo({ supervision: criteria }), undefined, session, NOW);
    render(model);
    await act(async () => {
      host.querySelector<HTMLButtonElement>("button[aria-expanded]")!.click();
      await import("./TodoStripDetail");
    });
    await act(async () => {});
    expect(host.querySelector("[data-todo-strip-detail]")).not.toBeNull();
    expect([...host.querySelectorAll("[data-criterion]")].map((li) => li.getAttribute("data-mark"))).toEqual(["met", "open"]);
    // only the criterion not met offers "Do this next"
    const next = [...host.querySelectorAll<HTMLButtonElement>("[data-criterion] button")];
    expect(next).toHaveLength(1);
    act(() => next[0].click());
    if (model.form !== "open") throw new Error("open");
    const facts = { id: model.id, title: model.title, stage: model.stage, criteria: model.criteria };
    expect(drafts.sent).toEqual([["mac/s1", { type: "replace", text: nextCriterionMessage(facts, model.criteria[1]) }]]);
  });

  it("offers no steering into a composer that cannot send, only the main conversation and the to-do", async () => {
    render(todoStrip(session.todo!, todo({ supervision: criteria }), undefined, session, NOW), false);
    await act(async () => {
      host.querySelector<HTMLButtonElement>("button[aria-expanded]")!.click();
      await import("./TodoStripDetail");
    });
    await act(async () => {});
    expect(button("汇报进度")).toBeUndefined();
    expect(button("继续推进")).toBeUndefined();
    expect(host.querySelectorAll("[data-criterion] button")).toHaveLength(0);
    expect(button("交给主对话")).toBeTruthy();
    expect(drafts.sent).toEqual([]);
  });
});
