import type { ReactNode } from "react";
import {
  SegmentedControl,
  SegmentedControlIndicator,
  SegmentedControlItem,
} from "@ui-sdk/react/segmented-control";
import { cx } from "./atoms";

export interface UnderlineTabItem<K extends string = string> {
  key: K;
  label: ReactNode;
  ariaLabel?: string;
  count?: number;
  lead?: ReactNode;
  /** Subtle emphasis when unselected but has active items (e.g. working > 0). */
  emphasized?: boolean;
  /** Extra data-* attributes for tests/selectors (e.g. { "data-usage-filter": "all" }). */
  dataAttrs?: Record<string, string | undefined>;
}

export function UnderlineTabs<K extends string = string>({
  items,
  value,
  onChange,
  ariaLabel,
  size = "sm",
  className,
  wrap = false,
}: {
  items: readonly UnderlineTabItem<K>[];
  value: K;
  onChange: (key: K) => void;
  /** Accepted for caller compatibility; SegmentedControl manages native radiogroup semantics. */
  role?: "radiogroup" | "tablist" | "group";
  ariaLabel: string;
  size?: "xs" | "sm";
  className?: string;
  wrap?: boolean;
}) {
  const tabSize =
    size === "xs"
      ? "gap-1 py-1.5 text-submeta leading-none"
      : "gap-1.5 py-2 text-label leading-none";

  return (
    <SegmentedControl
      aria-label={ariaLabel}
      value={value}
      onValueChange={(next) => {
        if (next != null) onChange(next as K);
      }}
      data-variant="underline"
      className={cx(
        "relative flex items-center border-b border-line/60",
        wrap
          ? size === "xs"
            ? "flex-wrap gap-x-3 gap-y-0.5"
            : "flex-wrap gap-x-3.5 gap-y-0.5"
          : size === "xs"
            ? "gap-3 overflow-x-auto scrollbar-none"
            : "gap-3.5 overflow-x-auto scrollbar-none",
        className,
      )}
    >
      <SegmentedControlIndicator data-underline-indicator="" />
      {items.map((item) => {
        const selected = item.key === value;
        const attrs = (item.dataAttrs ?? {}) as Record<`data-${string}`, string | undefined>;
        return (
          <SegmentedControlItem
            key={item.key}
            value={item.key}
            aria-label={item.ariaLabel}
            data-selected={selected}
            {...attrs}
            className={cx(
              "touch-hit relative inline-flex shrink-0 items-center whitespace-nowrap transition-colors duration-150 outline-none focus-visible:text-ink",
              tabSize,
              selected
                ? "font-medium text-ink"
                : item.emphasized
                  ? "text-ink-2 hover:text-ink"
                  : "text-ink-3 hover:text-ink-2",
            )}
          >
            {item.lead}
            <span>{item.label}</span>
            {item.count !== undefined && (
              <span className={cx("text-submeta font-medium tabular-nums", selected ? "text-ink-2" : "text-ink-3")}>
                {item.count}
              </span>
            )}
          </SegmentedControlItem>
        );
      })}
    </SegmentedControl>
  );
}

export interface FloatingSegmentItem<K extends string = string> {
  key: K;
  label: ReactNode;
  ariaLabel?: string;
  dataAttrs?: Record<string, string | undefined>;
}

export function FloatingSegment<K extends string = string>({
  items,
  value,
  onChange,
  ariaLabel,
  size = "xs",
  className,
}: {
  items: readonly FloatingSegmentItem<K>[];
  value: K;
  onChange: (key: K) => void;
  ariaLabel: string;
  size?: "xs" | "sm";
  className?: string;
}) {
  const itemSize =
    size === "xs"
      ? "px-2 py-1 text-submeta leading-none"
      : "px-2.5 py-1.5 text-label leading-none";

  return (
    <SegmentedControl
      data-variant="float"
      aria-label={ariaLabel}
      value={value}
      onValueChange={(next) => {
        if (next != null) onChange(next as K);
      }}
      className={cx(
        "inline-flex items-center gap-0.5 rounded-[9px] bg-hover/55 p-0.5",
        className,
      )}
    >
      <SegmentedControlIndicator />
      {items.map((item) => {
        const selected = item.key === value;
        const attrs = (item.dataAttrs ?? {}) as Record<`data-${string}`, string | undefined>;
        return (
          <SegmentedControlItem
            key={item.key}
            value={item.key}
            aria-label={item.ariaLabel}
            data-selected={selected}
            {...attrs}
            className={cx(
              "touch-hit relative inline-flex shrink-0 items-center justify-center rounded-[7px] whitespace-nowrap transition-colors duration-150 outline-none focus-visible:text-ink",
              itemSize,
              selected ? "font-medium text-ink" : "text-ink-3 hover:text-ink",
            )}
          >
            {item.label}
          </SegmentedControlItem>
        );
      })}
    </SegmentedControl>
  );
}
