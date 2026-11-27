// @vitest-environment jsdom
// The page of an agent being started names its launcher by the provider's display name, the startup ID in the line's
// tooltip (#5029); an rc alias reads as before. The field to change it on a failed start keeps the startup ID.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../i18n";
import { setToken } from "../lib/api";
import { getState } from "../lib/store";
import type { AgentLaunch } from "../lib/new-agent";

vi.mock("../lib/store", async (original) => ({
  ...(await original<typeof import("../lib/store")>()),
  useStore: (select: (s: { machines: { id: string; name: string }[] }) => unknown) => select({ machines: [{ id: "mac", name: "Mac" }] }),
}));

import { forgetProviderNames } from "../lib/provider-names";
import { StartingAgent } from "./StartingAgent";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const launch = (o: Partial<AgentLaunch>): AgentLaunch => ({
  id: "l1", machineId: "mac", cwd: "/code/app", launcher: "", text: "", attachments: [], phase: "failed", error: "busy", uploaded: {},
  startOpId: "o1", startPromptOpId: "o2", messageId: "m1", ...o,
});
let host: HTMLDivElement;
let root: Root;
const flush = async () => { for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); }); };
const line = () => host.querySelector<HTMLElement>("header p")!;

beforeEach(() => {
  forgetProviderNames();
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ providers: [{ id: "launcher.relay-example-com", name: "我的中转 站", kind: "launcher" }] }), { status: 200 })));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  getState().launches = {};
});

describe("starting an agent", () => {
  it("names a provider by its display name, its ID in the tooltip; the field keeps the ID", async () => {
    act(() => root.render(<StartingAgent launch={launch({ launcher: "relay-example-com", model: "glm-5" })} />));
    await flush();
    expect(line().textContent).toBe("Mac · /code/app · 我的中转 站 · glm-5");
    expect(line().title).toBe("Mac · /code/app · 我的中转 站 (relay-example-com) · glm-5");
    expect(host.querySelector("input")!.value).toBe("relay-example-com");
  });

  it("names an rc alias by its ID, with no tooltip", async () => {
    act(() => root.render(<StartingAgent launch={launch({ launcher: "cc_glm" })} />));
    await flush();
    expect(line().textContent).toBe("Mac · /code/app · cc_glm");
    expect(line().hasAttribute("title")).toBe(false);
  });
});

async function retryFixture(overrides: Partial<AgentLaunch> = {}, lostReply = false) {
  setToken("synthetic-retry-owner");
  const item = launch({ launcher: "work", text: "Original task", agentType: "traex", inlinePrompt: true, host: "herdr", ...overrides });
  getState().launches[item.id] = item;
  let attempts = 0;
  const fetcher = vi.fn(async (url: string, _init: RequestInit = {}) => {
    if (url.endsWith("/sessions")) {
      if (lostReply && attempts++ === 0) throw new TypeError("Synthetic lost reply");
      return new Response(JSON.stringify({ session: { machine_id: "mac", session_id: "synthetic-started", agent: item.agentType, status: "idle" } }), { status: 200 });
    }
    return new Response(JSON.stringify({ providers: [] }), { status: 200 });
  });
  vi.stubGlobal("fetch", fetcher);
  act(() => root.render(<StartingAgent launch={item} />));
  await flush();
  return { item, fetcher, starts: () => fetcher.mock.calls.filter(([url]) => url.endsWith("/sessions")) };
}
const retry = async () => {
  const button = [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === t("startingagent.retry"))!;
  act(() => button.click());
  await flush();
};

it("keeps an uncertain target without dispatch when confirmation is declined", async () => {
  vi.stubGlobal("confirm", vi.fn(() => false));
  const fixture = await retryFixture({ uncertain: true });
  expect(host.querySelector("input")).toBeNull();
  await retry();
  expect(fixture.starts()).toHaveLength(0);
  expect(getState().launches[fixture.item.id].agentType).toBe("traex");
});

