// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { setCsrf, setTransport } from "../api";
import type { Market } from "../../lib/extension-market";
import { ExtensionMarkets } from "./ExtensionMarket";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const LIMITED = "读不到 github:example/extensions：被限流了（HTTP 403）：约 45 分钟后再试；常用这个市场就填一个 token，额度更高";
let host: HTMLDivElement;
let root: Root;
let markets: Market[];

const flush = async () => {
  // the catalog loads after a timer (its search debounce)
  for (let i = 0; i < 8; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};
const text = () => host.textContent ?? "";

beforeEach(() => {
  setCsrf("tok");
  setTransport(async (url) => {
    const path = url.replace("/api/v1/admin", "").replace(/\?.*$/, "");
    const reply = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status });
    if (path === "/extensions") return reply({ extensions: [] });
    if (path === "/extensions/-/markets") return reply({ markets });
    if (path === "/extensions/-/catalog") return reply({ extensions: [] });
    return reply({}, 404);
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.body.innerHTML = "";
  setTransport((url, init) => fetch(url, init));
  setCsrf("");
});

const open = async () => {
  act(() => root.render(<ExtensionMarkets onChanged={() => {}} />));
  await flush();
};

const github = (id: string, error?: string): Market => ({ id, name: id, spec: "github:example/extensions", extensions: 0, error } as Market);
const button = (label: string) => [...document.querySelectorAll("button")].find((b) => b.textContent?.includes(label));

describe("the admin page's markets", () => {
  it("shows a market read failure and offers a GitHub token", async () => {
    markets = [github("example", LIMITED)];
    await open();
    expect(text()).toContain(LIMITED);
    expect(host.querySelector(".text-red")).not.toBeNull();
    expect(text()).toContain("市场暂时读不到，读到后扩展会列在这里");
    expect(text()).not.toContain("这里还没有扩展");
    expect(text()).toContain("GitHub 不带 token 时每小时只能读 60 次");
    expect(button("填 token")).toBeTruthy();
  });

  it("shows a web market error without a GitHub token hint", async () => {
    markets = [github("example"), { id: "m2", name: "team", spec: "https://market.example.com/persona-extensions.json", extensions: 0, error: "HTTP 404" } as Market];
    await open();
    expect(text()).toContain("读不到：HTTP 404");
    expect(host.querySelector(".text-red")).not.toBeNull();
    expect(text()).not.toContain("GitHub 不带 token 时每小时只能读 60 次");
  });

  it("offers a token under a GitHub market's error, and opens its dialog", async () => {
    markets = [github("team", "HTTP 403")];
    await open();
    expect(text()).toContain("GitHub 不带 token 时每小时只能读 60 次");
    const fix = button("填 token");
    expect(fix).toBeTruthy();
    act(() => fix!.click());
    await flush();
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain("「team」的 token");
  });

  it("does not offer a token when the GitHub market has one", async () => {
    markets = [{ ...github("team", "HTTP 403"), has_token: true }];
    await open();
    expect(text()).not.toContain("GitHub 不带 token 时每小时只能读 60 次");
  });

  it("says there are no extensions when every market was read", async () => {
    markets = [github("example")];
    await open();
    expect(text()).toContain("这里还没有扩展");
    expect(text()).not.toContain("暂时读不到");
  });
});
