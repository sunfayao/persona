// A thought in a work group, as cindy draws it (WorkGroupBlock.tsx ThinkingActivityRow / ExpandedThinkingRow, #5155):
// one row in the tool rows' chrome, the sparkle in the status slot and its words in italic on one line, `**strong**`
// and backtick code read as such (ThinkingText.tsx); it opens in place to the whole thought when they do not fit or
// have a line break. A thought without words is no row at all (Claude Code mostly writes thoughts without words:
// "Thought for 1s" rows say nothing; work-groups.ts leaves them out). Opened, its words can be selected and copied: the
// row is a div with the button role (a <button> takes no selection), and a click that ends a selection does not fold it.
// Drawn only inside an opened summary, so WorkGroup loads it lazily: off the first screen, with the emphasis parser.
import { type KeyboardEvent, type MouseEvent, useLayoutEffect, useMemo, useRef, useState } from "react";
import { thinkingPlainText, tokenizeThinkingText } from "../../lib/thinking-text";
import type { Item } from "../../lib/types";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { CHEVRON_SLOT, ROW_HOVER, STATUS_SLOT } from "./ActionRow";
import { useExpanded } from "./expanded";
import { useReveal } from "./search-reveal";
import { StepTelemetry } from "./StepTelemetry";
import { ThinkingText } from "./ThinkingText";

const NONE: string[] = [];

export function ThinkingRow({ item, scope = "" }: { item: Item; scope?: string }) {
  const raw = item.text?.trim() ?? "";
  const words = raw.replace(/\s+/g, " ");
  const lineBreak = /[\r\n]/.test(raw);
  const [open, setOpen] = useExpanded(`${scope}thinking:${item.id}`);
  const [overflows, setOverflows] = useState(false);
  const text = useRef<HTMLSpanElement>(null);
  // whether one line holds it: measured on the line itself, again whenever its width changes
  useLayoutEffect(() => {
    const el = text.current;
    if (open || !el) return;
    const measure = () => setOverflows(el.scrollWidth > el.clientWidth + 1);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const watch = new ResizeObserver(measure);
    watch.observe(el);
    return () => watch.disconnect();
  }, [words, open]);
  const opens = open || lineBreak || overflows;
  // tokenized once: what it draws and its hover text
  const tokens = useMemo(() => tokenizeThinkingText(open ? raw : words), [open, raw, words]);
  const title = useMemo(() => (open ? undefined : thinkingPlainText(tokens)), [open, tokens]);
  const hasTelemetry = Boolean(item.usage?.length || (typeof item.ttft_ms === "number" && item.ttft_ms >= 0));
  const stepItems = useMemo(() => [item], [item]);
  // a search hit in its words (search-reveal.ts): it opens
  useReveal(scope, words ? [item.id] : NONE, () => {
    if (open) return;
    setOpen(true);
    return () => setOpen(false);
  });
  if (!words) return null;
  const click = (e: MouseEvent<HTMLDivElement>) => {
    // the end of selecting its words (to copy them) is no click on the row
    const sel = window.getSelection();
    if (open && sel && !sel.isCollapsed && sel.anchorNode && e.currentTarget.contains(sel.anchorNode)) return;
    setOpen((o) => !o);
  };
  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    setOpen((o) => !o);
  };
  return (
    <div className="flex min-w-0 flex-col" data-thinking="words" data-items={item.id}>
      <div
        role={opens ? "button" : undefined}
        tabIndex={opens ? 0 : undefined}
        aria-expanded={opens ? open : undefined}
        onClick={opens ? click : undefined}
        onKeyDown={opens ? key : undefined}
        className={cx(
          "flex w-full min-w-0 gap-2 rounded-[10px] min-h-[38px] px-3.5 py-2 [@media(pointer:coarse)]:min-h-10 text-left",
          open ? "items-start" : "items-center",
          opens ? cx(ROW_HOVER, "cursor-pointer focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent") : "cursor-default",
        )}
      >
        <span aria-hidden className={cx(STATUS_SLOT, open && "mt-[1px]", opens && "transition-colors duration-150 group-hover:text-ink motion-reduce:transition-none")}>
          <Icon name="spark" size={13} />
        </span>
        <span
          ref={text}
          title={title}
          className={cx(
            "min-w-0 text-[length:var(--chat-secondary)] text-thinking-ink italic",
            opens && "transition-colors duration-150 group-hover:text-ink motion-reduce:transition-none",
            open ? "cursor-text whitespace-pre-wrap select-text [overflow-wrap:anywhere]" : "truncate",
          )}
        >
          <ThinkingText tokens={tokens} />
        </span>
        <span aria-hidden className={cx(CHEVRON_SLOT, open && "mt-[1px] group-hover:translate-x-0")}>
          {opens && <Icon name="chevronRight" size={13} stroke={2.1} className="transition-transform duration-200 ease-(--ease-out-strong) motion-reduce:transition-none" style={{ transform: open ? "rotate(90deg)" : "none" }} />}
        </span>
      </div>
      {open && hasTelemetry && <StepTelemetry items={stepItems} className="mt-0.5 mb-2 pl-9" />}
    </div>
  );
}
