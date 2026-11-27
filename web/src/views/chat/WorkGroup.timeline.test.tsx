// @vitest-environment jsdom
// The stretches of an exchange: the words in the open in order, each run of work between them one folded row that
// says in words what it did and opens by hand to its rows (super rule 2); thoughts with words are rows (#5155).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const live = vi.hoisted(() => ({ thinking: false }));
vi.mock("../../lib/live", async (load) => ({ ...(await load<typeof import("../../lib/live")>()), useSessionLive: () => (live.thinking ? { thinking: true } : null) }));

import { emptyTaskIndex } from "../../lib/tasks";
import type { Item } from "../../lib/types";
import { segmentIdentity, type Segment, type Step } from "../../lib/turns";
import { ChatContext, type ChatContextValue } from "./chat-context";
import { forgetExpanded } from "./expanded";
import { revealItem, undoReveals } from "./search-reveal";
import { SegmentView } from "./SegmentView";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const T0 = Date.parse("2026-11-04T10:00:00Z");
const at = (s: number) => new Date(T0 + s * 1000).toISOString();
const tool = (n: number, done = true): Step => ({
  kind: "tool", id: `t${n}#0`,
  use: { id: `t${n}#0`, seq: n, role: "assistant", kind: "tool_use", tool_name: "Bash", tool_use_id: `c-${n}`, tool_input: { command: `echo ${n}` }, ts: at(n) },
  result: done ? { id: `t${n}-r#0`, seq: n, role: "user", kind: "tool_result", tool_use_id: `c-${n}`, text: "ok", ts: at(n + 1) } : undefined,
  images: [],
});
const words = (n: number, text: string): Step => {
  const item: Item = { id: `a${n}#0`, seq: n, role: "assistant", kind: "text", text, ts: at(n) };
  return { kind: "text", id: item.id, item };
};
const thought = (n: number, text: string): Step => {
  const item: Item = { id: `th${n}#0`, seq: n, role: "assistant", kind: "thinking", text, ts: at(n) };
  return { kind: "thinking", id: item.id, item };
};
const seg = (steps: Step[]): Segment => ({ id: "seg", steps, tools: steps.length, thoughts: 0, replies: 0, folds: [], start: at(0) });
// a thought without words, one with, a run, the narration, a second run with a long thought, the answer
const finished = [thought(1, ""), thought(2, "Read the config first."), tool(3), words(5, "Found the setting."), tool(6), thought(8, "Line one\nline `two`"), tool(9), words(12, "Final result.")];
const running = [...finished.slice(0, -1), words(12, "Checking once more."), tool(13, false)];

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  forgetExpanded();
  live.thinking = false;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => { act(() => root.unmount()); host.remove(); });
function draw(s: Segment, isLive: boolean, style: "agents" | "assistant" = "agents") {
  const ctx: ChatContextValue = { tasks: emptyTaskIndex(), subagents: [], scope: "x:", lineStyle: style };
  act(() => root.render(<ChatContext.Provider value={ctx}><SegmentView key={segmentIdentity(s)} seg={s} live={isLive} mid="m" sid="s" expandFold={async () => {}} /></ChatContext.Provider>));
}
const groupsShown = () => [...host.querySelectorAll<HTMLElement>("[data-work-group]")];
const head = (g: HTMLElement) => g.querySelector<HTMLButtonElement>(":scope > button")!;
const title = (g: HTMLElement) => head(g).textContent ?? "";
const tap = (b: HTMLElement) => act(() => b.click());
const openAll = () => { for (const g of groupsShown()) if (head(g).getAttribute("aria-expanded") === "false" && !head(g).disabled) tap(head(g)); };
// thought rows come in their own chunk (WorkGroup.tsx lazy): wait for it once something opened
const loaded = () => act(async () => { await vi.dynamicImportSettled(); });
const rows = () => [...host.querySelectorAll("[data-action-row]")].map((r) => r.textContent ?? "");
const thoughtRows = () => [...host.querySelectorAll<HTMLElement>('[data-thinking="words"] > div')];

