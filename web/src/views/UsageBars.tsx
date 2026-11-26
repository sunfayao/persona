// The sidebar's bottom (#2432): compact quota meters; a click opens
// a popover with every window of every account, its reset countdown, when it was sampled and whether use is ahead of or
// behind an even pace. Hidden when there are no readings or unconnected providers.
import { type ComponentType, useEffect, useId, useRef, useState } from "react";
import { t as tr } from "../i18n";
import { fullest, isBalanceAccount, isCritical, isWarn, liveAccounts, selectSidebarUsageAccounts, sourceNames, usageProvider, usePinnedUsageAccounts, useUsageWindows, type UnconnectedProvider, type UsageAccount } from "../lib/usage-windows";
import { AgentMark } from "../ui/AgentMark";
import { cx } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { lazyOverlay } from "./LazyOverlay";

const UsagePopover = lazyOverlay<{ id: string; accounts: UsageAccount[]; unconnected?: readonly UnconnectedProvider[]; now?: number; onClose: () => void }>(() => import("./UsagePopover").then((m) => m.UsagePopover));
let LoadedUnconnected: ComponentType<{ accounts: UsageAccount[]; onChange: (next: readonly UnconnectedProvider[]) => void }> | undefined;

const percent = (p: number) => Math.min(100, Math.max(0, p));
const shown = (p: number) => `${Math.round(p)}%`;
const fill = (p: number) => (isCritical(p) ? "bg-red" : isWarn(p) ? "bg-orange" : "bg-accent");
const valueTone = (p: number) => (isCritical(p) ? "text-red" : isWarn(p) ? "text-orange" : "text-ink");
const VALUE = "shrink-0 text-submeta font-medium tabular-nums";
const PILL = "inline-flex shrink-0 items-center gap-1.5 rounded-md bg-inset/70 px-2 py-0.5 ring-1 ring-inset ring-line/55";

/** A bar: the track, the used part (caution colour from 80%, critical from 90%) and, with `expected`, a hairline tick where an even pace would be. */
export function Meter({ used, expected, protect, label, tall }: { used: number; expected?: number; protect?: number; label: string; tall?: boolean }) {
  const hasProtect = typeof protect === "number" && Number.isFinite(protect) && protect >= 0 && protect <= 100;
  return (
    <span
      role="progressbar"
      aria-label={hasProtect ? `${label}, ${tr("usage.protect_label", { percent: Math.round(protect) })}` : label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(percent(used))}
      className={cx("relative block w-full rounded-full bg-line-strong/80", tall ? "h-1.5" : "h-1", hasProtect && "mt-3.5")}
    >
      <span className={cx("block h-full rounded-full motion-safe:transition-[width] motion-safe:duration-300", fill(used))} style={{ width: `${percent(used)}%` }} />
      {hasProtect && (
        <>
          <span
            aria-hidden
            data-protect-zone
            className="absolute inset-y-0 right-0 rounded-r-full"
            style={{ left: `${percent(protect)}%`, backgroundImage: "repeating-linear-gradient(135deg, transparent 0 2px, color-mix(in srgb, var(--orange) 45%, transparent) 2px 3px)" }}
          />
          <span aria-hidden data-protect-line className="absolute -top-1 -bottom-1 w-0.5 -translate-x-1/2 rounded-full bg-orange" style={{ left: `${percent(protect)}%` }} />
          <span aria-hidden data-protect-flag className="absolute -top-4 -translate-x-1/2 whitespace-nowrap rounded-sm bg-surface px-0.5 text-submeta font-medium leading-none text-orange" style={{ left: `${Math.min(percent(protect), 92)}%` }}>
            {tr("usage.protect_flag", { percent: Math.round(protect) })}
          </span>
        </>
      )}
      {expected !== undefined && <span aria-hidden data-pace-tick className="absolute -top-0.5 -bottom-0.5 w-px -translate-x-1/2 rounded-full bg-ink/60" style={{ left: `${percent(expected)}%` }} />}
    </span>
  );
}

const EMPTY_UNCONNECTED: readonly UnconnectedProvider[] = [];
const SSR_VISIBLE_ITEMS = 4;
const ITEM_SLOT_PX = 76;
const OVERFLOW_BADGE_PX = 30;

function orderStripCandidates(bars: UsageAccount[], balances: UsageAccount[], custom: boolean): UsageAccount[] {
  if (custom) return [...bars, ...balances];
  const seen = new Set<string>();
  const distinctBars: UsageAccount[] = [];
  const extraBars: UsageAccount[] = [];
  for (const a of bars) {
    const p = usageProvider(a.provider);
    if (!seen.has(p)) {
      seen.add(p);
      distinctBars.push(a);
    } else {
      extraBars.push(a);
    }
  }
  return [...distinctBars, ...balances, ...extraBars];
}

