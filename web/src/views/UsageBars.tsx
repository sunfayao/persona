// The sidebar's bottom (#2432): compact quota meters; a click opens
// a popover with every window of every account, its reset countdown, when it was sampled and whether use is ahead of or
// behind an even pace. Hidden when there are no readings or unconnected providers.
import { type ComponentType, useEffect, useId, useRef, useState } from "react";
import { t as tr } from "../i18n";
import { fullest, isBalanceAccount, isWarn, liveAccounts, selectSidebarUsageAccounts, sourceNames, usageProvider, usePinnedUsageAccounts, useUsageWindows, type UnconnectedProvider, type UsageAccount } from "../lib/usage-windows";
import { AgentMark } from "../ui/AgentMark";
import { cx } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { lazyOverlay } from "./LazyOverlay";

const UsagePopover = lazyOverlay<{ id: string; accounts: UsageAccount[]; unconnected?: readonly UnconnectedProvider[]; now?: number; onClose: () => void }>(() => import("./UsagePopover").then((m) => m.UsagePopover));
let LoadedUnconnected: ComponentType<{ accounts: UsageAccount[]; onChange: (next: readonly UnconnectedProvider[]) => void }> | undefined;

const percent = (p: number) => Math.min(100, Math.max(0, p));
const shown = (p: number) => `${Math.round(p)}%`;
const fill = (p: number) => (isWarn(p) ? "bg-orange" : "bg-accent");
const VALUE = "shrink-0 text-submeta font-medium tabular-nums";

/** A bar: the track, the used part (caution colour from 80%) and, with `expected`, a tick where an even pace would be. */
export function Meter({ used, expected, protect, label, tall }: { used: number; expected?: number; protect?: number; label: string; tall?: boolean }) {
  const hasProtect = typeof protect === "number" && Number.isFinite(protect) && protect >= 0 && protect <= 100;
  return (
    <span role="progressbar" aria-label={hasProtect ? `${label}, ${tr("usage.protect_label", { percent: Math.round(protect) })}` : label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(percent(used))} className={cx("relative block w-full rounded-full bg-inset", tall ? "h-1.5" : "h-1", hasProtect && "mt-3.5")}>
      <span className={cx("block h-full rounded-full motion-safe:transition-[width] motion-safe:duration-300", fill(used))} style={{ width: `${percent(used)}%` }} />
      {hasProtect && (
        <>
          <span aria-hidden data-protect-zone className="absolute inset-y-0 right-0 rounded-r-full" style={{ left: `${percent(protect)}%`, backgroundImage: "repeating-linear-gradient(135deg, transparent 0 2px, color-mix(in srgb, var(--orange) 45%, transparent) 2px 3px)" }} />
          <span aria-hidden data-protect-line className="absolute -top-1 -bottom-1 w-0.5 -translate-x-1/2 rounded-full bg-orange" style={{ left: `${percent(protect)}%` }} />
          <span aria-hidden data-protect-flag className="absolute -top-4 -translate-x-1/2 whitespace-nowrap rounded-sm bg-surface px-0.5 text-submeta font-bold leading-none text-orange" style={{ left: `${Math.min(percent(protect), 92)}%` }}>
            {tr("usage.protect_flag", { percent: Math.round(protect) })}
          </span>
        </>
      )}
      {expected !== undefined && <span aria-hidden data-pace-tick className="absolute -top-0.5 -bottom-0.5 w-0.5 -translate-x-1/2 rounded-full bg-ink-2" style={{ left: `${percent(expected)}%` }} />}
    </span>
  );
}

const EMPTY_UNCONNECTED: readonly UnconnectedProvider[] = [];
const VISIBLE_USAGE_ITEMS = 4;

