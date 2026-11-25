// @vitest-environment jsdom
// Settings › Notifications: this browser's system notifications switch, when the browser has said no or has no
// notifications at all, and the Web Push group under it.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { NotificationSettings } = await import("./NotificationSettings");

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const render = () => act(async () => root.render(<NotificationSettings />));
const switches = () => [...host.querySelectorAll<HTMLButtonElement>('[role="switch"]')];
const named = (label: string) => switches().find((b) => b.getAttribute("aria-label") === label);

describe("设置 › 通知", () => {
  it("has the system switch and Web Push, and nothing else to choose", async () => {
    vi.stubGlobal("Notification", { permission: "default", requestPermission: async () => "granted" });
    await render();
    expect(named("弹出系统通知")).toBeDefined();
    expect(named("通知这个浏览器")).toBeDefined();
    expect(switches()).toHaveLength(2);
    expect(host.querySelector('[role="radio"]')).toBeNull();
    expect(host.querySelector('input[type="time"]')).toBeNull();
  });

  it("turns system notifications on after the browser allows them", async () => {
    vi.stubGlobal("Notification", { permission: "default", requestPermission: async () => "granted" });
    await render();
    await act(async () => named("弹出系统通知")!.click());
    expect(localStorage.getItem("persona.systemNotify")).toBe("1");
  });

  it("浏览器拒绝了：开关关着、点不了，并说去哪改", async () => {
    vi.stubGlobal("Notification", { permission: "denied", requestPermission: async () => "denied" });
    localStorage.setItem("persona.systemNotify", "1");
    await render();
    const sys = named("弹出系统通知")!;
    expect(sys.disabled).toBe(true);
    expect(sys.getAttribute("aria-checked")).toBe("false");
    expect(host.textContent).toContain("已拒绝");
    expect(host.textContent).toContain("网站设置");
  });

  it("浏览器不支持：开关点不了，并说明", async () => {
    vi.stubGlobal("Notification", undefined);
    await render();
    expect(named("弹出系统通知")!.disabled).toBe(true);
    expect(host.textContent).toContain("这个浏览器不支持系统通知");
  });
});
