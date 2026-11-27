// @vitest-environment jsdom
// A question on a terminal's screen (a CLI in a pane) is drawn as an agent's own question is: option rows, the write-in
// row with its send arrow, a pick sends it. Nothing beside the answers puts the question away or closes the Agent; a
// screen the hub could not read into options keeps both (Interactions.close.test.tsx).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
const calls = vi.hoisted(() => [] as { path: string; body: Record<string, unknown> }[]);
vi.mock("../../lib/ops", () => ({
  apiOp: vi.fn(async (path: string, init: { json: Record<string, unknown> }) => {
    calls.push({ path, body: init.json });
    return {};
  }),
}));
import { InteractionCard } from "./Interactions";
import { t } from "../../i18n";
import { getState, reset } from "../../lib/store";
import type { Interaction, Session } from "../../lib/types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const question = {
  id: "screen_q", machine_id: "m", session_id: "s", kind: "screen", screen: "",
  dialog: {
    question: "Question 4/4: Which banner?",
    rev: "r1",
    options: [{ key: "1", label: "Red", detail: "Warm" }, { key: "2", label: "Blue" }, { key: "3", label: "Type something.", kind: "type_something" }],
  },
} as Interaction;
const multi = {
  id: "screen_m", machine_id: "m", session_id: "s", kind: "screen", screen: "",
  dialog: {
    question: "Which platforms?",
    rev: "r2",
    multi: true,
    options: [{ key: "1", label: "Phone" }, { key: "2", label: "Watch", checked: true }, { key: "3", label: "Type something", kind: "type_something" }],
  },
} as Interaction;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};
const buttons = () => [...host.querySelectorAll<HTMLButtonElement>("button")];
const labelled = (label: string) => buttons().find((b) => b.textContent?.trim() === label || b.getAttribute("aria-label") === label);
const option = (label: string) => buttons().find((b) => b.hasAttribute("aria-pressed") && b.textContent?.includes(label))!;
const type = (el: HTMLInputElement, text: string) => act(() => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, text);
  el.dispatchEvent(new Event("input", { bubbles: true }));
});

beforeEach(() => {
  calls.length = 0;
  // blocked on its question: the state in which an unread screen's card offers to close the Agent
  Object.assign(getState(), { sessions: { m: [{ machine_id: "m", session_id: "s", agent: "agy", status: "blocked", state: "needs_you" } as Session] }, interactions: [], answered: {} });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
});

describe("a terminal question's card", () => {
  it("offers the answers and nothing else: no hand back, no close, no answer row", () => {
    act(() => root.render(<InteractionCard ix={question} />));
    expect(labelled(t("chat-interactions.hand_back"))).toBeUndefined();
    expect(labelled(t("chat-interactions.close_agent"))).toBeUndefined();
    expect(host.querySelector("[data-card-actions]")).toBeNull();
    expect(host.textContent).not.toContain(t("chat-interactions.screen_hint"));
    expect(option("Red").textContent).toContain("Warm");
  });

  it("sends a pick by its key", async () => {
    act(() => root.render(<InteractionCard ix={question} />));
    act(() => option("Blue").click());
    await flush();
    expect(calls).toEqual([{ path: "/interactions/screen_q/resolve", body: { option: "2" } }]);
  });

  it("writes in through the write-in row and its send arrow", async () => {
    act(() => root.render(<InteractionCard ix={question} />));
    const field = host.querySelector<HTMLInputElement>(`input[aria-label="Type something."]`)!;
    const arrow = labelled(t("chat-interactions.send_answer"))!;
    expect(arrow.closest("label")?.contains(field)).toBe(true);
    expect(arrow.disabled).toBe(true);
    await type(field, "Green");
    act(() => labelled(t("chat-interactions.send_answer"))!.click());
    await flush();
    expect(calls).toEqual([{ path: "/interactions/screen_q/resolve", body: { option: "3", text: "Green" } }]);
  });

  it("sends a multi-select question's ticks and text by the answer row's arrow, with nothing that puts it away", async () => {
    act(() => root.render(<InteractionCard ix={multi} />));
    expect(labelled(t("chat-interactions.hand_back"))).toBeUndefined();
    expect(option("Watch").getAttribute("aria-pressed")).toBe("true");
    act(() => option("Phone").click());
    await type(host.querySelector<HTMLInputElement>(`input[aria-label="${t("chat-interactions.other_answer")}"]`)!, "Car");
    const arrow = host.querySelector<HTMLButtonElement>(`[data-card-actions] button[aria-label="${t("chat-interactions.send_answer")}"]`)!;
    act(() => arrow.click());
    await flush();
    expect(calls).toEqual([{ path: "/interactions/screen_m/resolve", body: { options: ["1", "2"], text: "Car" } }]);
  });
});