/** The sidebar's strip. `accounts` as the hub gave them; `unconnected` lists active providers without usage data yet. */
export function UsageBars({ accounts, unconnected = EMPTY_UNCONNECTED, now }: { accounts: UsageAccount[]; unconnected?: readonly UnconnectedProvider[]; now?: number }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  const pinnedIds = usePinnedUsageAccounts();
  const live = liveAccounts(accounts, now);
  const { bars: top, balances } = selectSidebarUsageAccounts(live, pinnedIds);
  const shownAccounts = [...top, ...balances].slice(0, VISIBLE_USAGE_ITEMS);
  const shownUnconnected = unconnected.slice(0, VISIBLE_USAGE_ITEMS - shownAccounts.length);
  const extra = live.length + unconnected.length - shownAccounts.length - shownUnconnected.length;

  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      e.preventDefault();
      setOpen(false);
      trigger.current?.focus();
    };
    addEventListener("pointerdown", down, true);
    addEventListener("keydown", key);
    return () => {
      removeEventListener("pointerdown", down, true);
      removeEventListener("keydown", key);
    };
  }, [open]);

  if (!top.length && !balances.length && !unconnected.length) return null;
  return (
    <div ref={root} className="@container/usage relative shrink-0 border-t border-line bg-canvas px-2.5" data-usage-bars>
      <button
        ref={trigger}
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        title={tr("usage.title")}
        className={cx("touch-hit flex min-h-11 w-full items-center gap-2 rounded-control px-1 text-left transition-colors hover:bg-hover", open && "bg-hover")}
      >
        <span className="shrink-0 text-submeta font-semibold text-ink-2 @max-[17rem]/usage:sr-only">{tr("usage.short_title")}</span>
        <span className="flex min-w-0 flex-1 items-center gap-1.5 py-1.5">
          {shownAccounts.map((a) => {
            if (isBalanceAccount(a)) return <BalanceSummary key={a.id} a={a} />;
            const used = fullest(a);
            const from = sourceNames(a);
            const label = tr("usage.used_label", { name: a.name, percent: shown(used) });
            const hint = [label, from.length ? tr("usage.sources_title", { sources: from.join(" · ") }) : "", a.stale ? tr("usage.stale_title") : ""].filter(Boolean).join(" · ");
            return (
              <span key={a.id} data-usage-summary={a.id} className={cx("inline-flex shrink-0 items-center gap-1 rounded-control bg-inset px-1.5 py-1", a.stale && "opacity-60")} title={hint}>
                <AgentMark agent={usageProvider(a.provider)} label={a.name} decorative size={12} />
                <span className="sr-only">{a.name}</span>
                <span className="w-4 @max-[20rem]/usage:hidden"><Meter used={used} label={label} /></span>
                <span className={cx(VALUE, isWarn(used) ? "text-orange" : "text-ink-2")}>{shown(used)}</span>
              </span>
            );
          })}
          {shownUnconnected.map((p) => (
            <span key={p.id} data-usage-unconnected={p.id} className="inline-flex shrink-0 items-center gap-1 rounded-control bg-inset px-1.5 py-1 text-ink-3" title={`${p.title} · ${tr("usage.not_connected")}`}>
              <AgentMark agent={p.agent} label={p.title} decorative size={12} />
              <span className="sr-only">{p.title} · {tr("usage.not_connected")}</span>
              <span aria-hidden className={VALUE}>—</span>
            </span>
          ))}
          {extra > 0 && (
            <span data-usage-overflow className="shrink-0 rounded-control bg-inset px-1.5 py-1 text-submeta font-medium tabular-nums text-ink-2">
              <span className="sr-only">{tr("usage.more", { count: extra })}</span>
              <span aria-hidden>+{extra}</span>
            </span>
          )}
        </span>
        <Icon name="chevron" size={12} className={cx("shrink-0 text-ink-3", !open && "rotate-180")} />
      </button>
      {open && <UsagePopover id={panelId} accounts={live} unconnected={unconnected} now={now} onClose={() => setOpen(false)} />}
    </div>
  );
}

/** A prepaid account's summary: the amount and currency, no bar. A genuine zero and an unreadable balance are both
 *  caution-coloured, but only the first shows an amount. */
function BalanceSummary({ a }: { a: UsageAccount }) {
  // Why a balance is missing or old is the popover's detail (progressive disclosure); the strip only fades a stale one.
  return (
    <span className={cx("inline-flex min-w-0 max-w-full items-center gap-1 rounded-control bg-inset px-1.5 py-1", a.stale && "opacity-60")} title={a.balance ? `${a.name} · ${a.balance.currency} ${a.balance.amount}` : a.name}>
      <AgentMark agent={usageProvider(a.provider)} label={a.name} decorative size={12} />
      <span className="sr-only">{a.name}</span>
      <span className={cx("truncate text-submeta font-medium tabular-nums", !a.balance || Number(a.balance.amount) === 0 ? "text-orange" : "text-ink-2")}>
        {a.balance ? `${a.balance.currency} ${a.balance.amount}`.trim() : "—"}
      </span>
    </span>
  );
}

/** The sidebar mounts. */
export function SidebarUsage() {
  return <SidebarUsageInner accounts={useUsageWindows()} />;
}

function SidebarUsageInner({ accounts }: { accounts: UsageAccount[] }) {
  const [, setLoaded] = useState(() => Boolean(LoadedUnconnected));
  const [unconnected, setUnconnected] = useState(EMPTY_UNCONNECTED);
  useEffect(() => {
    if (LoadedUnconnected) return;
    let active = true;
    void import("./UsageUnconnected").then((m) => {
      LoadedUnconnected = m.UsageUnconnected;
      if (active) setLoaded(true);
    });
    return () => { active = false; };
  }, []);
  return (
    <>
      {LoadedUnconnected && <LoadedUnconnected accounts={accounts} onChange={setUnconnected} />}
      <UsageBars accounts={accounts} unconnected={unconnected} />
    </>
  );
}
