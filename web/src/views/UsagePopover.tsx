// The account windows and error details behind a click on the sidebar's usage bars (#10082): these are not needed
// for the cold first screen, so the overlay and its long strings load on intent.
import { useEffect, useId, useState } from "react";
import { t as tr } from "../i18n";
import { BUSY_MOTION, spinStyle } from "../lib/busy-motion";
import { sendDraft } from "../lib/drafts";
import { navigate } from "../lib/router";
import { getState } from "../lib/store";
import { balanceEmpty, balanceText } from "../lib/usage-balance";
import { accountPace, fullest, groupByProvider, isBalanceAccount, isWarn, planNote, providerTimeline, providerTitle, refreshUsageWindows, remainingText, resetLabel, resetPinnedUsageAccounts, sampleAge, sortedWindows, sourceNames, TIMELINE_TICKS_HOURS, timelinePosition, togglePinnedUsageAccount, topAccounts, usePinnedUsageAccounts, windowKey, type PaceState, type ProviderGroup, type UnconnectedProvider, type UsageAccount, type UsageWindow } from "../lib/usage-windows";
import { AgentMark } from "../ui/AgentMark";
import { cx } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { Meter } from "./UsageBars";

const PACE_TEXT: Record<PaceState, string> = { over: "text-orange", under: "text-green/85", on: "text-ink-3" };
const paceLabel = (s: PaceState) => (s === "over" ? tr("usage.pace_over") : s === "under" ? tr("usage.pace_under") : tr("usage.pace_on"));
const shown = (p: number) => `${Math.round(p)}%`;

