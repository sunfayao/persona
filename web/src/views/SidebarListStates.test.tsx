// @vitest-environment jsdom
// The Agents block when it has nothing to list because a read failed or the hub is out of reach (the web audit's items
// 4 and 36), against the store: the complete list's failed read says why with Retry instead of leaving the list
// blank; a hub not reached since the page opened is said, not "Loading…" for good.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { t } from "../i18n";
import { connect, getState, reconnectNow, reset } from "../lib/store";
import type { Machine, Session } from "../lib/types";
import { Sidebar } from "./Sidebar";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

class FakeSocket {
  static OPEN = 1;
  static all: FakeSocket[] = [];
  readyState = 0;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  constructor() {
    FakeSocket.all.push(this);
  }
  send() {}
  close() {
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  push(msg: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  drop() {
    this.readyState = 3;
    this.onclose?.({ code: 1006 });
  }
}
const last = () => FakeSocket.all[FakeSocket.all.length - 1];

const mac: Machine = { id: "mac", name: "工作电脑", online: true };
const agent: Session = { machine_id: "mac", session_id: "s1", title: "改发布脚本", cwd: "/code/weather", status: "working", state: "working", updated_at: "2026-11-05T00:00:00Z" };

const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));
const failed = () => json({ error: "boom" }, 500);
const counts = { total: 1, revision: "r1", state: { working: 1 }, machine: { mac: 1 }, project: {}, group: {} };
const agentsResponse = (query: URLSearchParams) => query.get("counts") === "1"
  ? json(counts)
  : json({ sessions: [agent], total: 1, has_more: false, revision: "r1",
      ...(query.get("with_counts") === "1" ? { counts } : {}) });

/** The complete and paged lists share the route. A counts-only read uses its own response shape. */
const hub = (o: { sessions?: (query: URLSearchParams) => Promise<Response>; pinned?: boolean }) =>
  vi.fn((url: string) => {
    const { pathname: path, searchParams } = new URL(url, "http://hub");
    if (path === "/api/v1/sessions") return (o.sessions ?? agentsResponse)(searchParams);
    if (path === "/api/v1/machines") return json({ machines: [mac] });
    // a pin keeps the complete list on a paging hub (lib/sidebar-api.ts pagedAgentList)
    if (path === "/api/v1/pins" && o.pinned) return json({ sessions: ["mac/s1"], spaces: [] });
    return json({});
  });

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); await new Promise((r) => setTimeout(r, 0)); });
};
const agentsBlock = () => host.querySelector<HTMLElement>("[data-sidebar-agents]")!;
const alerts = () => [...agentsBlock().querySelectorAll('[role="alert"]')].map((el) => el.textContent);
const retryButton = () => [...agentsBlock().querySelectorAll("button")].find((b) => b.textContent === t("sidebar.retry"));
const render = async () => {
  act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
  await flush();
};
/** Connects, says hello with `features` and pushes the computer and its agent. */
const online = (features: string[] = []) => {
  connect();
  const sock = last();
  sock.open();
  sock.push({ type: "hello", server_version: "v1", features });
  sock.push({ type: "machines", machines: [mac] });
  sock.push({ type: "sessions", machine_id: "mac", sessions: [agent] });
};

beforeEach(() => {
  localStorage.clear();
  reset();
  FakeSocket.all = [];
  vi.stubGlobal("WebSocket", FakeSocket);
  history.replaceState(null, "", "/");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(async () => {
  // the reads this test started answer before the next one starts
  await flush();
  act(() => root.unmount());
  host.remove();
  reset();
  vi.unstubAllGlobals();
});

describe("a hub not reached since the page opened (audit item 36)", () => {
  it("says so in the Agents list instead of Loading… for good", async () => {
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(() => {})));
    connect();
    await render();
    // the first try: still loading, not offline yet
    expect(agentsBlock().textContent).toContain(t("sidebar.loading"));
    for (let i = 0; i < 3; i++) {
      await act(async () => last().drop());
      if (i < 2) act(() => reconnectNow());
    }
    await flush();
    expect(getState().conn).toBe("offline");
    expect(agentsBlock().textContent).toContain(t("sidebar.agents_offline"));
    expect(agentsBlock().textContent).not.toContain(t("sidebar.loading"));
  });
});

describe("the complete Agents list's read fails (audit item 4)", () => {
  it("says it could not read the list, why, and Retry reads it again, instead of a blank list", async () => {
    let sessions = failed;
    vi.stubGlobal("fetch", hub({ sessions: () => sessions(), pinned: true }));
    online(["sessions_list"]);
    await render();
    expect(getState().pins.sessions).toEqual(["mac/s1"]);
    const [said] = alerts();
    expect(said).toContain(t("sidebar.agents_read_failed", { error: "" }).replace(/[:：]\s*$/, ""));
    expect(said).toMatch(/500/);
    expect(agentsBlock().textContent).not.toContain(t("sidebar.loading"));
    sessions = () => json({ sessions: [agent], total: 1, has_more: false, revision: "r1" });
    await act(async () => retryButton()!.click());
    await flush();
    expect(alerts()).toEqual([]);
    expect(agentsBlock().textContent).toContain("改发布脚本");
  });
});

describe("a list drawn while the socket tries again after giving up (audit item 36)", () => {
  it("says the hub is out of reach, as the page's Offline does, not Loading…", async () => {
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(() => {})));
    connect();
    for (let i = 0; i < 3; i++) {
      await act(async () => last().drop());
      if (i < 2) act(() => reconnectNow());
    }
    expect(getState().conn).toBe("offline");
    // a socket that opens and closes before the hub says hello: connecting again, and the page stays Offline; a list
    // drawn now (the phone's drawer opened) reads the page's flag
    act(() => reconnectNow());
    await act(async () => { last().open(); last().drop(); });
    expect(getState().conn).toBe("connecting");
    await render();
    expect(agentsBlock().textContent).toContain(t("sidebar.agents_offline"));
    expect(agentsBlock().textContent).not.toContain(t("sidebar.loading"));
  });
});

describe("the paged Agents list's read fails (audit item 4)", () => {
  it("says it could not read the Agents list, as the complete list does, with Retry", async () => {
    let unavailable = true;
    const fetchHub = hub({ sessions: (query) => unavailable ? failed() : agentsResponse(query) });
    vi.stubGlobal("fetch", fetchHub);
    const countReads = () => fetchHub.mock.calls.filter(([url]) => {
      const { pathname, searchParams } = new URL(url, "http://hub");
      return pathname === "/api/v1/sessions" && searchParams.get("agents_only") === "1" && searchParams.get("counts") === "1";
    }).length;
    online(["sessions_list", "sessions_agents_only"]);
    await render();
    expect(host.querySelector("[data-paged-sessions]")).not.toBeNull();
    const said = alerts();
    expect(said.length).toBeGreaterThan(0);
    for (const a of said) expect(a).toContain(t("sidebar.agents_read_failed", { error: "" }).replace(/[:：]\s*$/, ""));
    expect(said.some((a) => /500/.test(a ?? ""))).toBe(true);
    const readsBeforeRetry = countReads();
    unavailable = false;
    await act(async () => retryButton()!.click());
    await flush();
    expect(countReads()).toBeGreaterThan(readsBeforeRetry);
    expect(alerts()).toEqual([]);
  });
});

