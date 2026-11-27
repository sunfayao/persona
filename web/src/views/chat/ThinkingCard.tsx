// While the agent is thinking right now (the screen's status line, `live.thinking`), the line at the end of the
// innermost live work group says Thinking with the unified circular spinner, shimmer label, laser sweep, and clock
// (#5155). A thought itself is a row (ThinkingRow.tsx).
import { useState } from "react";
import { useStore } from "../../lib/store";
import { workDuration } from "../../lib/work-groups";
import { Shimmer, Spinner, useNow } from "../../ui/atoms";
import { t } from "../../i18n";

/** The live line: unified circular spinner, shimmering Thinking label, live laser sweep, and elapsed clock. */
export function LiveThinking() {
  const [since] = useState(Date.now);
  const now = useNow(500, useStore((s) => s.conn === "online"));
  return (
    <div
      className="cmd-laser-row flex w-full min-w-0 items-center gap-2 rounded-[10px] px-3.5 py-1.5 select-none [@media(pointer:coarse)]:min-h-10"
      style={{ animation: "fade-in 180ms var(--ease-out-strong) both" }}
      data-thinking="live"
      aria-live="polite"
    >
      <span className="inline-flex h-[18px] w-4 shrink-0 items-center justify-center">
        <Spinner size={13} className="text-accent" />
      </span>
      <Shimmer className="translate-y-[0.5px] text-[length:var(--chat-secondary)] font-medium">{t("chat-thinkingcard.thinking")}</Shimmer>
      <span className="flex-1" />
      <span className="font-mono text-[12px] text-tool-ink tabular-nums">{workDuration(Math.max(0, now - since))}</span>
    </div>
  );
}
