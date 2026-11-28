// The account windows and error details behind a click on the sidebar's usage bars (#10082): these are not needed
// for the cold first screen, so the overlay and its long strings load on intent.
import { useEffect, useId, useState } from "react";
import { t as tr } from "../i18n";
import { BUSY_MOTION, spinStyle } from "../lib/busy-motion";
import { sendDraft } from "../lib/drafts";
import { navigate } from "../lib/router";
import { getState, useStore } from "../lib/store";
import { balanceEmpty, balanceText } from "../lib/usage-balance";
import {
  accountPace,
  fullest,
  groupByDevice,
  isBalanceAccount,
  isCritical,
  isWarn,
  planNote,
  providerTimeline,
  providerTitle,
  refreshUsageWindows,
  remainingText,
  resetLabel,
  resetPinnedUsageAccounts,
  sampleAge,
  sortedWindows,
  sourceNames,
  TIMELINE_TICKS_HOURS,
  timelinePosition,
  togglePinnedUsageAccount,
  topAccounts,
  usePinnedUsageAccounts,
  windowKey,
  type DeviceGroup,
  type PaceState,
  type ProviderGroup,
  type UnconnectedProvider,
  type UsageAccount,
  type UsageMachineHint,
  type UsageWindow,
} from "../lib/usage-windows";
import { AgentMark } from "../ui/AgentMark";
import { cx } from "../ui/atoms";
import { Disclosure, DisclosurePanel, DisclosureTrigger } from "../ui/Disclosure";
import { Icon } from "../ui/icons";
import { ScrollFade } from "../ui/ScrollFade";
import { createTooltipController, TooltipRoot, TooltipTrigger, UiTooltipContent } from "../ui/UiTooltip";
import { FloatingSegment, UnderlineTabs, type UnderlineTabItem } from "../ui/UnderlineTabs";
import { Meter } from "./UsageBars";

type UsageTooltipController = ReturnType<typeof createTooltipController<string>>;

const PACE_TONE: Record<PaceState, string> = {
  over: "text-orange",
  under: "text-green",
  on: "text-ink-3",
};
const paceLabel = (s: PaceState) => (s === "over" ? tr("usage.pace_over") : s === "under" ? tr("usage.pace_under") : tr("usage.pace_on"));
const shown = (p: number) => `${Math.round(p)}%`;
const valueTone = (p: number) => (isCritical(p) ? "text-red" : isWarn(p) ? "text-orange" : "text-ink");

function deviceTitle(device: DeviceGroup): string {
  return device.kind === "global" ? tr("usage.device_cloud") : device.title;
}

/** Strip parenthesized `(host)` suffixes that already match the surrounding device card. */
function stripDeviceHostHint(text: string, device: DeviceGroup): string {
  if (!device.aliases.length) return text.trim();
  const aliasSet = new Set(device.aliases);
  return text
    .replace(/\s*\(([^()]+)\)/g, (full, inner: string) => (aliasSet.has(inner.trim().toLowerCase()) ? "" : full))
    .trim();
}

/** Sub-account label to show beside or inside a provider section when `a.name` adds information beyond the provider and device. */
function distinctAccountLabel(rawName: string, providerName: string, providerId: string, device: DeviceGroup): string {
  const cleaned = stripDeviceHostHint(rawName, device);
  const lower = cleaned.toLowerCase();
  if (!cleaned || lower === providerName.toLowerCase() || lower === providerId.toLowerCase()) return "";
  return cleaned;
}

/** Sources that add context beyond the device card header and provider heading. */
function filterRedundantSources(from: readonly string[], accountName: string, providerId: string, providerName: string, device: DeviceGroup): string[] {
  const aliasSet = new Set(device.aliases);
  const localLabel = tr("usage.source_local").toLowerCase();
  const out: string[] = [];
  for (const src of from) {
    const raw = src.trim();
    if (!raw) continue;
    const rawLower = raw.toLowerCase();
    if (aliasSet.has(rawLower)) continue;
    if (device.kind !== "global" && rawLower === localLabel) continue;
    const stripped = stripDeviceHostHint(raw, device);
    const lower = stripped.toLowerCase();
    if (!stripped || aliasSet.has(lower)) continue;
    if (
      stripped !== raw &&
      (lower === providerId.toLowerCase() || lower === providerName.toLowerCase() || lower === stripDeviceHostHint(accountName, device).toLowerCase())
    ) {
      continue;
    }
    if (!out.includes(stripped)) out.push(stripped);
  }
  return out;
}

