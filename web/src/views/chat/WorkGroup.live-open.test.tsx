// @vitest-environment jsdom
// One user message, one continuous answer: the words in order, each stretch of work between them one folded row,
// nothing replaced while the agent works, the same on every conversation.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptyTaskIndex } from "../../lib/tasks";
import type { Item } from "../../lib/types";
import { segments, segmentIdentity, type Segment, type Step } from "../../lib/turns";
import type { WorkLineStyle } from "../../lib/work-groups";
import { ChatContext, type ChatContextValue } from "./chat-context";
import { forgetExpanded, openExpanded } from "./expanded";
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
const seg = (steps: Step[]): Segment => ({ id: "seg", steps, tools: steps.length, thoughts: 0, replies: 0, folds: [], start: at(0) });
let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  forgetExpanded();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => { act(() => root.unmount()); host.remove(); });
function draw(style: WorkLineStyle, s: Segment, live: boolean, expand: (f: Item) => Promise<void> = async () => {}, streaming?: string) {
  const ctx: ChatContextValue = { tasks: emptyTaskIndex(), subagents: [], scope: "x:", lineStyle: style };
  act(() => root.render(<ChatContext.Provider value={ctx}><SegmentView key={segmentIdentity(s)} seg={s} live={live} mid="m" sid="s" expandFold={expand} streaming={streaming} /></ChatContext.Provider>));
}
const headers = () => [...host.querySelectorAll<HTMLButtonElement>("[data-work-group] > button")];
const header = () => headers()[0];
const detailText = () => [...host.querySelectorAll("[data-work-detail]")].map((d) => d.textContent ?? "").join("\n");
const slot = () => host.querySelector("[data-latest-text]");
const tap = (b = header()) => act(() => b.click());
/** What the exchange shows, in order: each paragraph's words and each stretch's row. */
const blocks = () => [...host.querySelectorAll<HTMLElement>("[data-segment] > [data-words], [data-segment] > [data-work-group]")]
  .map((el) => (el.dataset.workGroup ? `[${el.dataset.workGroup}]` : (el.querySelector("[data-answer]")?.textContent ?? "").trim()));
const running = [tool(1), words(2, "First progress."), tool(3), words(4, "Latest progress."), tool(5, false)];
const finished = [...running.slice(0, -1), tool(5), words(9, "Final result.")];

