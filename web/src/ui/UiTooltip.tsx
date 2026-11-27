import { useEffect, useState, type ReactElement, type ReactNode } from "react";
import type { PartProps } from "@ui-sdk/react/primitive";
import {
  Tooltip as TooltipRoot,
  TooltipContent,
  TooltipShortcut,
  TooltipTrigger,
  createTooltipController,
  type TooltipContentProps,
  type TooltipContentState,
} from "@ui-sdk/react/tooltip";
import { cx } from "./atoms";

export { createTooltipController, TooltipRoot, TooltipTrigger, TooltipShortcut };

const TOOLTIP_SURFACE =
  "ui-tooltip-surface pointer-events-none rounded-[8px] bg-ink px-2 py-1 text-submeta font-medium leading-tight text-page shadow-float-sm";

export type UiTooltipContentProps<Payload = unknown> = PartProps<
  "div",
  TooltipContentState<Payload>
> &
  TooltipContentProps;

export function UiTooltipContent<Payload = unknown>({
  side = "top",
  align = "center",
  sideOffset = 6,
  collisionPadding = 8,
  className,
  children,
  ...rest
}: UiTooltipContentProps<Payload>) {
  // Gate top-layer portal behind client hydration so `renderToStaticMarkup` in jsdom never throws on createPortal.
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  if (!mounted) return null;
  return (
    <TooltipContent<Payload>
      side={side}
      align={align}
      sideOffset={sideOffset}
      collisionPadding={collisionPadding}
      className={
        typeof className === "function"
          ? (state) => cx(TOOLTIP_SURFACE, className(state))
          : cx(TOOLTIP_SURFACE, className)
      }
      {...rest}
    >
      {children}
    </TooltipContent>
  );
}

export interface UiTooltipProps {
  label: ReactNode;
  shortcut?: string;
  side?: TooltipContentProps["side"];
  align?: TooltipContentProps["align"];
  sideOffset?: number;
  delay?: number;
  disabled?: boolean;
  children: ReactElement;
}

export function UiTooltip({
  label,
  shortcut,
  side = "top",
  align = "center",
  sideOffset = 6,
  delay = 350,
  disabled,
  children,
}: UiTooltipProps) {
  return (
    <TooltipRoot delay={delay} disabled={disabled} shortcut={shortcut}>
      <TooltipTrigger render={children} />
      <UiTooltipContent side={side} align={align} sideOffset={sideOffset}>
        {label}
        {shortcut && <TooltipShortcut className="ml-1.5 opacity-70" />}
      </UiTooltipContent>
    </TooltipRoot>
  );
}