function problemText(a: UsageAccount): string {
  switch (a.problem) {
    case "rejected":
      return tr("usage.problem_rejected");
    case "unreachable":
      return tr("usage.problem_unreachable");
    case "failed":
      return tr("usage.problem_failed");
    case "error":
      return tr("usage.problem_error");
    default:
      return "";
  }
}

/** Why a balance could not be read, or why the amount shown is the last one the manager had. */
function balanceProblemText(a: UsageAccount): string {
  if (!isBalanceAccount(a)) return "";
  switch (a.problem) {
    case "rejected":
      return a.balance ? tr("usage.balance_rejected_kept") : tr("usage.balance_rejected");
    case "unreachable":
      return a.balance ? tr("usage.balance_unreachable_kept") : tr("usage.balance_unreachable");
    case "failed":
      return a.balance ? tr("usage.balance_failed_kept") : tr("usage.balance_failed");
    default:
      return "";
  }
}

/** Why a provider has no bars, in plain words (#12411). */
function connectHint(p: UnconnectedProvider): string {
  if (p.agent === "claude" && p.id === "claude") return tr("usage.connect_hint_claude");
  if (p.agent === "agy" || p.agent === "antigravity" || (p.agent === "codex" && p.id === "codex")) return tr("usage.connect_hint_pending", { name: p.title });
  return tr("usage.connect_hint_other");
}

function connectPrompt(p: UnconnectedProvider): string {
  if (p.agent === "claude" && p.id === "claude") return tr("usage.connect_prompt_claude", { id: p.id });
  if (p.agent === "agy" || p.agent === "antigravity") return tr("usage.connect_prompt_agy", { id: p.id });
  return tr("usage.connect_prompt_other", { name: p.title, id: p.id });
}

/** Every live account organized device-first, window by window, plus active providers without usage data yet. It opens
 * compact (one row per provider); the detailed view lists every window. */