/** The sidebar's strip. `accounts` as the hub gave them; `unconnected` lists active providers without usage data yet. */
export function UsageBars({ accounts, unconnected = EMPTY_UNCONNECTED, now }: { accounts: UsageAccount[]; unconnected?: readonly UnconnectedProvider[]; now?: number }) {
  const [open, setOpen] = useState(false);
  const [rowWidth, setRowWidth] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const rowRef = useRef<HTMLSpanElement>(null);
  const panelId = useId();
  const pinnedIds = usePinnedUsageAccounts();
  const live = liveAccounts(accounts, now);
  const { bars: top, balances, custom } = selectSidebarUsageAccounts(live, pinnedIds);
  const hasContent = top.length > 0 || balances.length > 0 || unconnected.length > 0;

  useEffect(() => {
    const el = rowRef.current;
    if (!el || !hasContent) return;
    const measure = (w: number) => {
      const next = Math.round(w);
      setRowWidth((prev) => (prev === next ? prev : next));
    };
    measure(el.getBoundingClientRect().width);
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) measure(entry.contentRect.width);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [hasContent]);

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

  if (!hasContent) return null;

  const candidates = orderStripCandidates(top, balances, custom);
  const totalItems = candidates.length + unconnected.length;
  const capacity =
    rowWidth <= 0
      ? SSR_VISIBLE_ITEMS
      : Math.floor(rowWidth / ITEM_SLOT_PX) >= totalItems
        ? totalItems
        : Math.max(1, Math.floor((rowWidth - OVERFLOW_BADGE_PX) / ITEM_SLOT_PX));
  const shownAccounts = candidates.slice(0, capacity);
  const shownUnconnected = unconnected.slice(0, Math.max(0, capacity - shownAccounts.length));
  const extra = totalItems - shownAccounts.length - shownUnconnected.length;

  return (
    <div ref={root} className="@container/usage relative shrink-0 border-t border-line/80 bg-canvas px-2 py-1" data-usage-bars>
      <button
        ref={trigger}
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        title={tr("usage.title")}
        className={cx("touch-hit flex min-h-9 w-full items-center gap-1.5 rounded-lg px-2 text-left transition-colors hover:bg-hover", open && "bg-hover")}
      >
        <span className="shrink-0 text-submeta font-medium text-ink-3 @max-[17rem]/usage:sr-only">{tr("usage.short_title")}</span>
        <span ref={rowRef} className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden py-1">
          {shownAccounts.map((a) => {
            if (isBalanceAccount(a)) return <BalanceSummary key={a.id} a={a} />;
            const used = fullest(a);
            const from = sourceNames(a);
            const label = tr("usage.used_label", { name: a.name, percent: shown(used) });
            const hint = [label, from.length ? tr("usage.sources_title", { sources: from.join(" · ") }) : "", a.stale ? tr("usage.stale_title") : ""].filter(Boolean).join(" · ");
            return (
              <span key={a.id} data-usage-summary={a.id} className={cx(PILL, a.stale && "opacity-60")} title={hint}>
                <AgentMark agent={usageProvider(a.provider)} label={a.name} decorative size={13} />
                <span className="sr-only">{a.name}</span>
                <span className="w-5 @max-[19rem]/usage:hidden"><Meter used={used} label={label} /></span>
                <span className={cx(VALUE, valueTone(used))}>{shown(used)}</span>
              </span>
            );
          })}
          {shownUnconnected.map((p) => (
            <span key={p.id} data-usage-unconnected={p.id} className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-inset/45 px-2 py-0.5 text-ink-3 ring-1 ring-inset ring-line/45" title={`${p.title} · ${tr("usage.not_connected")}`}>
              <AgentMark agent={p.agent} label={p.title} decorative size={13} />
              <span className="sr-only">{p.title} · {tr("usage.not_connected")}</span>
              <span aria-hidden className={VALUE}>—</span>
            </span>
          ))}
          {extra > 0 && (
            <span data-usage-overflow className="ml-auto shrink-0 rounded-md bg-inset/80 px-1.5 py-0.5 text-submeta font-medium tabular-nums text-ink-3 ring-1 ring-inset ring-line/55">
              <span className="sr-only">{tr("usage.more", { count: extra })}</span>
              <span aria-hidden>+{extra}</span>
            </span>
          )}
        </span>
        <Icon name="chevron" size={12} className={cx("ml-auto shrink-0 text-ink-3/80 transition-transform duration-150", !open && "rotate-180")} />
      </button>
      {open && <UsagePopover id={panelId} accounts={live} unconnected={unconnected} now={now} onClose={() => setOpen(false)} />}
    </div>
  );
}

/** A prepaid account's summary: the amount and currency, no bar. A genuine zero and an unreadable balance are both
 *  caution-coloured, but only the first shows an amount. */
function BalanceSummary({ a }: { a: UsageAccount }) {
  return (
    <span data-usage-summary={a.id} className={cx("inline-flex min-w-0 max-w-full shrink-0 items-center gap-1.5 rounded-md bg-inset/70 px-2 py-0.5 ring-1 ring-inset ring-line/55", a.stale && "opacity-60")} title={a.balance ? `${a.name} · ${a.balance.currency} ${a.balance.amount}` : a.name}>
      <AgentMark agent={usageProvider(a.provider)} label={a.name} decorative size={13} />
      <span className="sr-only">{a.name}</span>
      <span className={cx("truncate text-submeta font-medium tabular-nums", !a.balance || Number(a.balance.amount) === 0 ? "text-orange" : "text-ink")}>
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
