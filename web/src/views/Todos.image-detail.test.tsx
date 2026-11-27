// @vitest-environment jsdom
// #11084: a saved to-do reads its private image bytes with the caller's credentials, still shows them after refresh, and
// opens the existing large viewer. The page only shows them: nothing adds or removes one.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { initI18n, t } from "../i18n";
import { getToken, setToken } from "../lib/api";
import { setRosterDefault } from "../lib/manager-choice";
import { getState, refreshTodos } from "../lib/store";
import type { Todo } from "../lib/todos";

vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
const toast = vi.hoisted(() => vi.fn());
vi.mock("../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../ui/atoms")>()), toast }));
const { TodosView } = await import("./Todos");
await import("./chat/ImageViewer");
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const imageURL = "/api/v1/todos/t1/images/d0000000-0000-4000-8000-000000000001";
const todo = {
  id: "t1", title: "Screenshot regression", notes: "The stored picture is the requirement.",
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", agents: [],
  images: [{ id: "d0000000-0000-4000-8000-000000000001", mime: "image/png", width: 2, height: 3, url: imageURL }],
} as unknown as Todo;
const before = { ...getState() };
const originalToken = getToken();
let host: HTMLDivElement;
let root: Root;
let serverTodo: Todo;
let writes: string[];
let imageReads: RequestInit[];
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
const flush = async () => { for (let i = 0; i < 10; i++) await act(async () => { await Promise.resolve(); }); };
const button = (label: string, within: ParentNode = document) => [...within.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label);
const click = async (el: Element | undefined) => { expect(el, "button").toBeTruthy(); await act(async () => (el as HTMLElement).click()); await flush(); };

beforeEach(async () => {
  await initI18n("en");
  serverTodo = JSON.parse(JSON.stringify(todo)) as Todo;
  writes = [];
  imageReads = [];
  toast.mockReset();
  setToken("synthetic-image-owner");
  setRosterDefault("assistant");
  Object.assign(getState(), { todos: [serverTodo], todosLoaded: true, todosError: null, todosSupported: true, machines: [{ id: "m1", name: "Remote box", online: true }], sessions: {}, interactions: [], answered: {} });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace("/api/v1", "");
    if (path === imageURL.replace("/api/v1", "")) {
      imageReads.push(init);
      return new Response(new Uint8Array([0x89, 0x50, 0x4e, 0x47]), { status: 200, headers: { "Content-Type": "image/png", "Cache-Control": "private, no-store" } });
    }
    if (path === "/todos") return json({ todos: [serverTodo] });
    if (path === "/todos/t1") return json({ todo: serverTodo });
    if (path.endsWith("/decisions")) return json({ decisions: [] });
    if (init.method && init.method !== "GET") writes.push(`${init.method} ${path}`);
    return json({});
  }));
  URL.createObjectURL = vi.fn(() => "blob:persisted-todo-image");
  URL.revokeObjectURL = vi.fn();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => root.render(<TodosView selectedId="t1" />));
  await flush();
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  document.body.innerHTML = "";
  Object.assign(getState(), before);
  setToken(originalToken);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("persisted to-do images", () => {
  it("reads private bytes with credentials, keeps the association on refresh, and opens the large viewer", async () => {
    expect(imageReads).toHaveLength(1);
    const headers = new Headers(imageReads[0].headers);
    expect(headers.get("Authorization")).toBe("Bearer synthetic-image-owner");
    expect(headers.get("X-Persona-Manager")).toBe("assistant");
    const thumbnail = () => document.querySelector<HTMLImageElement>('[data-testid="todos-detail-column"] img[src="blob:persisted-todo-image"]');
    expect(thumbnail(), "saved image thumbnail").toBeTruthy();
    await act(async () => { await refreshTodos(); });
    await flush();
    expect(thumbnail(), "thumbnail after a fresh GET").toBeTruthy();
    const opener = thumbnail()!.closest("button");
    expect(opener, "enlarge control").toBeTruthy();
    await click(opener!);
    await act(async () => { await vi.dynamicImportSettled(); });
    await vi.waitFor(() => {
      const viewer = [...document.querySelectorAll<HTMLElement>("[role=dialog]")].at(-1);
      expect(viewer, "large image viewer").toBeTruthy();
      expect(viewer!.querySelector('img[src="blob:persisted-todo-image"]')).toBeTruthy();
      expect(button(t("chat-imageviewer.download"), viewer!), "viewer download action").toBeTruthy();
    });
    // the pictures are the to-do's: nothing on its page adds, removes or sends one
    expect(document.querySelector('[data-testid="todos-detail-column"] input[type=file]')).toBeNull();
    expect(writes).toEqual([]);
  });
});
