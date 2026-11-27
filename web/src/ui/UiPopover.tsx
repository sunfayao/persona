import type { PartProps } from "@ui-sdk/react/primitive";
import {
  Popover as UiPopover,
  PopoverClose as UiPopoverClose,
  PopoverContent,
  PopoverDescription as UiPopoverDescription,
  PopoverTitle as UiPopoverTitle,
  PopoverTrigger as UiPopoverTrigger,
  createPopoverController,
  type PopoverContentProps,
  type PopoverContentState,
  type PopoverOptions,
} from "@ui-sdk/react/popover";
import { cx } from "./atoms";

export {
  UiPopover,
  UiPopoverTrigger,
  UiPopoverTitle,
  UiPopoverDescription,
  UiPopoverClose,
  createPopoverController,
};

const POPOVER_SURFACE =
  "ui-popover-surface rounded-2xl bg-surface text-ink shadow-overlay outline-none";

export type UiPopoverContentProps<Payload = unknown> = PartProps<
  "div",
  PopoverContentState<Payload>
> &
  PopoverContentProps & {
    /** Accepted for caller convenience; prefer setting `presentation` on `<UiPopover>` root where `@ui-sdk/react/popover` reads it. */
    presentation?: PopoverOptions["presentation"];
  };

export function UiPopoverContent<Payload = unknown>({
  anchor,
  side = "bottom",
  align = "center",
  sideOffset = 8,
  collisionPadding = 12,
  presentation,
  className,
  children,
  ...rest
}: UiPopoverContentProps<Payload>) {
  void presentation;
  return (
    <PopoverContent<Payload>
      anchor={anchor}
      side={side}
      align={align}
      sideOffset={sideOffset}
      collisionPadding={collisionPadding}
      className={
        typeof className === "function"
          ? (state) => cx(POPOVER_SURFACE, className(state))
          : cx(POPOVER_SURFACE, className)
      }
      {...rest}
    >
      {children}
    </PopoverContent>
  );
}
