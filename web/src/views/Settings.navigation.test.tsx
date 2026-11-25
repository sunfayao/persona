// @vitest-environment jsdom
// Settings category navigation: the four categories, their URLs, and the phone's list and history.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const viewport = vi.hoisted(() => ({ phone: false }));
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({ get matches() { return /min-width/.test(query) ? !viewport.phone : viewport.phone; }, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
vi.mock("../lib/store", async (original) => ({
  ...(await original<typeof import("../lib/store")>()),
  useStore: (select: (state: { features: string[]; machines: unknown[]; conn: string; link: { networkUp: boolean; restarting: boolean }; hubName: string; mesh: unknown[] }) => unknown) => select({ features: [], machines: [], conn: "online", link: { networkUp: true, restarting: false }, hubName: "", mesh: [] }),
}));
vi.mock("./settings/NotificationSettings", () => ({ NotificationSettings: () => <div data-page="notifications" /> }));
vi.mock("./settings/ModelConfiguration", () => ({
  ModelConfiguration: ({ open, onOpen }: { open: string | null; onOpen: (id: string | null) => void }) => (
    <div data-page="model-config" data-open={open}>
      <button type="button" data-purpose="open" onClick={() => onOpen("agent.triage")} />
      <button type="button" data-purpose="close" onClick={() => onOpen(null)} />
    </div>
  ),
}));
vi.mock("../lib/pwa", async (original) => ({ ...(await original<typeof import("../lib/pwa")>()), useInstall: () => "installed" }));
vi.mock("../lib/api", async (original) => ({ ...(await original<typeof import("../lib/api")>()), api: () => Promise.resolve({ launchers: [], tokens: [] }) }));

import { initI18n, t } from "../i18n";
import { Settings } from "./Settings";
import { FROM_LIST } from "./settings/nav";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement;
let root: Root;

beforeEach(async () => {
  await initI18n("en");
  viewport.phone = false;
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  await initI18n("zh-Hans");
});
const mount = async (url: string) => {
  history.replaceState(null, "", url);
  await act(async () => root.render(<Settings onToggleSidebar={() => {}} />));
};
const nav = () => host.querySelector("nav")!;
const category = (label: string) => [...nav().querySelectorAll("button")].find((button) => button.textContent?.startsWith(label))!;

describe("Settings category navigation", () => {
  it("lists only the four categories beside the page, General first", async () => {
    await mount("/settings");
    const labels = [...nav().querySelectorAll("button")].map((b) => b.textContent);
    expect(labels).toEqual([t("settings-nav.tab_general"), t("settings-nav.tab_notifications"), t("settings-nav.tab_models"), t("settings-nav.tab_devices")]);
    expect(host.querySelector('[data-settings-group="appearance"]')).toBeTruthy();
    expect(host.querySelector('[data-settings-group="this-browser"]')).toBeTruthy();
  });

  it("opens the root for a category that no longer exists", async () => {
    for (const tab of ["persona", "hub", "agents", "extensions", "nope"]) {
      await mount(`/settings?tab=${tab}`);
      expect(nav().querySelector('[aria-current="page"]')?.textContent).toBe(t("settings-nav.tab_general"));
      expect(host.querySelector('[data-settings-group="appearance"]')).toBeTruthy();
    }
  });

  it("switches category in place and keeps the address in step", async () => {
    await mount("/settings");
    await act(async () => category(t("settings-nav.tab_notifications")).click());
    expect(host.querySelector('[data-page="notifications"]')).toBeTruthy();
    expect(location.search).toBe("?tab=notifications");
  });

  it("opens a model purpose over Models and closes it again", async () => {
    await mount("/settings?tab=model-config&purpose=agent.triage");
    expect(location.search).toContain("tab=models");
    expect(host.querySelector('[data-page="model-config"]')?.getAttribute("data-open")).toBe("agent.triage");
    await act(async () => host.querySelector<HTMLButtonElement>('[data-purpose="close"]')!.click());
    expect(host.querySelector('[data-page="model-config"]')?.getAttribute("data-open")).toBeNull();
    expect(location.search).not.toContain("purpose");
  });

  it("shows the list on a phone and pushes a category over it", async () => {
    viewport.phone = true;
    await mount("/settings");
    const rows = [...nav().querySelectorAll("button")].map((b) => b.textContent ?? "");
    expect(rows).toHaveLength(4);
    const pushed = vi.spyOn(history, "pushState");
    await act(async () => [...nav().querySelectorAll("button")].find((b) => b.textContent?.startsWith(t("settings-nav.tab_devices")))!.click());
    expect(pushed).toHaveBeenCalledWith({ [FROM_LIST]: true }, "", expect.stringContaining("tab=devices"));
    expect(host.querySelector("h1")?.textContent).toBe(t("settings-nav.tab_devices"));
  });
});