describe.each(["agents", "assistant"] as const)("%s: the stretches", (style) => {
  it("a finished turn: each stretch a folded row in words, its words in the open between them; opened, every row", async () => {
    draw(seg(finished), false, style);
    expect(groupsShown().map(title)).toEqual(["运行 1 条命令", "运行 2 条命令"]);
    expect(groupsShown().map((g) => head(g).getAttribute("aria-expanded"))).toEqual(["false", "false"]);
    expect(host.textContent).toContain("Found the setting.");
    expect(host.querySelector("[data-latest-text]")?.textContent).toContain("Final result.");
    // in order: the first stretch, the words, the second stretch, the answer
    const text = host.textContent!;
    expect(text.indexOf("运行 1 条命令")).toBeLessThan(text.indexOf("Found the setting."));
    expect(text.indexOf("Found the setting.")).toBeLessThan(text.indexOf("运行 2 条命令"));
    openAll();
    await loaded();
    expect(rows().join(" ")).toContain("echo 3");
    expect(rows().join(" ")).toContain("echo 9");
    // a thought without words is no row; one with words is
    expect(thoughtRows().map((r) => r.textContent)).toEqual(["Read the config first.", "Line one line two"]);
    // backtick code reads as code on the row too, without its backticks
    expect(thoughtRows()[1].querySelector('[data-thinking-markup="code"]')?.textContent).toBe("two");
    expect(host.textContent).not.toContain("思考");
  });

  it("each stretch keeps its own open / closed choice, also after a remount", () => {
    draw(seg(finished), false, style);
    tap(head(groupsShown()[1]));
    act(() => root.unmount());
    root = createRoot(host);
    draw(seg(finished), false, style);
    expect(groupsShown().map((g) => head(g).getAttribute("aria-expanded"))).toEqual(["false", "true"]);
  });

  it("a thought with a line break opens in place to its whole text and stays open after a remount", async () => {
    draw(seg(finished), false, style);
    openAll();
    await loaded();
    const long = thoughtRows()[1];
    expect(long.getAttribute("aria-expanded")).toBe("false");
    tap(long);
    expect(thoughtRows()[1].getAttribute("aria-expanded")).toBe("true");
    expect(thoughtRows()[1].textContent).toBe("Line one\nline two");
    expect(thoughtRows()[1].querySelector('[data-thinking-markup="code"]')?.textContent).toBe("two");
    expect(thoughtRows()[1].className).toContain("items-start");
    act(() => root.unmount());
    root = createRoot(host);
    draw(seg(finished), false, style);
    await loaded();
    expect(thoughtRows()[1].getAttribute("aria-expanded")).toBe("true");
  });

  it("live: the last stretch is Working… with the clock, earlier ones are done; nothing opens by itself", async () => {
    draw(seg(running), true, style);
    const [first, second, going] = groupsShown();
    expect([title(first), title(second)]).toEqual(["运行 1 条命令", "运行 2 条命令"]);
    expect(going.dataset.workGroup).toBe("live");
    expect(title(going)).toMatch(/^正在工作… · 运行 1 条命令/);
    expect(going.querySelector("[data-work-elapsed]")).not.toBeNull();
    expect(host.querySelectorAll("[data-work-elapsed]")).toHaveLength(1);
    expect(groupsShown().map((g) => head(g).getAttribute("aria-expanded"))).toEqual(["false", "false", "false"]);
    expect(host.textContent).toContain("Found the setting.");
    expect(host.textContent).toContain("Checking once more.");
    tap(head(going));
    await loaded();
    expect(rows().join(" ")).toContain("echo 13");
  });

  it("the live Thinking line is drawn once, in the live stretch", () => {
    live.thinking = true;
    draw(seg(running), true, style);
    tap(head(groupsShown().at(-1)!));
    const lines = host.querySelectorAll('[data-thinking="live"]');
    expect(lines).toHaveLength(1);
    expect(groupsShown().at(-1)?.contains(lines[0])).toBe(true);
    expect(lines[0].textContent).toContain("正在思考");
  });

  it("an opened thought's words can be selected and copied: a click that ends a selection does not fold it; keys toggle it", async () => {
    draw(seg(finished), false, style);
    openAll();
    await loaded();
    const row = () => thoughtRows()[1];
    expect(row().getAttribute("role")).toBe("button");
    expect(row().tabIndex).toBe(0);
    act(() => row().dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
    expect(row().getAttribute("aria-expanded")).toBe("true");
    const words = row().querySelector<HTMLElement>("span.italic")!;
    expect(words.className).toContain("select-text");
    // the reader selected some of its words: the click that ends it leaves the row open
    const range = document.createRange();
    range.selectNodeContents(words);
    getSelection()!.removeAllRanges();
    getSelection()!.addRange(range);
    act(() => words.click());
    expect(row().getAttribute("aria-expanded")).toBe("true");
    getSelection()!.removeAllRanges();
    act(() => words.click());
    expect(row().getAttribute("aria-expanded")).toBe("false");
    act(() => row().dispatchEvent(new KeyboardEvent("keydown", { key: " ", bubbles: true })));
    expect(row().getAttribute("aria-expanded")).toBe("true");
  });

  it("thoughts without words are no steps: a turn of nothing else has no stretch, and they split no rows", async () => {
    draw(seg([thought(1, ""), words(4, "Just an answer.")]), false, style);
    expect(groupsShown()).toHaveLength(0);
    expect(host.querySelector("[data-latest-text]")?.textContent).toContain("Just an answer.");
    // live, the turn still says it is at work
    draw(seg([thought(1, "")]), true, style);
    expect(groupsShown()).toHaveLength(1);
    expect(head(groupsShown()[0]).disabled).toBe(true);
    // between two pieces of words they make no empty stretch; between two calls they leave one block of rows
    draw(seg([tool(1), words(2, "First."), thought(3, ""), words(4, "Second."), tool(5), thought(6, " "), tool(7), words(9, "Done.")]), false, style);
    expect(groupsShown().map(title)).toEqual(["运行 1 条命令", "运行 2 条命令"]);
    tap(head(groupsShown()[1]));
    await loaded();
    const blocks = [...groupsShown()[1].querySelectorAll("[data-action-row]")].map((r) => r.parentElement);
    expect(blocks[0]).toBe(blocks[1]);
  });

  it("while the exchange runs, a call without its result spins in a finished stretch too; in history it does not", () => {
    const steps = [tool(1, false), words(2, "Started the build."), tool(3), words(4, "Checking the logs.")];
    const status = () => [...host.querySelectorAll("[data-action-row] [role=img]")].map((e) => e.getAttribute("aria-label"));
    draw(seg(steps), true, style);
    expect(groupsShown().slice(0, 2).map((g) => g.dataset.workGroup)).toEqual(["done", "done"]);
    openAll();
    expect(status()).toEqual(["进行中", "完成"]);
    draw(seg([...steps, words(9, "Done.")]), false, style);
    openAll();
    expect(status()[0]).toBe("完成");
  });

  it("a search hit in a thought opens its stretch and the row; Back closes them again", async () => {
    draw(seg(finished), false, style);
    act(() => revealItem("x:", "th8#0"));
    // the row's chunk comes in after the stretch opened: the hit is still asked for, so it opens on arrival
    await loaded();
    expect(groupsShown().map((g) => head(g).getAttribute("aria-expanded"))).toEqual(["false", "true"]);
    expect(thoughtRows()[0].getAttribute("aria-expanded")).toBe("true");
    act(() => undoReveals());
    expect(head(groupsShown()[1]).getAttribute("aria-expanded")).toBe("false");
  });
});