export function UsagePopover({
  id,
  accounts,
  unconnected = [],
  machines,
  now: fixed,
  onClose,
  onRefresh = refreshUsageWindows,
  initialView = "compact",
}: {
  id?: string;
  accounts: UsageAccount[];
  unconnected?: readonly UnconnectedProvider[];
  machines?: readonly UsageMachineHint[];
  now?: number;
  onClose?: () => void;
  onRefresh?: () => Promise<boolean>;
  initialView?: "detailed" | "compact";
}) {
  const storeMachines = useStore((s) => s.machines);
  const knownMachines = machines ?? storeMachines;
  const pinnedIds = usePinnedUsageAccounts();
  const pinnedSet = new Set(pinnedIds);
  const livePinnedCount = accounts.reduce((n, a) => n + (pinnedSet.has(a.id) ? 1 : 0), 0);
  const [view, setView] = useState<"detailed" | "compact">(initialView);
  const [deviceFilter, setDeviceFilter] = useState<string>("all");
  const [compactSelection, setCompactSelection] = useState<string>();
  const [refresh, setRefresh] = useState<"idle" | "loading" | "done" | "failed">("idle");
  const [tooltip] = useState(() => createTooltipController<string>());
  const refreshing = refresh === "loading";
  const detailsId = useId();

  const deviceGroups = groupByDevice(accounts, knownMachines);
  const activeDevice = deviceFilter !== "all" && deviceGroups.some((d) => d.key === deviceFilter) ? deviceFilter : "all";
  const shownDevices = activeDevice === "all" ? deviceGroups : deviceGroups.filter((d) => d.key === activeDevice);

  const selectedCompact =
    view === "compact" && compactSelection
      ? deviceGroups
          .flatMap((d) => d.providers.map((g) => ({ key: `${d.key}\0${g.provider}`, device: d, group: g })))
          .find((item) => item.key === compactSelection)
      : undefined;

  const [tick, setTick] = useState(() => Date.now());
  useEffect(() => {
    if (fixed !== undefined) return;
    const t = setInterval(() => setTick(Date.now()), 1000);
    return () => clearInterval(t);
  }, [fixed]);
  const now = fixed ?? tick;

  const refreshAccounts = async () => {
    setRefresh("loading");
    setRefresh((await onRefresh()) ? "done" : "failed");
  };

  const askAssistant = (p: UnconnectedProvider) => {
    sendDraft("persona-new", { type: "replace", text: connectPrompt(p) });
    onClose?.();
    const onStarter = typeof location !== "undefined" && (location.pathname === "/persona/new" || (location.pathname === "/persona" && !getState().assistant?.main));
    if (!onStarter) navigate("/persona/new");
  };

  const deviceTabItems: UnderlineTabItem<string>[] =
    view === "detailed" && deviceGroups.length >= 2
      ? [
          {
            key: "all",
            label: tr("usage.all_devices"),
            count: accounts.length,
            dataAttrs: { "data-usage-filter": "all" },
          },
          ...deviceGroups.map((dev): UnderlineTabItem<string> => ({
            key: dev.key,
            label: <span className="block max-w-[9rem] truncate">{deviceTitle(dev)}</span>,
            count: dev.accounts.length,
            lead:
              dev.online !== undefined ? (
                <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", dev.online ? "bg-green" : "bg-ink-3/50")} />
              ) : (
                <Icon name={dev.kind === "global" ? "cloud" : "machine"} size={12} className="shrink-0 text-ink-3" />
              ),
            dataAttrs: { "data-usage-filter": dev.key },
          })),
        ]
      : [];

  return (
    <div
      id={id}
      role="dialog"
      aria-label={tr("usage.title")}
      data-usage-popover
      className={cx(
        // the fade over the list's end is shorter than the chat's: its end padding (pb-fade-end) follows
        "absolute bottom-full left-2 right-2 z-20 mb-2 flex max-h-[75dvh] flex-col overflow-hidden rounded-2xl bg-surface shadow-overlay [--spacing-fade:calc(var(--spacing)*6)] lg:right-auto",
        selectedCompact ? "lg:w-184" : "lg:w-[26rem]",
      )}
      style={{ animation: "pop-in 180ms var(--ease-out-strong) both" }}
    >
      <header className="shrink-0 border-b border-line/60 px-4 py-3">
        <div className="flex items-center gap-2">
          <Icon name="gauge" size={14} className="shrink-0 text-ink-3" />
          <h2 className="text-meta font-semibold tracking-[-0.01em] text-ink">{tr("usage.short_title")}</h2>
          <span className="min-w-0 flex-1 truncate text-submeta text-ink-3">{tr("usage.used")}</span>
          {deviceGroups.length > 0 && (
            <FloatingSegment
              ariaLabel={tr("usage.display")}
              value={view}
              onChange={setView}
              size="xs"
              items={[
                { key: "detailed", label: tr("usage.detailed") },
                { key: "compact", label: tr("usage.compact") },
              ]}
            />
          )}
          <TooltipTrigger
            controller={tooltip}
            payload={tr("usage.refresh")}
            type="button"
            aria-label={tr("usage.refresh")}
            disabled={refreshing}
            aria-busy={refreshing || undefined}
            onClick={refreshAccounts}
            className="inline-flex size-7 shrink-0 items-center justify-center rounded-control text-ink-3 transition-[background-color,color,transform] duration-150 hover:bg-hover hover:text-ink active:scale-[0.94] disabled:opacity-60"
          >
            <Icon name="retry" size={14} className={refreshing ? BUSY_MOTION : undefined} style={refreshing ? spinStyle(900) : undefined} />
          </TooltipTrigger>
        </div>
        <div role="status" className={refresh === "failed" ? "pt-1.5 text-submeta text-orange" : "sr-only"}>
          {refreshing ? tr("usage.refreshing") : refresh === "done" ? tr("usage.refreshed") : refresh === "failed" ? tr("usage.refresh_failed") : ""}
        </div>
      </header>

      {deviceTabItems.length > 0 && (
        <div data-usage-device-filter className="shrink-0">
          <UnderlineTabs
            items={deviceTabItems}
            value={activeDevice}
            onChange={(next) => setDeviceFilter(next !== "all" && next === activeDevice ? "all" : next)}
            role="group"
            ariaLabel={tr("usage.devices")}
            size="sm"
            className="px-4"
          />
        </div>
      )}

      {pinnedIds.length > 0 && accounts.length > 0 && (
        <div data-usage-pinned-bar className="flex shrink-0 items-center justify-between gap-2 border-b border-line/60 px-4 py-2 text-submeta">
          <span className="min-w-0 truncate text-ink-3">
            {livePinnedCount > 0 ? tr("usage.pinned_summary", { count: livePinnedCount }) : tr("usage.pinned_fallback")}
          </span>
          <button
            type="button"
            data-usage-reset-pins
            onClick={() => resetPinnedUsageAccounts()}
            className="shrink-0 font-medium whitespace-nowrap text-accent-ink hover:underline"
          >
            {tr("usage.reset_pinned")}
          </button>
        </div>
      )}

      <div className={cx("min-h-0 flex-1 overflow-y-auto overscroll-contain scroll-pb-fade-end", selectedCompact && "lg:grid lg:grid-cols-2 lg:items-start lg:divide-x lg:divide-line/60")}>
        <div className="flex min-w-0 flex-col pb-fade-end">
          {view === "detailed" ? (
            shownDevices.length > 0 && (
              <div className="divide-y divide-line/60">
                {shownDevices.map((dev) => (
                  <DeviceSection key={dev.key} device={dev} now={now} pinned={pinnedSet} tooltip={tooltip} />
                ))}
              </div>
            )
          ) : (
            <div className="flex flex-col gap-2 px-2 py-2.5">
              {deviceGroups.map((dev) => {
                const title = deviceTitle(dev);
                return (
                  <div key={dev.key} className="flex flex-col gap-0.5">
                    {deviceGroups.length > 1 && (
                      <div className="flex items-center gap-1.5 px-2 pt-1 pb-0.5 text-submeta font-medium text-ink-3">
                        <Icon name={dev.kind === "global" ? "cloud" : "machine"} size={12} className="shrink-0 text-ink-3" />
                        <span className="truncate">{title}</span>
                      </div>
                    )}
                    {dev.providers.map((g) => {
                      const itemKey = `${dev.key}\0${g.provider}`;
                      const isSelected = selectedCompact?.key === itemKey;
                      return (
                        <ProviderSummary
                          key={itemKey}
                          group={g}
                          selected={isSelected}
                          detailsId={detailsId}
                          now={now}
                          onClick={() => setCompactSelection(isSelected ? undefined : itemKey)}
                        />
                      );
                    })}
                  </div>
                );
              })}
            </div>
          )}

          {unconnected.length > 0 && (
            <div className={cx("divide-y divide-line/45 px-4", accounts.length > 0 && "border-t border-line/60")}>
              {unconnected.map((p) => (
                <section key={`provider:${p.id}`} aria-label={p.title} className="flex flex-col gap-1 py-3 text-label">
                  <div className="flex items-center gap-2">
                    <AgentMark agent={p.agent} label={p.title} decorative size={14} />
                    <h3 className="min-w-0 flex-1 truncate font-medium text-ink">{p.title}</h3>
                    <button
                      type="button"
                      data-usage-connect={p.id}
                      onClick={() => askAssistant(p)}
                      className="shrink-0 text-submeta font-medium whitespace-nowrap text-accent-ink hover:underline"
                    >
                      {tr("usage.connect_action")}
                    </button>
                  </div>
                  <div className="text-submeta leading-snug text-ink-3">{connectHint(p)}</div>
                </section>
              ))}
            </div>
          )}
        </div>

        {selectedCompact && (
          <div id={detailsId} className="min-w-0 border-t border-line/60 pt-1 pb-fade-end lg:border-t-0">
            <ProviderSection group={selectedCompact.group} device={selectedCompact.device} now={now} pinned={pinnedSet} tooltip={tooltip} />
          </div>
        )}
      </div>
      <ScrollFade surface="surface" className="z-10" />
      <TooltipRoot controller={tooltip} delay={250}>
        <UiTooltipContent side="top" sideOffset={6}>
          {(state) => (typeof state.payload === "string" ? state.payload : null)}
        </UiTooltipContent>
      </TooltipRoot>
    </div>
  );
}

