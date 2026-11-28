// Lazy-loaded Session Insights popover opened from ContextMeter (#12541, #13054).
// Shows context window usage with stacked ContextBreakdown bar (exact/~estimated indicator, auto-compact marker,
// top consumer hint), cumulative session token & cache summary, per-model breakdown, performance metrics
// (turn duration, tok/s, TTFT), and one-click /compact.
import { useEffect, useMemo, useState, type RefObject } from "react";
import { t } from "../../i18n";
import { api, errorText } from "../../lib/api";
import { COMPACT_FEATURE, compactBody, compactPath, formatTokens } from "../../lib/compaction";
import { contextHint, contextTier, contextTooltip, type ContextTier } from "../../lib/context-usage";
import {
  buildSessionInsights,
  compactActionSupport,
  formatHitRate,
  formatLatencyMs,
  type ContextBreakdownKey,
  type SessionContextBreakdown,
} from "../../lib/session-insights";
import { hubHas, useStore } from "../../lib/store";
import { formatRate, type RateHistory } from "../../lib/token-rate";
import { compactTokens, USAGE_COLOR } from "../../lib/tokens";
import type { ContextBreakdown, Item, Session } from "../../lib/types";
import { workDuration } from "../../lib/work-groups";
import { cx, EASE, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { UiPopover, UiPopoverClose, UiPopoverContent, UiPopoverTitle } from "../../ui/UiPopover";
import { createTooltipController, TooltipRoot, TooltipTrigger, UiTooltipContent } from "../../ui/UiTooltip";

const COMPACT_REQUESTED = "persona:compact-requested";
const EMPTY_ITEMS: readonly Item[] = [];
const TIER_BAR: Record<ContextTier, string> = { ok: "bg-accent", warn: "bg-orange", danger: "bg-orange" };
const TIER_TEXT: Record<ContextTier, string> = { ok: "text-ink", warn: "text-orange", danger: "text-orange" };

/** Color-blind-validated categorical tokens from styles.css (--usage-*), paired for light & dark modes. */
const BREAKDOWN_COLOR: Record<ContextBreakdownKey, string> = {
  system_and_rules: USAGE_COLOR.cache_write,
  tools_and_mcp: USAGE_COLOR.in,
  conversation: USAGE_COLOR.cache_read,
  tool_outputs: USAGE_COLOR.out,
};

const BREAKDOWN_LABEL_KEY: Record<ContextBreakdownKey, string> = {
  system_and_rules: "chat-sessioninsights.breakdown_system_and_rules",
  tools_and_mcp: "chat-sessioninsights.breakdown_tools_and_mcp",
  conversation: "chat-sessioninsights.breakdown_conversation",
  tool_outputs: "chat-sessioninsights.breakdown_tool_outputs",
};

const formatShare = (pct: number): string => `${Math.round(Math.min(100, Math.max(0, pct)))}%`;

export interface SessionInsightsPopoverProps {
  anchor: RefObject<HTMLElement | null>;
  session: Session;
  items?: readonly Item[];
  assistant?: boolean;
  onCompact?: () => boolean | void | Promise<boolean | void | unknown>;
  compactBlocked?: boolean;
  onClose: () => void;
  /** Optional overrides for tests. */
  rateHistory?: RateHistory | null;
  breakdown?: ContextBreakdown | null;
  ttftMs?: number | null;
}

async function requestAssistantCompaction(session: Session): Promise<boolean> {
  try {
    await api(compactPath(session.session_id), { method: "POST", json: compactBody(session.machine_id) });
    window.dispatchEvent(new CustomEvent(COMPACT_REQUESTED, { detail: { mid: session.machine_id, sid: session.session_id } }));
    return true;
  } catch (e) {
    toast(t("assistant-assistantchat.compact_start_failed", { error: errorText(e) }), "error");
    return false;
  }
}

export function StackedBreakdownBar({ breakdown }: { breakdown: SessionContextBreakdown }) {
  const [tooltip] = useState(() => createTooltipController<string>());
  const markerShare = breakdown.autoCompactShare !== null ? Math.min(99, Math.max(1, breakdown.autoCompactShare)) : null;
  const markerLabel =
    breakdown.autoCompactThreshold !== null && breakdown.autoCompactShare !== null
      ? t("chat-sessioninsights.breakdown_auto_compact_at", {
          tokens: compactTokens(breakdown.autoCompactThreshold),
          percent: Math.round(breakdown.autoCompactShare),
        })
      : undefined;

  return (
    <div className="relative h-1.5 w-full" data-insights-stacked-bar>
      <div className="flex h-full w-full overflow-hidden rounded-full bg-line">
        {breakdown.segments.map((seg) => {
          if (seg.tokens <= 0 || seg.windowShare <= 0) return null;
          const label = t(BREAKDOWN_LABEL_KEY[seg.key]);
          const tokText = breakdown.source === "estimated"
            ? t("chat-sessioninsights.breakdown_estimated_tokens", { tokens: compactTokens(seg.tokens) })
            : compactTokens(seg.tokens);
          const tip = `${label}: ${tokText} (${formatShare(seg.windowShare)})`;
          return (
            <TooltipTrigger
              key={seg.key}
              controller={tooltip}
              payload={tip}
              render={
                <div
                  data-breakdown-segment={seg.key}
                  aria-label={tip}
                  className="h-full shrink-0 first:rounded-l-full last:rounded-r-full motion-safe:transition-[width] motion-safe:duration-300"
                  style={{ width: `${seg.windowShare}%`, backgroundColor: BREAKDOWN_COLOR[seg.key], transitionTimingFunction: EASE }}
                />
              }
            />
          );
        })}
      </div>
      {markerShare !== null && markerLabel && (
        <TooltipTrigger
          controller={tooltip}
          payload={markerLabel}
          render={
            <span
              role="img"
              aria-label={markerLabel}
              data-breakdown-compact-marker
              className="pointer-events-auto absolute -top-1 -bottom-1 flex w-2.5 -translate-x-1/2 cursor-default items-stretch justify-center"
              style={{ left: `${markerShare}%` }}
            >
              <span aria-hidden className="w-px rounded-full bg-ink/80" />
            </span>
          }
        />
      )}
      <TooltipRoot controller={tooltip} delay={120}>
        <UiTooltipContent side="top" sideOffset={8}>
          {(state) => (typeof state.payload === "string" ? state.payload : null)}
        </UiTooltipContent>
      </TooltipRoot>
    </div>
  );
}

export function BreakdownCard({ breakdown }: { breakdown: SessionContextBreakdown }) {
  const estimated = breakdown.source === "estimated";
  const formatCatTokens = (n: number) =>
    estimated ? t("chat-sessioninsights.breakdown_estimated_tokens", { tokens: compactTokens(n) }) : compactTokens(n);

  return (
    <div className="border-t border-line/55 pt-2.5" data-insights-breakdown>
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="text-submeta font-medium text-ink-3">{t("chat-sessioninsights.breakdown_section")}</span>
        <span
          data-breakdown-source={breakdown.source}
          className={cx(
            "shrink-0 text-submeta font-medium",
            estimated ? "text-ink-3" : "text-green",
          )}
        >
          {t(estimated ? "chat-sessioninsights.breakdown_source_estimated" : "chat-sessioninsights.breakdown_source_exact")}
        </span>
      </div>

      <dl className="flex flex-col gap-1.5 text-label tabular-nums">
        {breakdown.segments.map((seg) => {
          const isTop = breakdown.topConsumer?.key === seg.key;
          return (
            <div key={seg.key} className="flex items-center justify-between gap-2" data-breakdown-row={seg.key}>
              <dt className="flex min-w-0 items-center gap-2 text-ink-3">
                <span
                  aria-hidden
                  className="size-1.5 shrink-0 rounded-full"
                  style={{ backgroundColor: BREAKDOWN_COLOR[seg.key] }}
                />
                <span className={cx("truncate", isTop ? "font-medium text-ink" : "text-ink-2")}>{t(BREAKDOWN_LABEL_KEY[seg.key])}</span>
              </dt>
              <dd className="flex shrink-0 items-baseline gap-1.5 text-ink">
                <span className="font-medium">{formatCatTokens(seg.tokens)}</span>
                {breakdown.totalTokens > 0 && (
                  <span className="inline-block w-8 text-right text-submeta text-ink-3">{formatShare(seg.windowShare)}</span>
                )}
              </dd>
            </div>
          );
        })}

        {breakdown.freeTokens !== null && (
          <div className="flex items-center justify-between gap-2" data-breakdown-row="free">
            <dt className="flex min-w-0 items-center gap-2 text-ink-3">
              <span aria-hidden className="size-1.5 shrink-0 rounded-full border border-line-strong bg-surface" />
              <span className="truncate">{t("chat-sessioninsights.breakdown_free")}</span>
            </dt>
            <dd className="flex shrink-0 items-baseline gap-1.5 text-ink-2">
              <span className="font-medium">{compactTokens(breakdown.freeTokens)}</span>
              {breakdown.freeShare !== null && (
                <span className="inline-block w-8 text-right text-submeta text-ink-3">{formatShare(breakdown.freeShare)}</span>
              )}
            </dd>
          </div>
        )}

        {breakdown.autoCompactThreshold !== null && (
          <div className="mt-1 flex items-center justify-between gap-2 border-t border-line/55 pt-2 text-submeta" data-breakdown-auto-compact>
            <dt className="flex min-w-0 items-center gap-2 text-ink-3">
              <span aria-hidden className="inline-block h-2.5 w-px shrink-0 rounded-full bg-ink/70" />
              <span className="truncate">{t("chat-sessioninsights.breakdown_auto_compact")}</span>
            </dt>
            <dd className="flex shrink-0 items-baseline gap-1.5 font-medium text-ink-2">
              <span>{compactTokens(breakdown.autoCompactThreshold)}</span>
              {breakdown.autoCompactShare !== null && (
                <span className="inline-block w-8 text-right font-normal text-ink-3">{formatShare(breakdown.autoCompactShare)}</span>
              )}
            </dd>
          </div>
        )}
      </dl>

      {breakdown.topConsumer && (
        <div
          data-breakdown-top-consumer={breakdown.topConsumer.key}
          className="mt-2 flex items-center gap-2 border-t border-line/55 pt-2 text-submeta text-ink-3"
        >
          <span
            aria-hidden
            className="size-1.5 shrink-0 rounded-full"
            style={{ backgroundColor: BREAKDOWN_COLOR[breakdown.topConsumer.key] }}
          />
          <span className="min-w-0 truncate">
            {t("chat-sessioninsights.breakdown_top_consumer", {
              category: t(BREAKDOWN_LABEL_KEY[breakdown.topConsumer.key]),
              percent: Math.round(breakdown.topConsumer.usedShare),
            })}
          </span>
        </div>
      )}
    </div>
  );
}

export function SessionInsightsPopover({
  anchor,
  session,
  items = EMPTY_ITEMS,
  assistant,
  onCompact,
  compactBlocked,
  onClose,
  rateHistory,
  breakdown,
  ttftMs,
}: SessionInsightsPopoverProps) {
  const [compactingNow, setCompactingNow] = useState(false);

  const hubSupportsAssistantCompact = useStore((s) => hubHas(s.features, COMPACT_FEATURE));
  const insights = useMemo(
    () => buildSessionInsights(session, items, {
      ...(rateHistory !== undefined ? { rateHistory } : {}),
      ...(breakdown !== undefined ? { breakdown } : {}),
      ...(ttftMs !== undefined ? { ttftMs } : {}),
    }),
    [session, items, rateHistory, breakdown, ttftMs],
  );
  const compact = useMemo(
    () => compactActionSupport(session, {
      assistant,
      hubSupportsAssistantCompact,
      hasSendCompact: Boolean(onCompact),
      blocked: compactBlocked,
    }),
    [session, assistant, hubSupportsAssistantCompact, onCompact, compactBlocked],
  );

  // Real keyboard Escape targets the focused element and is handled by UiPopover on document.
  // Keep a narrow listener for synthetic window-targeted Escape events (e.g. window.dispatchEvent).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented && !(e.target instanceof Node)) onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const triggerCompact = async () => {
    if (!compact.supported || compact.running || compactingNow) return;
    setCompactingNow(true);
    try {
      let ok = false;
      if (onCompact) {
        const res = await onCompact();
        ok = res !== false;
        if (!ok) toast(t("chat-sessioninsights.compact_failed"), "error");
      } else if (compact.mode === "assistant") {
        ok = await requestAssistantCompaction(session);
      }
      if (ok) onClose();
    } catch (e) {
      toast(t("assistant-assistantchat.compact_start_failed", { error: errorText(e) }), "error");
    } finally {
      setCompactingNow(false);
    }
  };

  const { context, tokens, performance, breakdown: normalizedBreakdown } = insights;
  const hitText = tokens ? formatHitRate(tokens.cacheHitRate) : null;
  const compactBusy = compact.running || compactingNow;
  const hasContextSection = Boolean(context || normalizedBreakdown);

  return (
    <UiPopover
      open
      presentation={typeof matchMedia === "function" ? "auto" : "popover"}
      onOpenChange={(next, details) => {
        if (next) return;
        const target = details.event?.target;
        if (
          details.reason === "outside-press" &&
          target instanceof Node &&
          anchor.current?.contains(target)
        ) {
          return details.cancel();
        }
        onClose();
      }}
    >
      <UiPopoverContent
        anchor={anchor.current}
        side="top"
        align="end"
        sideOffset={8}
        collisionPadding={12}
        data-session-insights=""
        className="w-[min(340px,calc(100vw-24px))] max-h-[min(70vh,520px)] overflow-y-auto p-4 text-label text-ink"
      >
        <div className="flex items-center justify-between gap-2">
          <UiPopoverTitle className="text-meta font-semibold tracking-[-0.01em] text-ink">
            {t("chat-sessioninsights.title")}
          </UiPopoverTitle>
          <UiPopoverClose
            type="button"
            aria-label={t("chat-sessioninsights.close")}
            className="inline-flex size-6 items-center justify-center rounded-control text-ink-3 transition-colors duration-150 hover:bg-hover hover:text-ink"
            data-insights-close=""
          >
            <Icon name="x" size={13} stroke={2} />
          </UiPopoverClose>
        </div>

        <div className="mt-3 flex flex-col gap-3">
          {hasContextSection && (
            <section className="flex flex-col gap-2" data-insights-context>
              {context && (
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium text-ink-2">{t("chat-sessioninsights.context_section")}</span>
                  <span className="tabular-nums text-ink-3">
                    {t("context.tokens", { used: formatTokens(context.tokens), window: formatTokens(context.window) })}
                  </span>
                </div>
              )}
              {normalizedBreakdown && normalizedBreakdown.totalTokens > 0 ? (
                <StackedBreakdownBar breakdown={normalizedBreakdown} />
              ) : context ? (
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-hover-2">
                  <div
                    className={cx("h-full rounded-full", TIER_BAR[contextTier(context)])}
                    style={{ width: `${context.used}%` }}
                  />
                </div>
              ) : null}
              {context && (
                <div className="flex flex-col gap-1">
                  <div className={cx("font-medium tabular-nums", TIER_TEXT[contextTier(context)])}>
                    {contextTooltip(context)}
                  </div>
                  <p className="leading-relaxed text-ink-3">{contextHint(context)}</p>
                </div>
              )}
              {normalizedBreakdown && <BreakdownCard breakdown={normalizedBreakdown} />}
            </section>
          )}

          {tokens && (
            <section className={cx("flex flex-col gap-2.5", hasContextSection && "border-t border-line/55 pt-3")} data-insights-tokens>
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium text-ink-2">{t("chat-sessioninsights.tokens_section")}</span>
                <span className="tabular-nums text-ink-3">
                  {t("chat-sessioninsights.total_tokens", { tokens: compactTokens(tokens.total) })}
                </span>
              </div>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-2.5 tabular-nums">
                <div className="flex flex-col gap-0.5">
                  <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.input")}</dt>
                  <dd className="text-meta font-semibold text-ink" data-insights-input>{compactTokens(tokens.input)}</dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.output")}</dt>
                  <dd className="text-meta font-semibold text-ink" data-insights-output>{compactTokens(tokens.output)}</dd>
                </div>
                {tokens.hasCache && (
                  <>
                    <div className="flex flex-col gap-0.5">
                      <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.cache_read")}</dt>
                      <dd className="text-meta font-semibold text-ink" data-insights-cache-read>{compactTokens(tokens.cacheRead)}</dd>
                    </div>
                    <div className="flex flex-col gap-0.5">
                      <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.cache_write")}</dt>
                      <dd className="text-meta font-semibold text-ink" data-insights-cache-write>{compactTokens(tokens.cacheWrite)}</dd>
                    </div>
                  </>
                )}
              </dl>
              {hitText && (
                <div className="flex items-center justify-between gap-2 border-t border-line/45 pt-2 tabular-nums" data-insights-cache-hit>
                  <span className="text-ink-3">{t("chat-sessioninsights.cache_hit_rate")}</span>
                  <span className="font-medium text-ink">{hitText}</span>
                </div>
              )}
              {tokens.models.length > 1 ? (
                <div className="flex flex-col gap-1.5 border-t border-line/45 pt-2" data-insights-models>
                  <div className="text-submeta text-ink-3">{t("chat-sessioninsights.by_model")}</div>
                  <ul className="divide-y divide-line/45 tabular-nums">
                    {tokens.models.map((m) => {
                      const modelHit = formatHitRate(m.cacheHitRate);
                      const name = m.label || t("chat-sessioninsights.unknown_model");
                      const label = m.fast ? `${name} · ${t("chat-sessiondialogs.speed_fast")}` : name;
                      return (
                        <li key={`${m.model || "__unknown__"}\n${m.fast ? 1 : 0}`} className="flex items-center justify-between gap-2 py-1.5 first:pt-0 last:pb-0">
                          <span className="min-w-0 truncate font-medium text-ink">{label}</span>
                          <span className="shrink-0 text-ink-3">
                            {compactTokens(m.total)}
                            {modelHit ? ` · ${modelHit}` : ""}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                </div>
              ) : tokens.models.length === 1 && (tokens.models[0].label || tokens.models[0].fast) ? (
                <div className="flex items-center justify-between gap-2 border-t border-line/45 pt-2 text-ink-3" data-insights-model-single>
                  <span>{t("chat-sessioninsights.model_single")}</span>
                  <span className="truncate font-medium text-ink">
                    {tokens.models[0].fast
                      ? `${tokens.models[0].label || t("chat-sessioninsights.unknown_model")} · ${t("chat-sessiondialogs.speed_fast")}`
                      : tokens.models[0].label}
                  </span>
                </div>
              ) : null}
            </section>
          )}

          {performance && (
            <section className={cx("flex flex-col gap-2.5", (hasContextSection || tokens) && "border-t border-line/55 pt-3")} data-insights-performance>
              <span className="font-medium text-ink-2">{t("chat-sessioninsights.performance_section")}</span>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-2.5 tabular-nums">
                {performance.latestTurnDurationMs !== null && (
                  <div className="flex flex-col gap-0.5">
                    <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.latest_turn")}</dt>
                    <dd className="text-meta font-semibold text-ink" data-insights-duration>{workDuration(performance.latestTurnDurationMs)}</dd>
                  </div>
                )}
                {performance.ttftMs != null && performance.ttftMs >= 0 && (
                  <div className="flex flex-col gap-0.5">
                    <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.ttft")}</dt>
                    <dd className="text-meta font-semibold text-ink" data-insights-ttft>
                      {formatLatencyMs(performance.ttftMs)}
                    </dd>
                  </div>
                )}
                {performance.latestTokPerSec !== null && (
                  <div className="flex flex-col gap-0.5">
                    <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.latest_speed")}</dt>
                    <dd className="text-meta font-semibold text-ink" data-insights-speed-latest>
                      {t("chat-sessioninsights.tok_per_sec", { rate: formatRate(performance.latestTokPerSec) })}
                    </dd>
                  </div>
                )}
                {performance.avgTokPerSec !== null && (
                  <div className="flex flex-col gap-0.5">
                    <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.average_speed")}</dt>
                    <dd className="text-meta font-semibold text-ink" data-insights-speed-avg>
                      {t("chat-sessioninsights.tok_per_sec", { rate: formatRate(performance.avgTokPerSec) })}
                    </dd>
                  </div>
                )}
                {performance.peakTokPerSec !== null && (
                  <div className="flex flex-col gap-0.5">
                    <dt className="text-submeta text-ink-3">{t("chat-sessioninsights.peak_speed")}</dt>
                    <dd className="text-meta font-semibold text-ink" data-insights-speed-peak>
                      {t("chat-sessioninsights.tok_per_sec", { rate: formatRate(performance.peakTokPerSec) })}
                    </dd>
                  </div>
                )}
              </dl>
            </section>
          )}

          {compact.supported && (
            <div className={cx((hasContextSection || tokens || performance) && "border-t border-line/55 pt-3")}>
              <button
                type="button"
                onClick={() => void triggerCompact()}
                disabled={compactBusy}
                data-insights-compact
                className="flex w-full items-center justify-between gap-2 rounded-[9px] bg-hover/60 px-3 py-2 text-label font-medium text-ink transition-[background-color,transform] duration-150 hover:bg-hover active:scale-[0.99] disabled:opacity-50"
              >
                <span className="flex items-center gap-1.5">
                  <Icon name="layers" size={14} className="shrink-0 text-ink-2" />
                  <span>{compactBusy ? t("chat-sessioninsights.compacting") : t("chat-sessioninsights.compact_action")}</span>
                </span>
                {compact.mode === "agent" && <span className="font-mono text-ink-3">/compact</span>}
              </button>
            </div>
          )}
        </div>
      </UiPopoverContent>
    </UiPopover>
  );
}
