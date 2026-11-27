import { cx } from "./atoms";

const GRADIENT = {
  page: "from-page via-page/75 to-transparent",
  surface: "from-surface via-surface/75 to-transparent",
  canvas: "from-canvas via-canvas/75 to-transparent",
} as const;

export function ScrollFade({
  surface = "page",
  height = "h-7",
  className,
}: {
  surface?: "page" | "surface" | "canvas";
  height?: string;
  className?: string;
}) {
  return (
    <div
      aria-hidden
      className={cx("pointer-events-none absolute inset-x-0 bottom-0 bg-linear-to-t", height, GRADIENT[surface], className)}
    />
  );
}