function DeviceSection({ device, now, pinned, tooltip }: { device: DeviceGroup; now: number; pinned: ReadonlySet<string>; tooltip: UsageTooltipController }) {
  const title = deviceTitle(device);
  const onlineLabel = device.online !== undefined ? (device.online ? tr("usage.device_online") : tr("usage.device_offline")) : "";
  return (
    <section data-usage-device={device.key} aria-label={title} className="flex flex-col">
      <header className="flex items-center justify-between gap-2 px-4 pt-3.5 pb-1.5">
        <div className="flex min-w-0 items-center gap-1.5">
          <Icon name={device.kind === "global" ? "cloud" : "machine"} size={13} className="shrink-0 text-ink-3" />
          <h3 className="min-w-0 truncate text-label font-semibold tracking-[-0.01em] text-ink">{title}</h3>
          {device.subtitle && (
            <span className="min-w-0 truncate font-mono text-submeta text-ink-3">
              {device.subtitle}
            </span>
          )}
          {device.online !== undefined && (
            <span className="inline-flex shrink-0 items-center pl-0.5" title={onlineLabel}>
              <span aria-hidden className={cx("size-1.5 rounded-full", device.online ? "bg-green" : "bg-ink-3/50")} />
              <span className="sr-only">{onlineLabel}</span>
            </span>
          )}
        </div>
        {device.sampledAt && <span className="shrink-0 text-submeta tabular-nums text-ink-3">{sampleAge(device.sampledAt, now)}</span>}
      </header>
      <div className="divide-y divide-line/45">
        {device.providers.map((g) => (
          <ProviderSection key={g.provider} group={g} device={device} now={now} pinned={pinned} tooltip={tooltip} />
        ))}
      </div>
    </section>
  );
}

