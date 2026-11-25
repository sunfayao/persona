// @vitest-environment jsdom
// A section heading selects no text and says nothing under itself.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { Section } from "./SidebarParts";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
function show(node: React.ReactNode) {
  act(() => root.render(node));
}

beforeEach(() => {
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.querySelectorAll('[role="tooltip"]').forEach((el) => el.remove());
});

describe("Section", () => {
  it("selects no text in its heading and shows no explanation under it", () => {
    show(<Section title="Recent" folded={false} onToggle={() => {}}><span>row</span></Section>);
    const heading = host.querySelector("[data-sidebar-group-heading] > div")!;
    expect(heading.className).toContain("select-none");
    expect(host.querySelector("[data-sidebar-group-heading] button")?.getAttribute("title")).toBeNull();
    expect(host.querySelectorAll("p")).toHaveLength(0);
  });
});
