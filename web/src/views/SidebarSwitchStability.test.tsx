// @vitest-environment jsdom
// Switching Agent conversations by clicking a row inside the sidebar must not jump scrollTop to re-center the row;
// external navigations (e.g. URL change or ⌘K) still reveal an off-screen active row.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { refresh, reset } from "../lib/store";
import type { Machine, Session } from "../lib/types";
import { Sidebar } from "./Sidebar";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const machines: Machine[] = [{ id: "mac", name: "MacBook", online: true }];
const sessions: Session[] = Array.from({ length: 12 }, (_, i) => ({
  machine_id: "mac",
  session_id: `s${i + 1}`,
  title: `task ${i + 1}`,
  cwd: "/code",
  status: "idle",
  state: "idle",
}));
const answers: Record<string, unknown> = { "/api/v1/sessions": { sessions }, "/api/v1/machines": { machines } };

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem("fold:loose", "0");
  vi.stubGlobal("fetch", vi.fn((url: string) => Promise.resolve(new Response(JSON.stringify(answers[new URL(url, "http://x").pathname] ?? {}), { headers: { "Content-Type": "application/json" } }))));
  history.replaceState(null, "", "/");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("sidebar conversation switch scroll stability", () => {
  it("does not jump scrollTop when switching conversations via sidebar click, but still reveals external navigation", async () => {
    await act(() => refresh());
    act(() => root.render(<Sidebar route={{ name: "session", mid: "mac", sid: "s1" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();

    const list = host.querySelector<HTMLElement>("[data-sidebar-rows]")!;
    list.style.overflowY = "auto";
    list.scrollTop = 120;

    // Give the scroller a 200px viewport and position s5 partially clipped at the bottom edge (top=190, bottom=220)
    // and s9 well below the viewport (top=400, bottom=430).
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(30);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this === list) {
        return { top: 0, bottom: 200, height: 200, left: 0, right: 240, width: 240, x: 0, y: 0, toJSON() {} } as DOMRect;
      }
      if (this.tagName === "A") {
        const href = this.getAttribute("href") ?? "";
        if (href.endsWith("/s/mac/s5")) return { top: 190, bottom: 220, height: 30, left: 0, right: 200, width: 200, x: 0, y: 190, toJSON() {} } as DOMRect;
        if (href.endsWith("/s/mac/s9")) return { top: 400, bottom: 430, height: 30, left: 0, right: 200, width: 200, x: 0, y: 400, toJSON() {} } as DOMRect;
      }
      return { top: 0, bottom: 30, height: 30, left: 0, right: 200, width: 200, x: 0, y: 0, toJSON() {} } as DOMRect;
    });

    // Clicking s5 inside the sidebar switches route to s5 without jumping list.scrollTop.
    const s5 = list.querySelector<HTMLAnchorElement>('a[href$="/s/mac/s5"]')!;
    act(() => {
      s5.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 }));
      root.render(<Sidebar route={{ name: "session", mid: "mac", sid: "s5" }} onSearch={() => {}} onNewAgent={() => {}} />);
    });
    await flush();
    expect(list.scrollTop).toBe(120);

    // Wait past the sidebar click window, then navigate externally to s9: reveal() should scroll it into view.
    vi.spyOn(Date, "now").mockReturnValue(Date.now() + 1000);
    act(() => {
      root.render(<Sidebar route={{ name: "session", mid: "mac", sid: "s9" }} onSearch={() => {}} onNewAgent={() => {}} />);
    });
    await flush();
    expect(list.scrollTop).toBeGreaterThan(120);
  });
});