function ProviderSummary({ group, selected, detailsId, now, onClick }: { group: ProviderGroup; selected: boolean; detailsId: string; now: number; onClick: () => void }) {
  const name = providerTitle(group.provider);
  const primary = topAccounts(group.accounts, 1)[0] ?? group.accounts.find((a) => !a.stale) ?? group.accounts[0];
  const nextReset = primary.windows.reduce((soonest, window) => {
    const reset = window.resets_at ? Date.parse(window.resets_at) : NaN;
    return reset > now ? Math.min(soonest, reset) : soonest;
  }, Infinity);
  const reset = Number.isFinite(nextReset) ? resetLabel(new Date(nextReset).toISOString(), now) : "";
  const hasQuota = primary.windows.length > 0 && !isBalanceAccount(primary);
  const used = fullest(primary);
  return (
    <button
      type="button"
      aria-expanded={selected}
      aria-controls={selected ? detailsId : undefined}
      onClick={onClick}
      className={cx("flex min-h-10 items-center gap-2.5 rounded-control px-2.5 py-2 text-left transition-[background-color,transform] duration-150 hover:bg-hover active:scale-[0.99]", selected && "bg-hover-2")}
    >
      <AgentMark agent={group.provider} label={name} decorative size={15} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-meta font-medium text-ink">{name}</span>
        <span className="block truncate text-submeta text-ink-3">{[reset, group.accounts.length > 1 ? tr("usage.provider_accounts", { count: group.accounts.length }) : planNote(primary)].filter(Boolean).join(" · ")}</span>
      </span>
      <span title={primary.name} className={cx("min-w-0 max-w-1/2 truncate text-submeta font-semibold tabular-nums", primary.stale && "opacity-60", hasQuota ? valueTone(used) : "text-ink-2")}>
        {hasQuota ? shown(used) : primary.balance ? balanceText(primary.balance) : "—"}
      </span>
      <Icon name={selected ? "chevron" : "chevronRight"} size={12} className="shrink-0 text-ink-3/80" />
    </button>
  );
}

