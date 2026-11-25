// @vitest-environment jsdom
// A sidebar row's ⋯ toggles the row's menu: a second press closes it. A browser sends a pointerdown before the click,
// and the menu closes on a pointerdown outside its panel and its anchor, so the ⋯ has to lie inside the anchor.
// jsdom's click() sends no pointerdown: `press` sends both, in order.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
  // jsdom has no CSS.escape; the menu's outside-press check calls it
  const css = ((globalThis as { CSS?: Partial<typeof CSS> }).CSS ??= {});
  css.escape ??= (s: string) => s.replace(/[^\w-]/g, (c) => `\\${c}`);
});
import { loadPages } from "../lib/lazy-page";
import { connect, refresh, reset } from "../lib/store";
import type { Session } from "../lib/types";
import { Sidebar } from "./Sidebar";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const conversation = (session_id: string, title: string): Session => ({
  machine_id: "hub", session_id, title, cwd: "/persona", status: "idle", state: "idle", assistant: true, origin: "user", updated_at: "2026-05-01T10:00:00Z",
});
const main = conversation("main", "Persona");
const agent: Session = { machine_id: "mac", session_id: "agent", title: "Build docs", cwd: "/code", status: "working", state: "working" };
const second: Session = { machine_id: "mac", session_id: "second", title: "Lead pane", cwd: "/code", status: "working", state: "working" };
const answers: Record<string, unknown> = {
  "/api/v1/assistant": { machine_id: "hub", online: true, main: "main", conversations: [main] },
  "/api/v1/sessions": { sessions: [main, agent, second] },
  "/api/v1/machines": { machines: [{ id: "hub", name: "Hub", online: true }, { id: "mac", name: "MacBook", online: true }] },
};
const json = (body: unknown) => Promise.resolve(new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }));

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 10; i++) await act(async () => { await Promise.resolve(); });
};
const menus = () => document.querySelectorAll('[role="menu"]');
const link = (text: string) => [...host.querySelectorAll("a")].find((a) => a.textContent?.includes(text));
// the ⋯ next to a row's link (not inside it: a button cannot sit in a link)
const more = (row: Element | undefined) => row?.closest(".group\\/row")?.querySelector<HTMLButtonElement>("button[aria-haspopup=\"menu\"]") ?? null;
// a press as a browser sends it: pointerdown, then (a separate task) the click
const press = async (el: Element) => {
  act(() => void el.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, cancelable: true, button: 0, pointerType: "mouse" })));
  await flush();
  act(() => (el as HTMLElement).click());
  await flush();
};

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("fetch", vi.fn((url: string) => json(answers[new URL(url, "http://x").pathname] ?? {})));
  history.replaceState(null, "", "/overview");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function renderSidebar() {
  await act(() => refresh());
  act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
  await flush();
}

const rows: [string, () => Element | undefined][] = [
  ["an agent", () => link("Build docs")],
];

describe("a sidebar row's ⋯", () => {
  it.each(rows)("on %s opens the row's menu and closes it on a second press", async (_name, row) => {
    await renderSidebar();
    const button = more(row());
    expect(button).not.toBeNull();

    await press(button!);
    await vi.waitFor(() => expect(menus()).toHaveLength(1));

    await press(more(row())!);
    await vi.waitFor(() => expect(menus()).toHaveLength(0));

    // and opens it again on a third
    await press(more(row())!);
    await vi.waitFor(() => expect(menus()).toHaveLength(1));
  });

  it("moves the menu to another row's ⋯, and a press elsewhere still closes it", async () => {
    await renderSidebar();
    await press(more(link("Build docs"))!);
    await vi.waitFor(() => expect(menus()).toHaveLength(1));
    const first = menus()[0].textContent;

    await press(more(link("Lead pane"))!);
    await vi.waitFor(() => expect(menus()).toHaveLength(1));
    expect(menus()[0].textContent).not.toBe(first);

    act(() => void document.body.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0 })));
    await flush();
    await vi.waitFor(() => expect(menus()).toHaveLength(0));
  });

  it("on a right-clicked row's ⋯ moves the menu there instead of closing it", async () => {
    await renderSidebar();
    const row = link("Build docs")!;
    act(() => void row.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 10, clientY: 10 })));
    await flush();
    await vi.waitFor(() => expect(menus()).toHaveLength(1));

    await press(more(row)!);
    await vi.waitFor(() => expect(menus()).toHaveLength(1));
    await press(more(row)!);
    await vi.waitFor(() => expect(menus()).toHaveLength(0));
  });
});

// The paged Agents list (a hub with sessions_list and nothing pinned): its rows' ⋯ toggles the same way.
describe("a paged Agents row's ⋯", () => {
  // its sections are a chunk of their own (PagedSessionList)
  beforeAll(() => loadPages(), 60_000);
  class FakeSocket {
    static last: FakeSocket | null = null;
    readyState = 0;
    onopen: (() => void) | null = null;
    onmessage: ((ev: { data: string }) => void) | null = null;
    constructor() { FakeSocket.last = this; }
    send() {}
    close() { this.readyState = 3; }
  }

  it("opens the row's menu and closes it on a second press", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    // the list draws only the rows in view of its scroller: give it one with a size (jsdom has no layout)
    host.style.overflowY = "auto";
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(800);
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(280);
    vi.stubGlobal("fetch", vi.fn((input: string) => {
      const url = new URL(input, "http://x");
      if (url.pathname === "/api/v1/sessions" && url.searchParams.has("counts")) return json({ total: 1, state: { working: 1 }, machine: { mac: 1 }, group: { work: 1 }, project: {}, revision: "r1" });
      if (url.pathname === "/api/v1/sessions" && url.search) return json({ sessions: [agent], remove: [], has_more: false, revision: "r1", total: 1 });
      return json(answers[url.pathname] ?? {});
    }));
    act(() => {
      connect();
      const socket = FakeSocket.last!;
      socket.readyState = 1;
      socket.onopen?.();
      socket.onmessage?.({ data: JSON.stringify({ type: "hello", features: ["sessions_list", "sessions_lookup", "sessions_agents_only"] }) });
    });
    await flush();
    act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    // the paged list, by project
    expect(host.querySelector("[data-paged-sessions]")).not.toBeNull();
    const button = more(link("Build docs"));
    expect(button).not.toBeNull();

    await press(button!);
    await vi.waitFor(() => expect(menus()).toHaveLength(1));
    await press(more(link("Build docs"))!);
    await vi.waitFor(() => expect(menus()).toHaveLength(0));
  });
});
