// @vitest-environment jsdom
// The agent's own background work reporting back (a wake) continues the same answer: one block from one message to the
// next, its earlier words where they were, no notice pill or divider, the notice a quiet step of the work it woke.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { chatLines } from "../../lib/turns";
import { emptyTaskIndex } from "../../lib/tasks";
import type { Item } from "../../lib/types";
import { ChatContext } from "./chat-context";
import { forgetExpanded } from "./expanded";
import { ChatLineView } from "./Lines";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
const item = (seq: number, kind: Item["kind"], text?: string): Item => ({ id: `i${seq}#0`, seq, role: kind === "notice" || kind === "recap" ? "system" : "assistant", kind, text, ts: new Date(Date.parse("2026-11-04T10:00:00Z") + seq * 1000).toISOString() });
const tool = (seq: number): Item => ({ ...item(seq, "tool_use"), tool_name: "Bash", tool_use_id: `call-${seq}`, tool_input: { command: `echo example-${seq}` } });
const user = { ...item(1, "text", "Check the example."), role: "user" as const };
const wake = { ...item(4, "notice", "Background task done"), wake: true };
const first = [user, tool(2), item(3, "text", "Earlier progress.")];
let host: HTMLDivElement;
let root: Root;
beforeEach(() => { forgetExpanded(); host = document.createElement("div"); document.body.append(host); root = createRoot(host); });
afterEach(() => { act(() => root.unmount()); host.remove(); });
// each exchange's own summary line (the groups inside an opened one are not it)
const headers = () => [...host.querySelectorAll<HTMLElement>("[data-work-group]")].filter((g) => !g.parentElement?.closest("[data-work-group]")).map((g) => g.querySelector<HTMLButtonElement>(":scope > button")!);
const slots = () => [...host.querySelectorAll("[data-latest-text]")];
const details = () => [...host.querySelectorAll("[data-work-detail]")].map((d) => d.textContent ?? "");

describe.each(["assistant", "agents"] as const)("%s background continuation", (style) => {
  function render(items: Item[], live: boolean) {
    const lines = chatLines(items, { live });
    act(() => root.render(<ChatContext.Provider value={{ scope: "wake-test:", tasks: emptyTaskIndex(), subagents: [], lineStyle: style }}>
      {lines.map((line) => <ChatLineView key={line.id} line={line} mid="machine" sid="session" expandFold={async () => {}} />)}
    </ChatContext.Provider>));
  }
  const words = () => [...host.querySelectorAll("[data-words]")];
  it("continues the answer across a wake: one block, earlier words in place, no pill, the notice inside the work", () => {
    render(first, false);
    const earlier = words()[0];
    expect(earlier.textContent).toContain("Earlier progress.");
    const live = [...first, wake, tool(5), item(6, "text", "New progress."), tool(7)];
    render(live, true);
    // one block for the one message; the words before the wake stay the same element, where they were
    expect(host.querySelectorAll("[data-segment]")).toHaveLength(1);
    expect(words()[0]).toBe(earlier);
    expect(words().map((w) => w.textContent)).toEqual(["Earlier progress.", "New progress."]);
    // no pill, no line of its own: the notice names the stretch it opened, folded like any other
    expect(host.querySelector("[data-line] > .flex.justify-center")).toBeNull();
    expect(host.textContent).not.toContain(wake.text);
    expect(headers().map((h) => h.querySelector(".truncate")?.textContent)).toEqual(["运行 1 条命令", "运行 1 条命令", "正在工作… · 运行 1 条命令"]);
    expect(headers().map((h) => h.getAttribute("aria-expanded"))).toEqual(["false", "false", "false"]);
    act(() => headers()[1].click());
    expect(details().join("\n")).toContain(wake.text);
    expect(details().join("\n")).toContain("echo example-5");
    const done = [...live, item(9, "text", "Final result.")];
    render(done, false);
    expect(host.querySelectorAll("[data-segment]")).toHaveLength(1);
    expect(words()[0]).toBe(earlier);
    expect(words().map((w) => w.textContent)).toEqual(["Earlier progress.", "New progress.", "Final result."]);
    expect(slots().map((s) => s.textContent)).toEqual(["Final result."]);
  });
  it("draws work a wake started after a recap under the recap, without a pill or an empty live placeholder", () => {
    const recap = item(4, "recap", "Earlier context.");
    const resumed = [...first, recap, { ...wake, id: "wake5#0", seq: 5 }, tool(6), item(7, "text", "Resumed progress.")];
    render(resumed, true);
    expect(host.querySelectorAll("[data-segment]")).toHaveLength(2);
    expect(host.querySelector("[data-line] > .flex.justify-center")).toBeNull();
    expect(words().map((s) => s.textContent)).toEqual(["Earlier progress.", "Resumed progress."]);
    render([...resumed, item(8, "text", "Resumed final result.")], false);
    expect(slots().map((s) => s.textContent)).toEqual(["Earlier progress.", "Resumed final result."]);
  });
});