function ProviderSection({ group, device, now, pinned, tooltip }: { group: ProviderGroup; device: DeviceGroup; now: number; pinned: ReadonlySet<string>; tooltip: UsageTooltipController }) {
  const providerName = providerTitle(group.provider);
  if (group.accounts.length === 1) {
    const a = group.accounts[0];
    const subLabel = distinctAccountLabel(a.name, providerName, group.provider, device);
    return (
      <section data-usage-provider={group.provider} aria-label={providerName} className="flex min-w-0 flex-col gap-2.5 px-4 py-2.5">
        <div role="group" aria-label={a.name} className={cx("flex flex-col gap-2.5", a.stale && "opacity-70")}>
          <header className="flex flex-col gap-0.5">
            <div className="flex items-center justify-between gap-2">
              <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
                <AgentMark agent={group.provider} label={providerName} decorative size={15} />
                <h4 className="shrink-0 text-meta font-semibold text-ink">{providerName}</h4>
                {subLabel && <span className="min-w-0 truncate text-label font-medium text-ink-2">{subLabel}</span>}
                <AccountSourceMeta a={a} providerId={group.provider} providerName={providerName} device={device} />
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                {a.stale && (
                  <span className="shrink-0 whitespace-nowrap text-submeta tabular-nums text-ink-3">
                    {tr("usage.stale_age", { age: sampleAge(a.sampled_at, now) })}
                  </span>
                )}
                <PinButton a={a} pinned={pinned.has(a.id)} tooltip={tooltip} />
              </div>
            </div>
            <AccountProblem a={a} />
          </header>
          <AccountBody a={a} now={now} />
        </div>
        <ProviderTimeline accounts={group.accounts} now={now} />
      </section>
    );
  }

  return (
    <section data-usage-provider={group.provider} aria-label={providerName} className="flex min-w-0 flex-col gap-2.5 px-4 py-2.5">
      <header className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <AgentMark agent={group.provider} label={providerName} decorative size={15} />
          <h4 className="min-w-0 truncate text-meta font-semibold text-ink">{providerName}</h4>
        </div>
        <span className="shrink-0 text-submeta font-medium tabular-nums text-ink-3">
          {tr("usage.provider_accounts", { count: group.accounts.length })}
        </span>
      </header>
      <div className="flex flex-col gap-3">
        {group.accounts.map((a, idx) => {
          const subLabel = distinctAccountLabel(a.name, providerName, group.provider, device) || a.name;
          const ageText = a.stale ? tr("usage.stale_age", { age: sampleAge(a.sampled_at, now) }) : sampleAge(a.sampled_at, now);
          return (
            <section
              key={a.id}
              aria-label={a.name}
              className={cx("flex flex-col gap-2", idx > 0 && "border-t border-line/45 pt-2.5", a.stale && "opacity-70")}
            >
              <header className="flex flex-col gap-0.5">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
                    <h5 className="min-w-0 truncate text-label font-medium text-ink">{subLabel}</h5>
                    <AccountSourceMeta a={a} providerId={group.provider} providerName={providerName} device={device} />
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    {ageText && <span className="shrink-0 text-submeta tabular-nums text-ink-3">{ageText}</span>}
                    <PinButton a={a} pinned={pinned.has(a.id)} tooltip={tooltip} />
                  </div>
                </div>
                <AccountProblem a={a} />
              </header>
              <AccountBody a={a} now={now} />
            </section>
          );
        })}
      </div>
      <ProviderTimeline accounts={group.accounts} now={now} />
    </section>
  );
}

function PinButton({ a, pinned, tooltip }: { a: UsageAccount; pinned: boolean; tooltip: UsageTooltipController }) {
  return (
    <TooltipTrigger
      controller={tooltip}
      payload={pinned ? tr("usage.unpin_title") : tr("usage.pin_title")}
      type="button"
      data-usage-pin={a.id}
      aria-pressed={pinned}
      aria-label={pinned ? tr("usage.unpin_account", { name: a.name }) : tr("usage.pin_account", { name: a.name })}
      onClick={() => togglePinnedUsageAccount(a.id)}
      className={cx(
        "inline-flex size-6 shrink-0 items-center justify-center rounded-chip transition-[background-color,color,transform] duration-150 active:scale-[0.94]",
        pinned ? "bg-hover/80 text-accent-ink" : "text-ink-3/75 hover:bg-hover hover:text-ink",
      )}
    >
      <Icon name="pin" size={12} />
    </TooltipTrigger>
  );
}

function AccountSourceMeta({ a, providerId, providerName, device }: { a: UsageAccount; providerId: string; providerName: string; device: DeviceGroup }) {
  const from = sourceNames(a);
  const plan = planNote(a);
  if (!from.length && !plan) return null;
  const fullText = from.join(" · ");
  const extraSources = filterRedundantSources(from, a.name, providerId, providerName, device);
  const extraText = extraSources.join(" · ");
  const hasVisibleMeta = Boolean(plan) || extraSources.length > 0;
  return (
    <div data-usage-source className={cx("inline-flex min-w-0 items-center gap-1.5 text-submeta text-ink-3", !hasVisibleMeta && "sr-only")}>
      {plan && <span className="shrink-0 font-medium whitespace-nowrap text-ink-3">{plan}</span>}
      {plan && extraSources.length > 0 && <span aria-hidden className="shrink-0 text-ink-3/50">·</span>}
      {extraSources.length > 0 && (
        <span className="min-w-0 truncate text-ink-3" title={tr("usage.sources_title", { sources: fullText })}>
          {extraText}
        </span>
      )}
      {from.length > 0 && extraText !== fullText && <span className="sr-only">{fullText}</span>}
    </div>
  );
}