describe.each(["assistant", "agents"] as const)("%s: one continuous answer", (style) => {
  it("while working, every paragraph stays in order with each stretch folded between them, the live row last", () => {
    draw(style, seg(running), true);
    expect(blocks()).toEqual(["[done]", "First progress.", "[done]", "Latest progress.", "[live]"]);
    expect(headers().map((h) => h.getAttribute("aria-expanded"))).toEqual(["false", "false", "false"]);
    expect(host.querySelector("[data-work-detail]")).toBeNull();
    expect(host.textContent).not.toContain("echo 5");
    expect(host.querySelector("[data-live-preview]")).toBeNull();
    // the words never say how long a stretch took
    expect(host.textContent).not.toContain("已工作");
  });
  it("adds new words after the others: an earlier paragraph stays the same element, nothing is replaced", () => {
    draw(style, seg(running), true);
    const first = [...host.querySelectorAll("[data-words]")][0];
    draw(style, seg([...running.slice(0, -1), tool(5), words(6, "New progress."), tool(7, false)]), true);
    expect([...host.querySelectorAll("[data-words]")][0]).toBe(first);
    expect(blocks()).toEqual(["[done]", "First progress.", "[done]", "Latest progress.", "[done]", "New progress.", "[live]"]);
  });
  it("keeps long intermediate Markdown in the open", () => {
    draw(style, seg([tool(1), words(2, `## Intermediate report\n${"Example progress. ".repeat(50)}`), tool(3), words(4, "Final result.")]), false);
    expect(host.textContent).toContain("Intermediate report");
    expect(slot()?.textContent).toContain("Final result.");
  });
  it("opens a single running call manually; later calls in the same stretch do not reset the choice", () => {
    draw(style, seg([tool(1, false)]), true);
    expect(header().disabled).toBe(false);
    tap();
    expect(detailText()).toContain("echo 1");
    draw(style, seg([tool(1), tool(2, false)]), true);
    expect(header().getAttribute("aria-expanded")).toBe("true");
    expect(detailText()).toContain("echo 2");
    tap();
    draw(style, seg([tool(1), tool(2), tool(3, false)]), true);
    expect(header().getAttribute("aria-expanded")).toBe("false");
  });
  it("completion collapses a live stretch the reader opened; the last words end the answer, the time worked said once under them", async () => {
    draw(style, seg(running), true);
    tap(headers()[2]);
    expect(detailText()).toContain("echo 5");
    draw(style, seg(finished), false);
    expect(headers()[2].getAttribute("aria-expanded")).toBe("false");
    expect(blocks()).toEqual(["[done]", "First progress.", "[done]", "Latest progress.", "[done]", "Final result."]);
    expect(slot()?.textContent).toContain("Final result.");
    // the reply bar's chunk: the exchange's whole time, from its start to its last words, once and only there
    await act(async () => { await vi.dynamicImportSettled(); });
    const said = [...host.querySelectorAll("[data-work-time]")];
    expect(said.map((e) => e.textContent)).toEqual(["已工作 9s"]);
    expect(slot()?.contains(said[0])).toBe(true);
    expect(host.textContent?.split("已工作").length).toBe(2);
  });
  it("streaming words come after everything, each paragraph once, and land without a duplicate", () => {
    const count = (text: string) => (host.textContent ?? "").split(text).length - 1;
    draw(style, seg(running), true, undefined, "New streamed progress.");
    expect(count("Latest progress.")).toBe(1);
    expect(count("New streamed progress.")).toBe(1);
    expect(host.querySelectorAll('[data-answer="streaming"]')).toHaveLength(1);
    // the streamed words after the work: the stretch before them is over, and the words stand for the live row
    expect(blocks().slice(-2)).toEqual(["[done]", "New streamed progress."]);
    draw(style, seg([...running.slice(0, -1), tool(5), words(9, "New streamed progress.")]), true, undefined, "New streamed progress.");
    expect(count("New streamed progress.")).toBe(1);
    draw(style, seg(finished), false);
    expect(count("Final result.")).toBe(1);
    expect(count("Latest progress.")).toBe(1);
    expect(host.querySelectorAll('[data-answer="streaming"]')).toHaveLength(0);
  });
  it("keeps the words written before the first tool when it arrives", () => {
    const steps = [words(1, "Earlier text."), words(2, "Latest text.")];
    draw(style, seg(steps), true);
    draw(style, seg([...steps, tool(3, false)]), true);
    expect(blocks()).toEqual(["Earlier text.", "Latest text.", "[live]"]);
  });
  it("does not drop earlier text across an unloaded history gap", () => {
    const first = words(1, "Earlier history text.");
    const later = words(2, "Latest history text.") as Extract<Step, { kind: "text" }>;
    draw(style, seg([first, { ...later, item: { ...later.item, ts: at(3600) } }]), false);
    expect(host.textContent).toContain("Earlier history text.");
    expect(slot()?.textContent).toContain("Latest history text.");
  });
  it("jump-to-call opens a running stretch and completion still collapses it", () => {
    draw(style, seg(running), true);
    const live = host.querySelector<HTMLElement>('[data-work-group="live"]')!;
    act(() => openExpanded([`x:${live.dataset.groupId}`]));
    expect(headers()[2].getAttribute("aria-expanded")).toBe("true");
    expect(detailText()).toContain("echo 5");
    draw(style, seg(finished), false);
    expect(headers()[2].getAttribute("aria-expanded")).toBe("false");
  });
  it("all tool kinds and every plan update stay inside their stretch until manually opened", async () => {
    const named = (n: number, name: string, input: Record<string, unknown>): Step => {
      const s = tool(n) as Extract<Step, { kind: "tool" }>;
      return { ...s, use: { ...s.use, tool_name: name, tool_input: input } };
    };
    const steps = [
      named(1, "TodoWrite", { todos: [{ content: "First plan", status: "pending" }, { content: "Second step", status: "pending" }] }),
      named(2, "TodoWrite", { todos: [{ content: "Revised plan", status: "pending" }, { content: "Last step", status: "pending" }] }),
      named(3, "Agent", { description: "Example subagent", prompt: "Check the example" }),
      named(4, "Workflow", { name: "Example workflow" }), words(5, "Latest text."),
    ];
    draw(style, seg(steps), true);
    expect(host.querySelectorAll("[data-work-group]")).toHaveLength(2);
    expect(host.textContent).not.toContain("Example subagent");
    expect(host.textContent).not.toContain("Example workflow");
    await act(async () => { header().click(); await vi.dynamicImportSettled(); });
    expect(detailText()).toContain("Example subagent");
    expect(detailText()).toContain("Example workflow");
    // one plan card, the latest list (#5156, cindy); the earlier update stays inspectable as a row
    const detail = host.querySelector("[data-work-detail]")!;
    const cards = detail.querySelectorAll("[data-plan-card]");
    expect(cards).toHaveLength(1);
    expect(cards[0].textContent).toContain("Revised plan");
    expect(cards[0].textContent).not.toContain("First plan");
    expect(detail.querySelector('[data-action-row="TodoWrite"]')).not.toBeNull();
  });
  it("reloaded completed history starts folded and remains manually inspectable", () => {
    draw(style, seg(finished), false);
    expect(headers().map((h) => h.getAttribute("aria-expanded"))).toEqual(["false", "false", "false"]);
    tap();
    expect(detailText()).toContain("echo 1");
    expect(detailText()).not.toContain("echo 3");
  });
});