function groupUsageOrder(group: ProviderGroup): number {
  const primary = topAccounts(group.accounts, 1)[0];
  return primary ? fullest(primary) + (primary.stale ? 0 : 101) : -1;
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

/** Every live account, window by window, plus active providers without usage data yet. */
export function UsagePopover({
  id,
  accounts,
  unconnected = [],
  now: fixed,
  onClose,
  onRefresh = refreshUsageWindows,
}: {
  id?: string;
  accounts: UsageAccount[];
  unconnected?: readonly UnconnectedProvider[];
  now?: number;
  onClose?: () => void;
  onRefresh?: () => Promise<boolean>;
}) {
  const pinnedIds = usePinnedUsageAccounts();
  const pinnedSet = new Set(pinnedIds);
  const livePinnedCount = accounts.reduce((n, a) => n + (pinnedSet.has(a.id) ? 1 : 0), 0);
  const [view, setView] = useState<"detailed" | "compact">("detailed");
  const [provider, selectProvider] = useState<string>();
  const [refresh, setRefresh] = useState<"idle" | "loading" | "done" | "failed">("idle");
  const refreshing = refresh === "loading";
  const detailsId = useId();
  const groups = groupByProvider(accounts).sort((a, b) => groupUsageOrder(b) - groupUsageOrder(a));
  const selected = view === "compact" ? groups.find((g) => g.provider === provider) : undefined;
  const [tick, setTick] = useState(() => Date.now());
  useEffect(() => {
    if (fixed !== undefined) return;
    const t = setInterval(() => setTick(Date.now()), 1000);
    return () => clearInterval(t);
  }, [fixed]);
  const now = fixed ?? tick;
  const refreshAccounts = async () => {
    setRefresh("loading");
    setRefresh(await onRefresh() ? "done" : "failed");
  };
  const askAssistant = (p: UnconnectedProvider) => {
    sendDraft("persona-new", { type: "replace", text: connectPrompt(p) });
    onClose?.();
    const onStarter = typeof location !== "undefined" && (location.pathname === "/persona/new" || (location.pathname === "/persona" && !getState().assistant?.main));
    if (!onStarter) navigate("/persona/new");
  };
  return (
    <div
      id={id}
      role="dialog"
      aria-label={tr("usage.title")}
      data-usage-popover
      className={cx(
        "absolute bottom-full left-2.5 right-2.5 z-20 mb-2 flex max-h-[68dvh] flex-col overflow-hidden rounded-card border border-line bg-surface shadow-overlay lg:right-auto",
        selected ? "lg:w-176" : "lg:w-100",
      )}
    >
      <header className="shrink-0 border-b border-line/80 px-4 py-2.5">
        <div className="flex items-center gap-2">
          <Icon name="gauge" size={14} className="shrink-0 text-ink-3" />
          <h2 className="text-meta font-semibold tracking-[-0.01em] text-ink">{tr("usage.short_title")}</h2>
          <span className="min-w-0 flex-1 truncate text-submeta text-ink-3">{tr("usage.used")}</span>
          {groups.length > 0 && (
            <div role="group" aria-label={tr("usage.display")} className="inline-flex shrink-0 items-center rounded-lg bg-inset p-0.5 ring-1 ring-inset ring-line/60">
              {(["detailed", "compact"] as const).map((mode) => (
                <button
                  key={mode}
                  type="button"
                  aria-pressed={view === mode}
                  onClick={() => setView(mode)}
                  className={cx(
                    "min-h-7 rounded-md px-2.5 text-submeta font-medium transition-colors",
                    view === mode ? "bg-surface text-ink shadow-hairline" : "text-ink-3 hover:text-ink",
                  )}
                >
                  {mode === "detailed" ? tr("usage.detailed") : tr("usage.compact")}
                </button>
              ))}
            </div>
          )}
          <button
            type="button"
            aria-label={tr("usage.refresh")}
            title={tr("usage.refresh")}
            disabled={refreshing}
            aria-busy={refreshing || undefined}
            onClick={refreshAccounts}
            className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg text-ink-3 transition-colors hover:bg-hover hover:text-ink disabled:opacity-60"
          >
            <Icon name="retry" size={14} className={refreshing ? BUSY_MOTION : undefined} style={refreshing ? spinStyle(900) : undefined} />
          </button>
        </div>
        <div role="status" className={refresh === "failed" ? "pt-1.5 text-submeta text-orange" : "sr-only"}>
          {refreshing ? tr("usage.refreshing") : refresh === "done" ? tr("usage.refreshed") : refresh === "failed" ? tr("usage.refresh_failed") : ""}
        </div>
      </header>
      <div className={cx("min-h-0 overflow-y-auto overscroll-contain", selected && "lg:grid lg:grid-cols-2 lg:items-start lg:divide-x lg:divide-line/80")}>
        <div className="flex min-w-0 flex-col">
          {pinnedIds.length > 0 && accounts.length > 0 && (
            <div data-usage-pinned-bar className="flex items-center justify-between gap-2 border-b border-line/80 bg-inset/40 px-4 py-2 text-submeta">
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
          {view === "detailed" ? (
            <div className="divide-y divide-line/80">
              {groups.map((g) => (
                <ProviderCard key={g.provider} group={g} now={now} pinned={pinnedSet} />
              ))}
            </div>
          ) : (
            <div className="flex flex-col gap-0.5 p-2">
              {groups.map((g) => (
                <ProviderSummary
                  key={g.provider}
                  group={g}
                  selected={selected === g}
                  detailsId={detailsId}
                  now={now}
                  onClick={() => selectProvider(selected === g ? undefined : g.provider)}
                />
              ))}
            </div>
          )}
          {unconnected.length > 0 && (
            <div className={cx("divide-y divide-line/70 px-4", accounts.length > 0 && "border-t border-line/80")}>
              {unconnected.map((p) => (
                <section key={`provider:${p.id}`} aria-label={p.title} className="flex flex-col gap-1 py-3 text-label">
                  <div className="flex items-baseline gap-2">
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
        {selected && (
          <div id={detailsId} className="min-w-0 border-t border-line/80 lg:border-t-0">
            <ProviderCard group={selected} now={now} pinned={pinnedSet} />
          </div>
        )}
      </div>
    </div>
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
      className={cx("flex min-h-10 items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-hover", selected && "bg-hover")}
    >
      <AgentMark agent={group.provider} label={name} decorative size={15} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-meta font-medium text-ink">{name}</span>
        <span className="block truncate text-submeta text-ink-3">{[reset, group.accounts.length > 1 ? tr("usage.provider_accounts", { count: group.accounts.length }) : primary.plan].filter(Boolean).join(" · ")}</span>
      </span>
      <span title={primary.name} className={cx("min-w-0 max-w-1/2 truncate text-submeta font-semibold tabular-nums", primary.stale && "opacity-60", hasQuota && isWarn(used) ? "text-orange" : "text-ink-2")}>
        {hasQuota ? shown(used) : primary.balance ? balanceText(primary.balance) : "—"}
      </span>
      <Icon name={selected ? "chevron" : "chevronRight"} size={12} className="shrink-0 text-ink-3/80" />
    </button>
  );
}

function ProviderCard({ group, now, pinned }: { group: ProviderGroup; now: number; pinned: ReadonlySet<string> }) {
  const name = providerTitle(group.provider);
  const metaParts = [
    tr("usage.provider_accounts", { count: group.accounts.length }),
    group.sampledAt ? sampleAge(group.sampledAt, now) : "",
  ].filter(Boolean);
  return (
    <section data-usage-provider={group.provider} aria-label={name} className="flex min-w-0 flex-col gap-3 px-4 py-3.5">
      <header className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <AgentMark agent={group.provider} label={name} decorative size={15} />
          <h3 className="min-w-0 truncate text-meta font-semibold text-ink">{name}</h3>
        </div>
        <span className="shrink-0 text-submeta tabular-nums text-ink-3">{metaParts.join(" · ")}</span>
      </header>
      <div className="flex flex-col divide-y divide-line/60">
        {group.accounts.map((a, i) => {
          const compactSingle = group.accounts.length === 1 && a.name === name;
          return (
            <div key={a.id} className={cx("flex flex-col", i > 0 && "pt-3", i < group.accounts.length - 1 && "pb-3")}>
              <AccountSection a={a} now={now} pinned={pinned.has(a.id)} hideDuplicateName={compactSingle} />
            </div>
          );
        })}
      </div>
      <ProviderTimeline accounts={group.accounts} now={now} />
    </section>
  );
}

function AccountSection({ a, now, pinned, hideDuplicateName }: { a: UsageAccount; now: number; pinned: boolean; hideDuplicateName?: boolean }) {
  const from = sourceNames(a);
  const plan = planNote(a);
  const problem = isBalanceAccount(a) ? balanceProblemText(a) : problemText(a);
  const ageText = a.stale ? tr("usage.stale_age", { age: sampleAge(a.sampled_at, now) }) : sampleAge(a.sampled_at, now);
  const pinButton = (
    <button
      type="button"
      data-usage-pin={a.id}
      aria-pressed={pinned}
      aria-label={pinned ? tr("usage.unpin_account", { name: a.name }) : tr("usage.pin_account", { name: a.name })}
      title={pinned ? tr("usage.unpin_title") : tr("usage.pin_title")}
      onClick={() => togglePinnedUsageAccount(a.id)}
      className={cx(
        "inline-flex size-6 shrink-0 items-center justify-center rounded-md transition-colors",
        pinned ? "bg-hover text-accent-ink" : "text-ink-3/75 hover:bg-hover hover:text-ink",
      )}
    >
      <Icon name="pin" size={12} />
    </button>
  );

  return (
    <section aria-label={a.name} className={cx("flex flex-col gap-2.5", a.stale && "opacity-70")}>
      <header className="flex flex-col gap-0.5">
        {hideDuplicateName ? (
          <div className="flex items-center justify-between gap-2">
            <div className="flex min-w-0 flex-1 items-baseline gap-1.5 text-submeta text-ink-3">
              {(from.length > 0 || plan) && (
                <div data-usage-source className="flex min-w-0 items-baseline gap-1.5">
                  {plan && <span className="shrink-0 whitespace-nowrap font-medium text-ink-2">{plan}</span>}
                  {plan && from.length > 0 && <span aria-hidden>·</span>}
                  <span className="min-w-0 truncate" title={tr("usage.sources_title", { sources: from.join(" · ") })}>{from.join(" · ")}</span>
                </div>
              )}
              {a.stale && <span className="shrink-0 whitespace-nowrap text-ink-3">{ageText}</span>}
            </div>
            {pinButton}
          </div>
        ) : (
          <>
            <div className="flex items-center gap-1.5">
              <div className="flex min-w-0 flex-1 items-baseline gap-2">
                <h3 className="min-w-0 truncate text-label font-medium text-ink">{a.name}</h3>
                <span className="shrink-0 text-submeta tabular-nums text-ink-3">{ageText}</span>
              </div>
              {pinButton}
            </div>
            {(from.length > 0 || plan) && (
              <div data-usage-source className="flex min-w-0 items-baseline gap-1.5 text-submeta text-ink-3">
                {plan && <span className="shrink-0 whitespace-nowrap">{plan}</span>}
                {plan && from.length > 0 && <span aria-hidden>·</span>}
                <span className="min-w-0 truncate" title={tr("usage.sources_title", { sources: from.join(" · ") })}>{from.join(" · ")}</span>
              </div>
            )}
          </>
        )}
        {problem && <div className="pt-0.5 text-submeta leading-snug text-orange">{problem}</div>}
      </header>
      {a.balance && (
        <div data-usage-balance className="flex items-baseline justify-between gap-2 rounded-lg bg-inset/60 px-3 py-2 ring-1 ring-inset ring-line/60">
          <span className="text-label text-ink-3">{tr("usage.balance_label")}</span>
          <span className={cx("text-meta font-semibold tabular-nums", balanceEmpty(a.balance) ? "text-orange" : "text-ink")}>{balanceText(a.balance)}</span>
        </div>
      )}
      {!a.balance && isBalanceAccount(a) && <div data-usage-balance-unavailable className="text-label text-orange">{tr("usage.balance_unavailable")}</div>}
      <div className="flex flex-col gap-2.5">
        {sortedWindows(a).map((w) => (
          <WindowRow key={windowKey(w)} name={a.name} w={w} stale={Boolean(a.stale)} now={now} />
        ))}
      </div>
    </section>
  );
}

const TIMELINE_NAME_W = "3.75rem";
const LANE_COLOR = { short: "var(--hue-teal)", long: "var(--hue-blue)" } as const;

/** One reset timeline for a whole provider section (#13173): shared axis, hairline rails and dots per account window. */
function ProviderTimeline({ accounts, now }: { accounts: UsageAccount[]; now: number }) {
  const rows = providerTimeline(accounts, now);
  if (!rows.length) return null;
  const tickLabel = (h: number) => (h < 24 ? tr("usage.hours", { h }) : tr("usage.days", { d: h / 24 }));
  return (
    <div data-usage-timeline className="@container/usage-timeline border-t border-line/60 pt-3" role="group" aria-label={tr("usage.timeline_title")}>
      <div className="relative mb-1.5 h-3.5" style={{ marginLeft: TIMELINE_NAME_W }} aria-hidden>
        <span className="absolute right-full top-0 mr-2 whitespace-nowrap text-submeta font-medium leading-none text-ink-2">{tr("usage.timeline_now")}</span>
        {TIMELINE_TICKS_HOURS.map((h, i) => {
          const denseHide = h === 6 || h === 120;
          return (
            <span
              key={h}
              className={cx(
                "absolute top-0 whitespace-nowrap text-submeta leading-none tabular-nums text-ink-3",
                denseHide && "@max-[23rem]/usage-timeline:hidden",
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
          return (
            <div
              key={r.account.id}
              data-timeline-row={r.account.id}
              role="img"
              aria-label={`${r.account.name}: ${parts.join("; ")}`}
              title={parts.join("\n")}
              className={cx("relative h-[30px]", i > 0 && "border-t border-line/40", r.account.stale && "opacity-60")}
            >
              <span
                className="absolute top-0 z-[1] flex h-[30px] flex-col justify-center truncate pr-2 text-submeta leading-tight text-ink-3"
                style={{ left: `-${TIMELINE_NAME_W}`, width: TIMELINE_NAME_W }}
              >
                <span className="truncate text-ink-2">{r.account.name}</span>
                {planNote(r.account) && <small className="truncate text-ink-3">{planNote(r.account).split(" ")[0]}</small>}
              </span>
              {r.lanes.map((l) => {
                const color = l.long ? LANE_COLOR.long : LANE_COLOR.short;
                const railTop = l.long ? 20 : 8;
                const dotTop = l.long ? 18 : 6;
                const textTop = l.long ? 16 : 4;
                const flip = l.end > 0.78;
                return (
                  <span key={l.key} data-timeline-lane={l.label}>
                    <span
                      aria-hidden
                      className="absolute left-0 z-[1] h-[3px] rounded-full"
                      style={{
                        top: railTop,
                        width: `${l.end * 100}%`,
                        backgroundImage: `linear-gradient(90deg, color-mix(in srgb, ${color} 18%, transparent), ${color})`,
                      }}
                    />
                    <span
                      aria-hidden
                      className="absolute z-[2] size-[7px] -translate-x-1/2 rounded-full ring-2 ring-surface"
                      style={{ left: `${l.end * 100}%`, top: dotTop, backgroundColor: color }}
                    />
                    <span
                      aria-hidden
                      className="absolute z-[2] whitespace-nowrap text-submeta font-medium leading-none tabular-nums text-ink-2"
                      style={{
                        top: textTop,
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
  );
}

function WindowRow({ name, w, stale, now }: { name: string; w: UsageWindow; stale: boolean; now: number }) {
  const p = accountPace({ stale }, w, now);
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline gap-2 text-submeta">
        <span className="shrink-0 font-medium tabular-nums text-ink">{w.label}</span>
        {w.model && (
          <span data-usage-model className="max-w-[7rem] shrink-0 truncate rounded-chip bg-hover px-1.5 py-px text-submeta text-ink-2" title={w.model}>
            {w.model}
          </span>
        )}
        <span className="min-w-0 flex-1 truncate text-ink-3">{resetLabel(w.resets_at, now)}</span>
        {p && p.state !== "on" && <span className={cx("shrink-0 whitespace-nowrap", PACE_TEXT[p.state])}>{paceLabel(p.state)}</span>}
        <span className={cx("w-9 shrink-0 text-right font-semibold tabular-nums", isWarn(w.used_percent) ? "text-orange" : "text-ink")}>
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