function AccountProblem({ a }: { a: UsageAccount }) {
  const problem = isBalanceAccount(a) ? balanceProblemText(a) : problemText(a);
  if (!problem) return null;
  return <div className="pt-0.5 text-submeta leading-snug text-orange">{problem}</div>;
}

function AccountBody({ a, now }: { a: UsageAccount; now: number }) {
  return (
    <>
      {a.balance && (
        <div data-usage-balance className="flex items-baseline justify-between gap-2 border-t border-line/50 pt-2">
          <span className="text-label text-ink-3">{tr("usage.balance_label")}</span>
          <span className={cx("text-meta font-semibold tabular-nums", balanceEmpty(a.balance) ? "text-orange" : "text-ink")}>{balanceText(a.balance)}</span>
        </div>
      )}
      {!a.balance && isBalanceAccount(a) && <div data-usage-balance-unavailable className="text-label text-orange">{tr("usage.balance_unavailable")}</div>}
      {a.windows.length > 0 && (
        <div className="flex flex-col gap-2.5">
          {sortedWindows(a).map((w) => (
            <WindowRow key={windowKey(w)} name={a.name} w={w} stale={Boolean(a.stale)} now={now} />
          ))}
        </div>
      )}
    </>
  );
}

const TIMELINE_NAME_W = "4.75rem";
const LANE_COLOR = { short: "var(--hue-teal)", long: "var(--hue-blue)" } as const;

