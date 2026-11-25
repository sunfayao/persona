// The system's reduced motion: replies fade in word by word unless it asks for less.
import { useSyncExternalStore } from "react";

const REDUCE = "(prefers-reduced-motion: reduce)";

export const prefersReducedMotion = () => typeof matchMedia === "function" && matchMedia(REDUCE).matches;

/** How a scripted scroll moves: animated, or straight there under reduced motion (the CSS rule cannot reach scrollTo). */
export const scrollBehavior = (): ScrollBehavior => (prefersReducedMotion() ? "auto" : "smooth");

function subscribe(l: () => void) {
  const q = typeof matchMedia === "function" ? matchMedia(REDUCE) : null;
  q?.addEventListener("change", l);
  return () => q?.removeEventListener("change", l);
}

/** Whether replies fade in now: the system doesn't ask for reduced motion. */
export function replyMotion(): boolean {
  return !prefersReducedMotion();
}

export function useReplyMotion(): boolean {
  return useSyncExternalStore(subscribe, replyMotion, () => false);
}
