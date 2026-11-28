// @vitest-environment jsdom
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { makeSessionLookup, type Todo } from "../../lib/todos";
import type { Session } from "../../lib/types";
import { TodoStrip } from "./TodoStrip";

const data = vi.hoisted(() => ({ todos: [] as Todo[] }));
vi.mock("../../lib/store", () => ({ useStore: (select: (state: typeof data) => unknown) => select(data) }));
vi.mock("../../lib/todo-owner-needs", () => ({ useTodoLookup: () => makeSessionLookup([session]) }));

const session: Session = { machine_id: "box", session_id: "s1", title: "Widget fix", cwd: "/work/app", status: "running", state: "working",
  todo: { id: "t1", title: "Fix the widget" } };
const todo: Todo = { id: "t1", title: "Fix the widget", created_at: "2026-11-20T08:00:00Z", updated_at: "2026-11-20T11:00:00Z",
  agents: [{ machine_id: "box", session_id: "s1", linked_at: "2026-11-20T08:00:00Z" }], stage: "develop" };

function render(current = session) {
  const host = document.createElement("div");
  host.innerHTML = renderToStaticMarkup(<TodoStrip session={current} />);
  return host;
}

describe("the linked to-do below an Agent's session header", () => {
  beforeEach(() => { data.todos = [todo]; });

  it("takes no space when the session has no to-do", () => {
    expect(render({ ...session, todo: undefined }).innerHTML).toBe("");
  });

  it("shows current status and stage, with the title as its only control", () => {
    const strip = render();
    const links = strip.querySelectorAll("a");
    expect(links).toHaveLength(1);
    expect(links[0].textContent).toBe("Fix the widget");
    expect(links[0].getAttribute("href")).toBe("/todos?id=t1");
    expect(strip.querySelector("[data-todo-strip-status]")?.textContent).toBe("工作中");
    expect(strip.querySelector('[data-stage="develop"]')?.getAttribute("data-state")).toBe("current");
    expect(strip.querySelector("button, [role=menu], input, textarea")).toBeNull();
  });

  it("links to a completed or not yet loaded to-do using the session's data", () => {
    data.todos = [];
    const current = render();
    const completed = render({ ...session, todo: undefined, completed_todo: session.todo });
    expect(current.querySelector("a")?.getAttribute("href")).toBe("/todos?id=t1");
    expect(completed.querySelector("a")?.getAttribute("href")).toBe("/todos?id=t1");
    expect(completed.querySelector("[data-todo-strip-status]")?.textContent).toBe("已完成");
  });
});