/** Collapsible reset timeline for a provider block (#13173): tucked neatly out of the way until opened, with dynamic per-lane spacing so windows never collide. */
function ProviderTimeline({ accounts, now }: { accounts: UsageAccount[]; now: number }) {
  const rows = providerTimeline(accounts, now);
  if (!rows.length) return null;
  const tickLabel = (h: number) => (h < 24 ? tr("usage.hours", { h }) : tr("usage.days", { d: h / 24 }));
  return (
    <div data-usage-timeline className="@container/usage-timeline border-t border-line/50 pt-2">
      <Disclosure>
        <DisclosureTrigger className="group/timeline flex w-full items-center justify-between gap-2 py-0.5 text-submeta font-medium text-ink-3 select-none transition-colors duration-150 hover:text-ink">
          <span className="inline-flex items-center gap-1.5">
            <Icon name="clock" size={12} className="shrink-0 text-ink-3" />
            <span>{tr("usage.timeline_title")}</span>
          </span>
          <Icon
            name="chevron"
            size={11}
            className="-rotate-90 shrink-0 text-ink-3/80 transition-transform duration-200 ease-(--ease-out-strong) group-data-[open]/timeline:rotate-0"
          />
        </DisclosureTrigger>
        <DisclosurePanel keepMounted>
          <div role="group" aria-label={tr("usage.timeline_title")} className="pt-2.5 pb-1">
        <div className="relative mb-1.5 h-3.5" style={{ marginLeft: TIMELINE_NAME_W }} aria-hidden>
          <span className="absolute right-full top-0 mr-2 whitespace-nowrap text-submeta font-medium leading-none text-ink-2">{tr("usage.timeline_now")}</span>
          {TIMELINE_TICKS_HOURS.map((h, i) => {
            const denseHide = h === 6 || h === 120;
            return (
              <span
                key={h}
                className={cx(
                  "absolute top-0 whitespace-nowrap text-submeta leading-none tabular-nums text-ink-3",
                  denseHide && "@max-[30rem]/usage-timeline:hidden",
                  i === TIMELINE_TICKS_HOURS.length - 1 ? "-translate-x-full" : "-translate-x-1/2",
                )}
                style={{ left: `${timelinePosition(h * 3600_000) * 100}%` }}
              >
                {tickLabel(h)}
              </span>
            );
          })}
        </div>
        <div className="relative" style={{ marginLeft: TIMELINE_NAME_W }}>
          {TIMELINE_TICKS_HOURS.map((h) => (
            <span
              key={h}
              aria-hidden
              className={cx("absolute inset-y-0 w-px", h === 24 ? "bg-line-strong/80" : "bg-line/70")}
              style={{ left: `${timelinePosition(h * 3600_000) * 100}%` }}
            />
          ))}
          <span aria-hidden data-timeline-now className="absolute -top-0.5 bottom-0 z-[3] w-px rounded-full bg-ink/70" />
          {rows.map((r, i) => {
            const parts = r.lanes.map((l) => `${l.label} · ${resetLabel(l.resetsAt, now)}`);
            const rowHeight = Math.max(26, r.lanes.length * 16 + 8);
            return (
              <div
                key={r.account.id}
                data-timeline-row={r.account.id}
                role="img"
                aria-label={`${r.account.name}: ${parts.join("; ")}`}
                title={parts.join("\n")}
                className={cx("relative", i > 0 && "border-t border-line/40", r.account.stale && "opacity-60")}
                style={{ height: rowHeight }}
              >
                <span
                  className="absolute top-0 z-[1] flex flex-col justify-center truncate pr-2 text-submeta leading-tight text-ink-3"
                  style={{ left: `-${TIMELINE_NAME_W}`, width: TIMELINE_NAME_W, height: rowHeight }}
                >
                  <span className="truncate text-ink-2">{r.account.name}</span>
                  {planNote(r.account) && <small className="truncate text-ink-3">{planNote(r.account).split(" ")[0]}</small>}
                </span>
                {r.lanes.map((l, laneIndex) => {
                  const color = l.long ? LANE_COLOR.long : LANE_COLOR.short;
                  const centerY = r.lanes.length === 1 ? Math.round(rowHeight / 2) : 10 + laneIndex * 16;
                  const flip = l.end > 0.76;
                  return (
                    <span key={l.key} data-timeline-lane={l.label}>
                      <span
                        aria-hidden
                        className="absolute left-0 z-[1] h-[3px] -translate-y-1/2 rounded-full"
                        style={{
                          top: centerY,
                          width: `${l.end * 100}%`,
                          backgroundImage: `linear-gradient(90deg, color-mix(in srgb, ${color} 18%, transparent), ${color})`,
                        }}
                      />
                      <span
                        aria-hidden
                        className="absolute z-[2] size-[9px] -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-surface"
                        style={{ left: `${l.end * 100}%`, top: centerY, backgroundColor: color }}
                      />
                      <span
                        aria-hidden
                        className="absolute z-[2] -translate-y-1/2 whitespace-nowrap text-submeta font-medium leading-none tabular-nums text-ink-2"
                        style={{
                          top: centerY,
                          ...(flip ? { right: `calc(${100 - l.end * 100}% + 8px)` } : { left: `calc(${l.end * 100}% + 8px)` }),
                        }}
                      >
                        {remainingText(l.remainingMs)}
                      </span>
                    </span>
                  );
                })}
              </div>
            );
          })}
        </div>
          </div>
        </DisclosurePanel>
      </Disclosure>
    </div>
  );
}

function WindowRow({ name, w, stale, now }: { name: string; w: UsageWindow; stale: boolean; now: number }) {
  const p = accountPace({ stale }, w, now);
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5 text-submeta">
        <span className="shrink-0 font-mono text-submeta font-semibold tabular-nums text-ink-2">{w.label}</span>
        {w.model && (
          <>
            <span aria-hidden className="shrink-0 text-ink-3/50">·</span>
            <span data-usage-model className="min-w-0 max-w-[11.5rem] truncate text-submeta text-ink-3" title={w.model}>
              {w.model}
            </span>
          </>
        )}
        <span className="min-w-0 flex-1 truncate text-right text-ink-3">{resetLabel(w.resets_at, now)}</span>
        {p && p.state !== "on" && <span className={cx("shrink-0 whitespace-nowrap font-medium", PACE_TONE[p.state])}>{paceLabel(p.state)}</span>}
        <span className={cx("w-9 shrink-0 text-right text-label font-semibold tabular-nums", valueTone(w.used_percent))}>
          {shown(w.used_percent)}
        </span>
      </div>
      <Meter
        used={w.used_percent}
        expected={p?.expected}
        protect={w.protect_percent}
        label={w.model ? tr("usage.window_model_label", { name, window: w.label, model: w.model, percent: shown(w.used_percent) }) : tr("usage.window_used_label", { name, window: w.label, percent: shown(w.used_percent) })}
        tall
      />
    </div>
  );
}
