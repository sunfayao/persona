// @vitest-environment jsdom
// Source subscriptions on the web (#2041): the pages are the manager's declarative UI at /api/v1/subscriptions/-/ui,
// drawn again when a subscription changes.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hub = vi.hoisted(() => ({
  calls: [] as { path: string; body?: Record<string, unknown> }[],
  features: [] as string[],
  listeners: new Map<string, Set<(m: Record<string, unknown>) => void>>(),
}));

vi.mock("../../lib/api", async (original) => ({
  ...(await original<typeof import("../../lib/api")>()),
  api: async (path: string, init: { json?: Record<string, unknown> } = {}) => {
    hub.calls.push({ path, body: init.json });
    return { view: { title: "Subscriptions", sections: [{ items: [{ type: "text", text: `drawn ${hub.calls.length}` }] }] } };
  },
}));
vi.mock("../../lib/store", async (original) => {
  const real = await original<typeof import("../../lib/store")>();
  return {
  ...real,
  useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T): T => select({ ...real.getState(), features: hub.features }),
  onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
    if (!hub.listeners.has(type)) hub.listeners.set(type, new Set());
    hub.listeners.get(type)!.add(l);
    return () => void hub.listeners.get(type)?.delete(l);
  },
  };
});

import { parse, href } from "../../lib/router";
import { subscriptionsHref, SUBSCRIPTIONS_UI } from "../../lib/subscriptions";
import { LegacyExtensionUI } from "../settings/ExtensionView";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.IS_REACT_ACT_ENVIRONMENT = true;
  g.matchMedia = (q: string) => ({ matches: q.includes("min-width"), addEventListener() {}, removeEventListener() {} });
});

let host: HTMLDivElement;
let root: Root;
const settle = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(() => {
  hub.calls.length = 0;
  hub.listeners.clear();
  hub.features = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

describe("the pages", () => {
  it("are drawn by the manager's endpoint, with the first state, and again when a subscription changes", async () => {
    act(() => root.render(<LegacyExtensionUI extension={SUBSCRIPTIONS_UI} ui="pages" state='{"ext":"apps"}' />));
    await settle();
    expect(hub.calls[0]).toMatchObject({ path: "/subscriptions/-/ui", body: { state: '{"ext":"apps"}' } });
    expect(host.textContent).toContain("drawn 1");
    await act(async () => hub.listeners.get("subscription")?.forEach((l) => l({ type: "subscription", id: "sub_1", state: "ok" })));
    await settle();
    expect(hub.calls.filter((c) => c.path === "/subscriptions/-/ui").length).toBe(2);
  });

  it("an extension's page does not redraw on subscription events", async () => {
    act(() => root.render(<LegacyExtensionUI extension="demo" ui="home" />));
    await settle();
    expect(hub.listeners.get("subscription")?.size ?? 0).toBe(0);
    expect(hub.calls[0].path).toMatch(/^\/(extensions|plugins)\/demo\/-\/ui\/home$/);
  });

  it("open at /subscriptions, with a scope in ?state=", () => {
    expect(parse("/subscriptions")).toEqual({ name: "subscriptions" });
    expect(href({ name: "subscriptions" })).toMatch(/\/subscriptions$/);
    expect(subscriptionsHref()).toBe("/subscriptions");
    expect(decodeURIComponent(subscriptionsHref({ ext: "apps" }))).toBe('/subscriptions?state={"ext":"apps"}');
  });
});
