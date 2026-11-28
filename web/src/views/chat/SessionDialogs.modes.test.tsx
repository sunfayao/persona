// @vitest-environment jsdom
// A model write whose response never arrives cannot leave Apply busy forever. Its result is uncertain, so the optimistic
// pick stays visible for the session push instead of being rolled back as though the hub had refused it.
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { initI18n } from "../../i18n";
import { pastHub, setToken } from "../../lib/api";
import { reset, getSession, upsertSession } from "../../lib/store";
import type { Session } from "../../lib/types";
import { applyModes } from "./SessionDialogs";

const toast = vi.hoisted(() => vi.fn());
vi.mock("../../ui/atoms", async (load) => ({ ...await load<typeof import("../../ui/atoms")>(), toast }));

beforeEach(async () => {
  await initI18n("en");
  setToken("synthetic-mode-owner");
  reset();
  toast.mockClear();
  vi.useFakeTimers();
  vi.stubGlobal("fetch", vi.fn((_url: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true });
  })));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("rolls back an optimistic pick when the hub explicitly refuses it", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ code: "timeout", error: "Request timed out" }), { status: 502, headers: { "Content-Type": "application/json" } })));
  const session = { machine_id: "m", session_id: "s", title: "Probe", status: "idle", model: "old-model" } as Session;
  upsertSession(session);
  await expect(applyModes(() => session, { model: "new-model" })).resolves.toBe(false);
  expect(getSession("m", "s")?.model).toBe("old-model");
  expect(toast).toHaveBeenCalledWith("Couldn't change: The request timed out. The computer may be busy or the network unstable. Check whether it went through, then try again later.", "error");
  expect(toast).not.toHaveBeenCalledWith(expect.stringContaining("may have been applied"), "error");
});

it("ends an unconfirmed model write with a reason without rolling back a change that may have run", async () => {
  const session = { machine_id: "m", session_id: "s", title: "Probe", status: "idle", model: "old-model" } as Session;
  upsertSession(session);
  let result: boolean | "pending" = "pending";
  void applyModes(() => session, { model: "new-model" }).then((ok) => { result = ok; });
  await vi.advanceTimersByTimeAsync(pastHub(90_000) - 1);
  expect(result).toBe("pending");
  await vi.advanceTimersByTimeAsync(1);
  expect(result).toBe(false);
  expect(getSession("m", "s")?.model).toBe("new-model");
  expect(toast).toHaveBeenCalledWith("The hub didn't confirm the change within 95 seconds. It may have been applied; check the current settings before trying again.", "error");
});

it("queues terminal Claude effort while working without overwriting confirmed effort", async () => {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => new Response(JSON.stringify({ pending_effort: "high" }), { status: 200, headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetchMock);
  const session = { machine_id: "m", session_id: "s", title: "Probe", status: "working", state: "working", agent: "claude", pane_id: "w1:p1", effort: "max" } as Session;
  upsertSession(session);
  await expect(applyModes(() => getSession("m", "s") ?? session, { effort: "high" })).resolves.toBe(true);
  expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ effort: "high", queue_effort: true });
  expect(getSession("m", "s")).toMatchObject({ effort: "max", pending_effort: "high", effort_change_failed: undefined });
  expect(toast).not.toHaveBeenCalled();
});

it("reconciles queued and immediate terminal Claude effort across idle/busy races and supports cancel", async () => {
  // Race 1: UI thought idle, node queued because a turn just started.
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ pending_effort: "medium" }), { status: 200, headers: { "Content-Type": "application/json" } })));
  const idle = { machine_id: "m", session_id: "s1", title: "Probe", status: "idle", state: "idle", agent: "claude", pane_id: "w1:p1", effort: "max" } as Session;
  upsertSession(idle);
  await expect(applyModes(() => getSession("m", "s1") ?? idle, { effort: "medium" })).resolves.toBe(true);
  expect(getSession("m", "s1")).toMatchObject({ effort: "max", pending_effort: "medium" });

  // Cancel queued effort by choosing the confirmed effort again.
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } })));
  await expect(applyModes(() => getSession("m", "s1") ?? idle, { effort: "max" })).resolves.toBe(true);
  expect(getSession("m", "s1")).toMatchObject({ effort: "max", pending_effort: undefined });

  // Race 2: UI thought working, node finished the turn and applied immediately.
  const busy = { machine_id: "m", session_id: "s2", title: "Probe", status: "working", state: "working", agent: "claude", pane_id: "w1:p2", effort: "max" } as Session;
  upsertSession(busy);
  await expect(applyModes(() => getSession("m", "s2") ?? busy, { effort: "low" })).resolves.toBe(true);
  expect(getSession("m", "s2")).toMatchObject({ effort: "low", pending_effort: undefined });
});

it("shows the direct 409 busy refusal message instead of HTTP 502", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ code: "modes_effort_busy", delivered: "none", error: "The agent is working. Change thinking effort after this turn finishes." }), { status: 409, headers: { "Content-Type": "application/json" } })));
  const session = { machine_id: "m", session_id: "s3", title: "Probe", status: "working", state: "working", agent: "claude", pane_id: "w1:p3", effort: "max" } as Session;
  upsertSession(session);
  await expect(applyModes(() => getSession("m", "s3") ?? session, { effort: "high" })).resolves.toBe(false);
  expect(getSession("m", "s3")).toMatchObject({ effort: "max", pending_effort: undefined });
  expect(toast).toHaveBeenCalledWith("Couldn't change: The agent is working. Change thinking effort after this turn finishes.", "error");
});
