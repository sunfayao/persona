import type { ButtonHTMLAttributes, ReactNode } from "react";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@ui-sdk/react/collapsible";

export interface DisclosureProps {
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  disabled?: boolean;
  className?: string;
  children: ReactNode;
}

export function Disclosure({
  open,
  defaultOpen,
  onOpenChange,
  disabled,
  className,
  children,
}: DisclosureProps) {
  return (
    <Collapsible
      open={open}
      defaultOpen={defaultOpen}
      onOpenChange={onOpenChange ? (next) => onOpenChange(next) : undefined}
      disabled={disabled}
      className={className}
    >
      {children}
    </Collapsible>
  );
}

export type DisclosureTriggerProps = ButtonHTMLAttributes<HTMLButtonElement>;

export function DisclosureTrigger({ className, children, ...rest }: DisclosureTriggerProps) {
  return (
    <CollapsibleTrigger {...rest} className={className}>
      {children}
    </CollapsibleTrigger>
  );
}

export interface DisclosurePanelProps {
  className?: string;
  keepMounted?: boolean;
  hiddenUntilFound?: boolean;
  children: ReactNode;
}

export function DisclosurePanel({
  className,
  keepMounted,
  hiddenUntilFound,
  children,
}: DisclosurePanelProps) {
  return (
    <CollapsiblePanel
      className={className}
      keepMounted={keepMounted}
      hiddenUntilFound={hiddenUntilFound}
    >
      {children}
    </CollapsiblePanel>
  );
}
