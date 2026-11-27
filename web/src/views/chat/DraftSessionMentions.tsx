import { Fragment, useLayoutEffect, useRef, type RefObject } from "react";
import { sessionMentionRanges } from "../../lib/inline-mentions";

/** Paints the highlight beneath the native textarea, which keeps its text, caret, selection and IME. */
export function DraftSessionMentions({ input, text, ranges, imeComposing = false }: {
  input: RefObject<HTMLTextAreaElement | null>;
  text: string;
  ranges: ReturnType<typeof sessionMentionRanges>;
  imeComposing?: boolean;
}) {
  const layer = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const field = input.current;
    const mirror = layer.current;
    if (!field || !mirror) return;
    const scroll = () => { mirror.scrollTop = field.scrollTop; };
    const size = () => {
      const css = getComputedStyle(field);
      Object.assign(mirror.style, {
        width: `${field.clientWidth}px`, height: `${field.clientHeight}px`,
        boxSizing: css.boxSizing, padding: css.padding, font: css.font, lineHeight: css.lineHeight,
        letterSpacing: css.letterSpacing, wordSpacing: css.wordSpacing, tabSize: css.tabSize, direction: css.direction,
      });
      scroll();
    };
    size();
    const observer = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(size);
    observer?.observe(field);
    field.addEventListener("scroll", scroll, { passive: true });
    return () => {
      observer?.disconnect();
      field.removeEventListener("scroll", scroll);
    };
  }, [input, text, ranges.length]);
  if (!ranges.length) return null;

  return (
    <div
      ref={layer}
      aria-hidden="true"
      className={
        "pointer-events-none absolute left-0 top-0 overflow-hidden whitespace-pre-wrap [overflow-wrap:anywhere] " +
        (imeComposing ? "text-transparent" : "text-ink")
      }
    >
      {ranges.map(({ start, end }, i) => (
        <Fragment key={start}>
          {text.slice(ranges[i - 1]?.end ?? 0, start)}
          <span
            data-draft-session-mention
            className={
              "rounded-chip bg-accent/18 dark:bg-accent/24 [box-decoration-break:clone] " +
              (imeComposing ? "text-transparent" : "text-accent-ink")
            }
          >
            {text.slice(start, end)}
          </span>
        </Fragment>
      ))}
      {text.slice(ranges.at(-1)?.end)}{"\u200b"}
    </div>
  );
}
