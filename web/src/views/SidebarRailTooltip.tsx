import { useEffect, useState } from "react";
import { createTooltipController, TooltipRoot, UiTooltipContent } from "../ui/UiTooltip";

export interface RailTooltipState {
  text: string;
  anchor: HTMLElement;
}

/** Lazy-loaded `@ui-sdk/react/tooltip` host for the collapsed sidebar rail so tooltip/floating code stays off the first screen. */
export function SidebarRailTooltip({ active }: { active: RailTooltipState | null }) {
  const [controller] = useState(() => createTooltipController<string>());
  useEffect(() => {
    if (active) {
      controller.open({ payload: active.text, anchor: active.anchor });
    } else {
      controller.close();
    }
  }, [active, controller]);
  return (
    <TooltipRoot controller={controller} delay={0}>
      <UiTooltipContent side="right" sideOffset={10}>
        {(state) => (typeof state.payload === "string" ? state.payload : null)}
      </UiTooltipContent>
    </TooltipRoot>
  );
}
