// @vitest-environment jsdom
// Exercise the real REST client: a mock api() cannot reveal a request borrowing the next login's token.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getToken, handleUnauthorized, setToken } from "./api";
import * as notify from "./notify";
import type { NotifyPrefs } from "./notify";

type Call = {
  method: string;
  authorization: string | null;
  body: unknown;
  settled: boolean;
  resolve: (response: Response) => void;
};
const calls: Call[] = [];
const initial: NotifyPrefs = { input: true, done: "long", muted_managers: [] };
const nextLogin: NotifyPrefs = { input: true, done: "off", muted_managers: ["example-new"] };
const outcomes: Promise<unknown>[] = [];
const result = (promise: Promise<unknown>) => {
  const outcome = promise.then(() => "saved", (error: unknown) => error);
  outcomes.push(outcome);
  return outcome;
};
const settle = async (index: number, body: unknown, status = 200) => {
  calls[index].settled = true;
  calls[index].resolve(new Response(JSON.stringify(body), { status }));
  // fetch(), response.text(), api(), and the preference queue each have their own promise continuation.
  for (let i = 0; i < 12; i++) await Promise.resolve();
};

beforeEach(async () => {
  localStorage.clear();
  calls.length = 0;
  outcomes.length = 0;
  setToken("test-token-a");
  vi.stubGlobal("fetch", vi.fn((_input: unknown, init?: RequestInit) => new Promise<Response>((resolve) => {
    calls.push({ method: init?.method ?? "GET", authorization: new Headers(init?.headers).get("Authorization"), body: init?.body ? JSON.parse(String(init.body)) : undefined, settled: false, resolve });
    // Deliberately retain late responses even if the client aborts: fencing must hold independently of cancellation.
  })));
  const load = notify.loadNotifyPrefs();
  await settle(0, initial);
  await load;
  calls.length = 0;
});

afterEach(async () => {
  // Drain unexpected requests too, so a pre-fix assertion failure does not leak pending work into the next case.
  for (let i = 0; i < calls.length; i++) {
    if (!calls[i].settled) await settle(i, initial);
  }
  await Promise.all(outcomes);
  handleUnauthorized(() => {});
  setToken("");
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("notification preference reads belong to the login that started them (#1285)", () => {
  it("discards an old preference load after re-pairing", async () => {
    const old = result(notify.loadNotifyPrefs());
    setToken("test-token-b");
    const load = notify.loadNotifyPrefs();
    await settle(1, nextLogin);
    await load;
    const cached = localStorage.getItem("persona.notifyPrefs");
    await settle(0, { ...initial, input: false });
    expect(await old).toMatchObject({ name: "AbortError" });
    expect(notify.notifyPrefs()).toEqual(notify.normalize(nextLogin));
    expect(localStorage.getItem("persona.notifyPrefs")).toBe(cached);
  });

  it("does not let an old 401 sign out the new login", async () => {
    const unauthorized = vi.fn(() => setToken(""));
    handleUnauthorized(unauthorized);
    const old = result(notify.loadNotifyPrefs());
    setToken("test-token-b");
    const load = notify.loadNotifyPrefs();
    await settle(1, nextLogin);
    await load;
    await settle(0, { error: "invalid token" }, 401);
    expect(await old).toMatchObject({ name: "AbortError" });
    expect(unauthorized).not.toHaveBeenCalled();
    expect(getToken()).toBe("test-token-b");
    expect(notify.notifyPrefs()).toEqual(notify.normalize(nextLogin));
  });

  it("still invokes sign-out for a current login's 401", async () => {
    const unauthorized = vi.fn(() => setToken(""));
    handleUnauthorized(unauthorized);
    const load = result(notify.loadNotifyPrefs());
    await settle(0, { error: "invalid token" }, 401);
    expect(unauthorized).toHaveBeenCalledOnce();
    expect(getToken()).toBe("");
    expect(await load).toBeDefined();
  });
});