const fold = (a: number, b: number): Step => {
  const item: Item = { id: `fold:t${a}#0`, seq: a, role: "assistant", kind: "fold", ts: at(a), fold: { items: (b - a + 1) * 2, tools: b - a + 1, thoughts: 0, until: at(b + 1), through_seq: b } };
  return { kind: "fold", id: item.id, item };
};
const calls = (a: number, b: number) => Array.from({ length: b - a + 1 }, (_, i) => tool(a + i));
function foldingChat(style: WorkLineStyle, start: Step[], live: boolean) {
  let steps = start;
  const expand = vi.fn(async (f: Item) => {
    steps = steps.flatMap((s) => s.kind === "fold" && s.item.id === f.id ? calls(f.seq, f.fold!.through_seq) : [s]);
    draw(style, seg(steps), live, expand);
  });
  return { expand, draw: () => draw(style, seg(steps), live, expand) };
}
const settle = async () => { for (let i = 0; i < 10; i++) await act(() => new Promise<void>((r) => setTimeout(r, 2))); };
describe.each(["assistant", "agents"] as const)("%s folded history", (style) => {
  it("keeps manual expansion when a fold resolves to a nonzero tool block", async () => {
    const folded = fold(1, 1);
    const text = words(3, "Latest text.");
    const first = tool(1) as Extract<Step, { kind: "tool" }>;
    const expanded = { ...first, id: "t1#1", use: { ...first.use, id: "t1#1" } };
    const sourceSegment = (steps: Step[]) => segments(steps.flatMap((s) => s.kind === "tool" ? [s.use, ...(s.result ? [s.result] : [])] : s.kind === "child_session" ? [] : [s.item]))[0];
    const before = sourceSegment([folded, text]);
    const after = sourceSegment([expanded, text]);
    expect(before.id).not.toBe(after.id);
    const expand = vi.fn(async () => draw(style, after, true, expand));
    draw(style, before, true, expand);
    const originalSlot = slot();
    tap();
    await settle();
    expect(slot()).toBe(originalSlot);
    expect(expand).toHaveBeenCalledTimes(1);
    expect(header().getAttribute("aria-expanded")).toBe("true");
    expect(detailText()).toContain("echo 1");
  });
  it.each([true, false])("fetches a stretch's folded tools only when it is opened by hand (live=%s)", async (live) => {
    const chat = foldingChat(style, [fold(1, 3), words(4, "Earlier progress."), fold(5, 7), words(8, "Latest text.")], live);
    chat.draw();
    await settle();
    expect(chat.expand).not.toHaveBeenCalled();
    expect(host.textContent).toContain("Earlier progress.");
    tap(headers()[0]);
    await settle();
    expect(chat.expand.mock.calls.map(([f]) => f.seq)).toEqual([1]);
    tap(headers()[1]);
    await settle();
    expect(new Set(chat.expand.mock.calls.map(([f]) => f.seq))).toEqual(new Set([1, 5]));
    for (const n of [1, 2, 3, 5, 6, 7]) expect(detailText()).toContain(`echo ${n}`);
    expect(host.querySelectorAll('[data-work-group] > button span[style*="width: 11px"]')).toHaveLength(0);
  });
});
