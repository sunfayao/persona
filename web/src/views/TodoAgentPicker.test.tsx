// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../i18n";
import type { Todo } from "../lib/todos";
import type { Session } from "../lib/types";

vi.hoisted(() => {
  window.matchMedia = vi.fn((media: string) => ({
    media, matches: false, onchange: null, addEventListener() {}, removeEventListener() {},
    addListener() {}, removeListener() {}, dispatchEvent: () => false,
  }));
});

const attach = vi.fn<(todoId: string, mid: string, sid: string) => Promise<void>>();
const refreshTodos = vi.fn<() => Promise<void>>();
vi.mock("../lib/todos", () => ({ attachUnassignedTodoAgent: attach }));
const fixtures: Session[] = [
  { machine_id: "m2", session_id: "s3", title: "Assigned", todo: { id: "t2", title: "Other to-do" } } as Session,
  { machine_id: "m1", session_id: "s1", title: "Review", state: "working" } as Session,
  { machine_id: "m2", session_id: "s2", title: "Test", state: "needs_you" } as Session,
  { machine_id: "m2", session_id: "s4", title: "Folded worker", folded_under: "s2" } as Session,
];
let sessions = fixtures;
const todo: Todo = { id: "t1", title: "Release", created_at: "", updated_at: "" };
vi.mock("../lib/sessions", () => ({ useFullAgentSessions: () => ({ sessions, loaded: true }), retryFullAgentSessions: vi.fn() }));
vi.mock("../lib/store", () => ({ getState: () => ({ todos: [todo] }), refreshTodos }));

const { TodoAgentPicker } = await import("./TodoAgentPicker");
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const close = vi.fn();
beforeEach(async () => {
  sessions = fixtures;
  attach.mockReset().mockResolvedValue(undefined);
  refreshTodos.mockReset().mockResolvedValue(undefined);
  close.mockClear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => root.render(<TodoAgentPicker todo={todo} machines={[]} onClose={close} />));
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
});

const checkbox = (title: string) => [...document.querySelectorAll<HTMLLabelElement>("label")].find((row) => row.textContent?.includes(title))?.querySelector<HTMLInputElement>('[type="checkbox"]');
const button = (count: number) => [...document.querySelectorAll("button")].find((node) => node.textContent?.trim() === t("todos.link_agents_submit", { count }));
const search = async (word: string) => {
  const input = document.querySelector<HTMLInputElement>('input:not([type="checkbox"])')!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, word);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
};

describe("linking existing agents from a to-do", () => {
  it("shows available agents first and keeps a conflicted selection within reach", async () => {
    const first = () => document.querySelector('input[type="checkbox"]');
    expect(first()).toBe(checkbox("Review"));

    await act(async () => checkbox("Test")!.click());
    sessions = fixtures.map((session) => session.session_id === "s2" ? { ...session, todo: { id: "t2", title: "Other to-do" } } : session);
    await act(async () => root.render(<TodoAgentPicker todo={todo} machines={[]} onClose={close} />));
    expect(first()).toBe(checkbox("Test"));
    expect(checkbox("Test")).toMatchObject({ checked: true, disabled: false });

    await act(async () => checkbox("Test")!.click());
    expect(first()).toBe(checkbox("Review"));
    expect(checkbox("Test")).toMatchObject({ checked: false, disabled: true });
  });

  it("keeps selection across search and excludes linked or folded agents", async () => {
    expect(checkbox("Assigned")?.disabled).toBe(true);
    expect(checkbox("Folded worker")).toBeUndefined();
    await act(async () => checkbox("Review")!.click());
    await search("Test");
    expect(checkbox("Review")).toBeUndefined();
    await act(async () => checkbox("Test")!.click());
    await act(async () => button(2)!.click());
    expect(attach.mock.calls).toEqual([["t1", "m1", "s1"], ["t1", "m2", "s2"]]);
    expect(refreshTodos).toHaveBeenCalledOnce();
    expect(close).toHaveBeenCalledOnce();
  });

  it("retries only failed members of a partial batch", async () => {
    attach.mockImplementationOnce(async () => {}).mockRejectedValueOnce(new Error("Computer unavailable"));
    await act(async () => checkbox("Review")!.click());
    await act(async () => checkbox("Test")!.click());
    await act(async () => button(2)!.click());
    expect(close).not.toHaveBeenCalled();
    expect(document.querySelector('[role="alert"]')?.textContent).toContain("Computer unavailable");
    expect(checkbox("Review")?.checked).toBe(false);
    expect(checkbox("Test")?.checked).toBe(true);
    await act(async () => button(1)!.click());
    expect(attach.mock.calls).toEqual([["t1", "m1", "s1"], ["t1", "m2", "s2"], ["t1", "m2", "s2"]]);
    expect(close).toHaveBeenCalledOnce();
  });
});
