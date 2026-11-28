// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
const sent = vi.hoisted(() => ({ calls: [] as { path: string; json?: Record<string, unknown> }[] }));
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string, init?: { json?: Record<string, unknown> }) => {
    sent.calls.push({ path, json: init?.json });
    return Promise.resolve({ id: "i1" });
  },
}));
import type { InboxItem } from "../../lib/inbox";
import { SuggestionCard } from "./InboxEvent";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  sent.calls = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const item = { id: "i1", source: "lark", title: "chat", suggestion: { version: 3, kind: "todo", text: "回复报价", due: "2026-11-20" } } as unknown as InboxItem;
describe("to-do suggestion", () => {
  it("shows the title as text without offering to create a to-do", async () => {
    await act(async () => root.render(<SuggestionCard item={item} suggestion={item.suggestion!} />));
    const card = host.querySelector('[data-suggestion="todo"]')!;
    expect(card.textContent).toContain(item.suggestion!.text);
    expect(card.querySelector("input, textarea, button")).toBeNull();
    expect(sent.calls).toEqual([]);
  });
});
