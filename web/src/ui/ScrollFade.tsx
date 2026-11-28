import { cx } from "./atoms";

const GRADIENT = {
  page: "from-page via-page/75 to-transparent",
  surface: "from-surface via-surface/75 to-transparent",
  canvas: "from-canvas via-canvas/75 to-transparent",
} as const;

/**
 * The fade over a scroller's end (docs/design/DESIGN_SYSTEM.md 3.1), in the scroller's positioned parent. It is
 * `--spacing-fade` tall: a box that wants another height sets that variable on the parent, never a height here, so the
 * end padding follows. The scroller's content ends with `pb-fade-end` and the scroller has `scroll-pb-fade-end`: its
 * last line rests above where the fade begins, and a control focused there is scrolled clear of it.
 */
export function ScrollFade({ surface = "page", className }: { surface?: "page" | "surface" | "canvas"; className?: string }) {
  return (
    <div
      aria-hidden
      data-scroll-fade
      className={cx("pointer-events-none absolute inset-x-0 bottom-0 h-fade bg-linear-to-t", GRADIENT[surface], className)}
    />
  );
}
