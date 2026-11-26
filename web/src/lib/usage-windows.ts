// Subscription rate-limit windows (#2432): GET /api/v1/usage/windows lists the accounts the
// machines' agents are signed in to with how much of each window (5h, 7d …) is used. Pure helpers here (pace,
// countdown, sample age, what the sidebar shows); the bars and the popover are views/UsageBars.tsx.
import { useSyncExternalStore } from "react";
import { formatDate, formatRelative, t as tr } from "../i18n";
import { api, ApiError } from "./api";

export interface UsageWindow {
  label: string;
  window_minutes: number;
  used_percent: number;
  resets_at?: string;
  /** An allowance a provider keeps for one model next to the plan's own (#2595); absent for the plan's window. */
  model?: string;
  /** Where scheduling stops using this window's account, 0-100; it can differ per account and per window. Absent
   *  when the hub (older, or no such rule) does not say: nothing is drawn then (#13173). */
  protect_percent?: number;
}
/** A prepaid balance (#11825): the provider's own decimal amount and currency, never turned into a percentage. */
export interface UsageBalance {
  amount: string;
  currency: string;
}
export interface UsageAccount {
  id: string;
  provider: string;
  name: string;
  plan?: string;
  windows: UsageWindow[];
  /** A remaining amount, for a provider with no rate-limit window (the official DeepSeek launcher). Such an account
   *  may have no windows; a genuine zero is `amount: "0.00"`, not a missing balance. */
  balance?: UsageBalance;
  sampled_at: string;
  stale?: boolean;
  /** When it turns stale if nothing newer comes (#1905): kept numbers are judged again against it (`liveAccounts`). */
  stale_at?: string;
  /** Where the numbers came from (#2595): an agent's files on one of the computers, and/or account-pool gateways by name. */
  local?: boolean;
  sources?: string[];
  /** The computers (by name) that read it (#1907); an older hub only says `local`. */
  machines?: string[];
  /** Why a gateway's numbers are old: "error" (it could not read this account just now) or, when its own last read
   *  failed, "rejected" (the key was turned away), "unreachable" or "failed". */
  problem?: string;
}

/** Samples older than this are dropped (a source that went quiet for days says nothing about now). */
const SAMPLE_MAX_AGE_MS = 3 * 24 * 3600_000;
/** From this used percentage a bar turns to the caution colour. */
const WARN_PERCENT = 80;
/** From this used percentage a bar and percentage turn to the critical colour. */
const CRITICAL_PERCENT = 90;
/** The pace is not judged in the first part of a window: a few minutes in, any use is "ahead". */
const PACE_QUIET_FRACTION = 0.05;
/** How far (percentage points) the actual use may be from the expected before it is over / under. */
const PACE_TOLERANCE = 5;
const POLL_MS = 60_000;

/** The window's length in minutes from its label ("5h", "7d", "45m"); the given minutes when the label is not one. */
export function windowMinutes(w: Pick<UsageWindow, "label" | "window_minutes">): number {
  const m = /^(\d+(?:\.\d+)?)\s*([mhdw])$/i.exec(w.label.trim());
  if (m) return Math.round(Number(m[1]) * { m: 1, h: 60, d: 1440, w: 10080 }[m[2].toLowerCase() as "m" | "h" | "d" | "w"]);
  return w.window_minutes > 0 ? w.window_minutes : 0;
}

export type PaceState = "over" | "under" | "on";
export interface Pace {
  /** Where use would be if it were spread evenly over the window, 0-100. */
  expected: number;
  state: PaceState;
}

/** How use compares with an even spread: expected% = elapsed / length; over when use is more than 5 points above it,
 *  under when more than 5 below. null with no reset time or length, and in the first 5% of the window. */
export function pace(w: UsageWindow, now = Date.now()): Pace | null {
  const total = windowMinutes(w) * 60_000;
  const resets = w.resets_at ? Date.parse(w.resets_at) : NaN;
  if (!(total > 0) || !Number.isFinite(resets)) return null;
  const elapsed = total - (resets - now);
  if (elapsed < 0 || elapsed > total) return null;
  if (elapsed / total < PACE_QUIET_FRACTION) return null;
  const expected = (elapsed / total) * 100;
  const diff = w.used_percent - expected;
  return { expected, state: diff > PACE_TOLERANCE ? "over" : diff < -PACE_TOLERANCE ? "under" : "on" };
}

/** The pace of one of an account's windows, as the popover shows it. An out-of-date reading says nothing about how
 *  fast it is being used now, so a stale account has none (#12411): its numbers and reset stay, faded and dated. */
export function accountPace(a: Pick<UsageAccount, "stale">, w: UsageWindow, now = Date.now()): Pace | null {
  return a.stale ? null : pace(w, now);
}

type Countdown = { kind: "due" | "clock" | "span"; text: string };

/** When a window starts over: the span ("45m", "2d 3h", "5d" with no hours left over; each language's own units, "2 天 3 小时"),
 *  the local clock time when that is today, or "due". null without a time. */
function countdownOf(resetsAt: string | undefined, now: number): Countdown | null {
  const at = resetsAt ? Date.parse(resetsAt) : NaN;
  if (!Number.isFinite(at)) return null;
  const ms = at - now;
  if (ms <= 0) return { kind: "due", text: tr("usage.due") };
  const mins = Math.ceil(ms / 60_000);
  if (mins < 60) return { kind: "span", text: tr("usage.minutes", { m: mins }) };
  if (new Date(at).toDateString() === new Date(now).toDateString()) return { kind: "clock", text: formatDate(at, { timeStyle: "short" }) };
  const hours = Math.floor(mins / 60);
  const d = Math.floor(hours / 24);
  const h = hours % 24;
  return { kind: "span", text: d === 0 ? tr("usage.hours", { h: hours }) : h === 0 ? tr("usage.days", { d }) : tr("usage.days_hours", { d, h }) };
}

/** When a window starts over, as it reads in the popover: "45m" within the hour, the clock time when it is today,
 *  otherwise "2d 3h" (days and hours, "5d" without a zero hour). "" without a time; "Resets soon" when it is due. */
export function countdown(resetsAt: string | undefined, now = Date.now()): string {
  return countdownOf(resetsAt, now)?.text ?? "";
}

/** The countdown as a phrase: "Resets in 55m", "Resets in 2d 3h", but a clock time reads "Resets at 1:05 PM". */
export function resetLabel(resetsAt: string | undefined, now = Date.now()): string {
  const c = countdownOf(resetsAt, now);
  if (!c) return "";
  if (c.kind === "due") return c.text;
  return c.kind === "clock" ? tr("usage.reset_at", { time: c.text }) : tr("usage.reset_in", { countdown: c.text });
}

/** How long ago a sample was taken ("12 seconds ago"); coarser past a minute. "" for a time that is not one. */
export function sampleAge(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  const ago = (n: number, unit: Intl.RelativeTimeFormatUnit) => formatRelative(-n, unit, { numeric: "always" });
  if (s < 60) return ago(s, "second");
  if (s < 3600) return ago(Math.floor(s / 60), "minute");
  if (s < 86400) return ago(Math.floor(s / 3600), "hour");
  return ago(Math.floor(s / 86400), "day");
}

/** The accounts worth showing: samples within three days, windows that have not reset, accounts that still have one;
 *  each stale once its `stale_at` has passed, however long ago the hub said it (#1905: the last answer is kept while
 *  refreshes fail, and its own stale mark was the hub's at that moment). */
export function liveAccounts(accounts: UsageAccount[], now = Date.now()): UsageAccount[] {
  const out: UsageAccount[] = [];
  for (const a of accounts) {
    const sampled = Date.parse(a.sampled_at);
    if (Number.isFinite(sampled) && now - sampled > SAMPLE_MAX_AGE_MS) continue;
    const windows = a.windows.filter((w) => !w.resets_at || Date.parse(w.resets_at) > now);
    const staleAt = a.stale_at ? Date.parse(a.stale_at) : NaN;
    const stale = Boolean(a.stale) || (Number.isFinite(staleAt) && now >= staleAt);
    if (windows.length || isBalanceAccount(a)) out.push({ ...a, windows, ...(stale ? { stale } : {}) });
  }
  return out;
}

/** A prepaid-balance account (#11825): it has a balance, or is a DeepSeek account with no window whose first read
 *  failed. A DeepSeek account that does report windows stays an ordinary bar account. */
export function isBalanceAccount(a: UsageAccount): boolean {
  return a.balance !== undefined || (a.provider === "deepseek" && a.windows.length === 0);
}

/** The used percentage of an account's fullest window. A per-model allowance does not count while the plan's own windows
 *  are there: one model running out is not the account running out. */
export function fullest(a: UsageAccount): number {
  const plan = a.windows.filter((w) => !w.model);
  return (plan.length ? plan : a.windows).reduce((m, w) => Math.max(m, w.used_percent), 0);
}

/** Where an account was seen, for people: the computers that read it by name (an older hub only says "a computer"),
 *  then the gateways'. Never "this computer": the numbers were read on a node, not on the device in hand (#1907). */
export function sourceNames(a: UsageAccount): string[] {
  const machines = a.machines?.length ? a.machines : a.local ? [tr("usage.source_local")] : [];
  return [...machines, ...(a.sources ?? [])];
}

/** The plan to write beside the name, unless the name already says it ("Codex Pro" and plan "pro"). */
export function planNote(a: UsageAccount): string {
  const plan = (a.plan ?? "").trim();
  return plan && !a.name.toLowerCase().includes(plan.toLowerCase()) ? plan : "";
}

export const windowKey = (w: UsageWindow) => `${w.label}:${w.window_minutes}:${w.model ?? ""}`;

/** The accounts the sidebar draws bars for: the two with the fullest window, stale ones last. A balance account has
 *  no bar (it is shown as an amount instead), so it never takes one of these places. */
export function topAccounts(accounts: UsageAccount[], n = 2): UsageAccount[] {
  return accounts
    .filter((a) => !isBalanceAccount(a))
    .sort((a, b) => Number(!!a.stale) - Number(!!b.stale) || fullest(b) - fullest(a))
    .slice(0, n);
}

/** Browser-local storage key for user-pinned sidebar usage account IDs (#12418, display-only). */
export const PINNED_USAGE_ACCOUNTS_KEY = "persona.usage.pinnedAccounts";

const emptyPinned: readonly string[] = [];
let pinnedRaw: string | null | undefined;
let pinnedCache: readonly string[] = emptyPinned;
const pinnedListeners = new Set<() => void>();
let stopPinnedSync: (() => void) | undefined;

function normalizePinnedIds(ids: readonly unknown[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const v of ids) {
    if (typeof v !== "string") continue;
    const id = v.trim();
    if (id && !seen.has(id)) {
      seen.add(id);
      out.push(id);
    }
  }
  return out;
}

/** Parse a stored pinned-account ID list, returning [] on missing or malformed values. */
export function parsePinnedUsageAccounts(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? normalizePinnedIds(parsed) : [];
  } catch {
    return [];
  }
}

function readPinnedRaw(): string | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage.getItem(PINNED_USAGE_ACCOUNTS_KEY);
  } catch {
    return null;
  }
}

function getPinnedSnapshot(): readonly string[] {
  const raw = readPinnedRaw();
  if (raw === pinnedRaw) return pinnedCache;
  pinnedRaw = raw;
  const parsed = parsePinnedUsageAccounts(raw);
  pinnedCache = parsed.length ? parsed : emptyPinned;
  return pinnedCache;
}

function notifyPinned(): void {
  for (const l of pinnedListeners) l();
}

/** Persist the user's chosen sidebar usage account IDs in this browser (display-only). */
function setPinnedUsageAccounts(ids: readonly string[]): readonly string[] {
  const next = normalizePinnedIds(ids);
  const raw = next.length ? JSON.stringify(next) : null;
  let persisted = false;
  try {
    if (typeof localStorage !== "undefined") {
      if (raw) localStorage.setItem(PINNED_USAGE_ACCOUNTS_KEY, raw);
      else localStorage.removeItem(PINNED_USAGE_ACCOUNTS_KEY);
      persisted = true;
    }
  } catch {
    // storage unavailable: keep in-memory state for this tab
  }
  pinnedRaw = persisted ? raw : readPinnedRaw();
  pinnedCache = next.length ? next : emptyPinned;
  notifyPinned();
  return pinnedCache;
}

export function togglePinnedUsageAccount(id: string): readonly string[] {
  const cur = getPinnedSnapshot();
  return setPinnedUsageAccounts(cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]);
}

export function resetPinnedUsageAccounts(): readonly string[] {
  return setPinnedUsageAccounts([]);
}

function subscribePinned(listener: () => void) {
  pinnedListeners.add(listener);
  if (!stopPinnedSync && typeof addEventListener === "function") {
    const onStorage = (e: StorageEvent) => {
      if (e.key === null || e.key === PINNED_USAGE_ACCOUNTS_KEY) notifyPinned();
    };
    addEventListener("storage", onStorage);
    stopPinnedSync = () => removeEventListener("storage", onStorage);
  }
  return () => {
    pinnedListeners.delete(listener);
    if (pinnedListeners.size === 0) {
      stopPinnedSync?.();
      stopPinnedSync = undefined;
    }
  };
}

/** Browser-local pinned account IDs for the collapsed sidebar usage strip. */
export function usePinnedUsageAccounts(): readonly string[] {
  return useSyncExternalStore(subscribePinned, getPinnedSnapshot, getPinnedSnapshot);
}

/** Choose which live accounts appear in the collapsed sidebar strip:
 *  when pinned IDs match at least one live account, show those matched accounts in pinned order;
 *  otherwise rank distinct providers first (fresh before stale, highest fullest first), followed by any remaining
 *  non-balance accounts so wide sidebars fill cleanly without a hardcoded 2-item cap, plus live balance accounts. */
export function selectSidebarUsageAccounts(
  live: UsageAccount[],
  pinnedIds: readonly string[],
): { bars: UsageAccount[]; balances: UsageAccount[]; custom: boolean } {
  if (pinnedIds.length > 0) {
    const byId = new Map(live.map((a) => [a.id, a]));
    const matched: UsageAccount[] = [];
    for (const id of pinnedIds) {
      const a = byId.get(id);
      if (a) matched.push(a);
    }
    if (matched.length > 0) {
      return {
        bars: matched.filter((a) => !isBalanceAccount(a)),
        balances: matched.filter(isBalanceAccount),
        custom: true,
      };
    }
  }
  const ranked = topAccounts(live, live.length);
  const seen = new Set<string>();
  const primary: UsageAccount[] = [];
  const secondary: UsageAccount[] = [];
  for (const account of ranked) {
    const provider = usageProvider(account.provider);
    if (!seen.has(provider)) {
      seen.add(provider);
      primary.push(account);
    } else {
      secondary.push(account);
    }
  }
  return {
    bars: [...primary, ...secondary],
    balances: live.filter(isBalanceAccount),
    custom: false,
  };
}

/** The longest stretch the reset timeline shows: windows resetting later are drawn to its end. */
const TIMELINE_CAP_MS = 7 * 24 * 3600_000;

/** The windows of one account, in the order a card lists them: the plan's own 5h, then 7d, then the other plan windows
 *  by length, and last the per-model allowances (by length too). */
export function sortedWindows(a: Pick<UsageAccount, "windows">): UsageWindow[] {
  const rank = (w: UsageWindow) => {
    const m = windowMinutes(w);
    return (w.model ? 2e6 : 0) + (m === 300 ? 0 : m === 10080 ? 1 : 2 + m);
  };
  return [...a.windows].sort((x, y) => rank(x) - rank(y));
}

/** When an account's plan 7d window resets, in ms; Infinity without one (or without a reset time): those sort last. */
function weeklyResetMs(a: Pick<UsageAccount, "windows">): number {
  const w = a.windows.find((x) => !x.model && windowMinutes(x) === 10080 && x.resets_at);
  const at = w?.resets_at ? Date.parse(w.resets_at) : NaN;
  return Number.isFinite(at) ? at : Infinity;
}

/** A card's accounts: the soonest 7d reset first; accounts without one last, then by name. */
export function sortForCard(accounts: UsageAccount[]): UsageAccount[] {
  return [...accounts].sort((a, b) => {
    const x = weeklyResetMs(a);
    const y = weeklyResetMs(b);
    if (x !== y) return x < y ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
}

export interface ProviderGroup {
  provider: string;
  /** Sorted by `sortForCard`. */
  accounts: UsageAccount[];
  /** The newest `sampled_at` of the group, "" when none reads as a time. */
  sampledAt: string;
}

/** The accounts grouped by provider (account type), groups in the order the hub first listed them. */
export function groupByProvider(accounts: UsageAccount[]): ProviderGroup[] {
  const by = new Map<string, UsageAccount[]>();
  for (const a of accounts) {
    const provider = usageProvider(a.provider);
    by.set(provider, [...(by.get(provider) ?? []), a]);
  }
  return [...by].map(([provider, list]) => {
    const newest = list.reduce((m, a) => Math.max(m, Date.parse(a.sampled_at) || -Infinity), -Infinity);
    return { provider, accounts: sortForCard(list), sampledAt: Number.isFinite(newest) ? new Date(newest).toISOString() : "" };
  });
}

export interface UsageMachineHint {
  id: string;
  name?: string;
  hostname?: string;
  online?: boolean;
}

export type DeviceGroupKind = "machine" | "inferred" | "local" | "global";

export interface DeviceGroup {
  key: string;
  kind: DeviceGroupKind;
  /** Primary display title; empty for "global" so the lazy popover renders its localized label. */
  title: string;
  /** Secondary identifier when distinct from `title` (for example machine ID under a friendly name, or an inferred host tag). */
  subtitle?: string;
  online?: boolean;
  /** Lowercased device identifiers used to suppress redundant per-account source labels. */
  aliases: readonly string[];
  /** Provider blocks on this device, ordered by tightest live usage first. */
  providers: ProviderGroup[];
  accounts: UsageAccount[];
  sampledAt: string;
}

function extractSourceHostTags(sources: readonly string[] | undefined): string[] {
  if (!sources?.length) return [];
  const out: string[] = [];
  for (const src of sources) {
    for (const match of src.matchAll(/\(([^()]+)\)/g)) {
      const tag = match[1]?.trim();
      if (tag && !out.some((x) => x.toLowerCase() === tag.toLowerCase())) out.push(tag);
    }
  }
  return out;
}

function findKnownMachine(raw: string, machines: readonly UsageMachineHint[]): UsageMachineHint | undefined {
  const q = raw.trim().toLowerCase();
  if (!q) return undefined;
  return machines.find(
    (m) =>
      m.id.trim().toLowerCase() === q ||
      Boolean(m.name && m.name.trim().toLowerCase() === q) ||
      Boolean(m.hostname && m.hostname.trim().toLowerCase() === q),
  );
}

export function providerGroupOrder(group: ProviderGroup): number {
  const primary = topAccounts(group.accounts, 1)[0];
  if (primary) return fullest(primary) + (primary.stale ? 0 : 101);
  return group.accounts.some((a) => !a.stale) ? 0.5 : 0;
}

/** Group live accounts into a device-first hierarchy:
 *  1) Explicit `a.machines[0]` matched against known `machines` (`id`, `name`, `hostname`, case-insensitive).
 *  2) Parenthesized host hints in `a.sources` (such as `"Claude (devbox)"` / `"agy (devbox)"`): merged into the exact
 *     explicit machine that shares the same `(tag)` when present, or into a matching known machine, else an inferred host bucket.
 *  3) `"local"` (`usage.source_local`) when `a.local` is true with no machine name, else `"global"` for cloud/gateway accounts. */
export function groupByDevice(accounts: UsageAccount[], machines: readonly UsageMachineHint[] = []): DeviceGroup[] {
  interface Bucket {
    normKey: string;
    key: string;
    kind: DeviceGroupKind;
    title: string;
    subtitle?: string;
    online?: boolean;
    aliases: Set<string>;
    list: UsageAccount[];
    order: number;
  }

  const buckets = new Map<string, Bucket>();
  const tagToNormKey = new Map<string, string>();
  let seq = 0;

  const ensureMachineBucket = (raw: string): Bucket => {
    const m = findKnownMachine(raw, machines);
    const key = m ? m.id.trim() : raw.trim();
    const normKey = `machine:${key.toLowerCase()}`;
    const existing = buckets.get(normKey);
    if (existing) {
      existing.aliases.add(raw.trim().toLowerCase());
      return existing;
    }
    const title = m?.name?.trim() || key;
    let subtitle: string | undefined;
    if (m) {
      if (m.id.trim().toLowerCase() !== title.toLowerCase()) subtitle = m.id.trim();
      else if (m.hostname && m.hostname.trim().toLowerCase() !== title.toLowerCase()) subtitle = m.hostname.trim();
    } else if (raw.trim().toLowerCase() !== title.toLowerCase()) {
      subtitle = raw.trim();
    }
    const aliases = new Set<string>([raw.trim().toLowerCase(), key.toLowerCase(), title.toLowerCase()]);
    if (m?.name?.trim()) aliases.add(m.name.trim().toLowerCase());
    if (m?.hostname?.trim()) aliases.add(m.hostname.trim().toLowerCase());
    if (subtitle) aliases.add(subtitle.toLowerCase());
    const created: Bucket = {
      normKey,
      key,
      kind: "machine",
      title,
      subtitle,
      online: m?.online,
      aliases,
      list: [],
      order: seq++,
    };
    buckets.set(normKey, created);
    return created;
  };

  // Pass 1: discover explicit machine buckets and link any `(host)` tags carried by their sources.
  for (const a of accounts) {
    const primaryMachine = a.machines?.[0]?.trim();
    if (!primaryMachine) continue;
    const b = ensureMachineBucket(primaryMachine);
    for (const tag of extractSourceHostTags(a.sources)) {
      const lower = tag.toLowerCase();
      if (!tagToNormKey.has(lower)) tagToNormKey.set(lower, b.normKey);
      b.aliases.add(lower);
      if (!b.subtitle && lower !== b.title.toLowerCase()) b.subtitle = tag;
    }
  }

  // Pass 2: assign every account to its resolved device bucket.
  for (const a of accounts) {
    const primaryMachine = a.machines?.[0]?.trim();
    if (primaryMachine) {
      ensureMachineBucket(primaryMachine).list.push(a);
      continue;
    }
    const tags = extractSourceHostTags(a.sources);
    if (tags.length > 0) {
      const linkedKey = tags.map((t) => tagToNormKey.get(t.toLowerCase())).find(Boolean);
      if (linkedKey) {
        buckets.get(linkedKey)!.list.push(a);
        continue;
      }
      const matchedTag = tags.find((t) => findKnownMachine(t, machines));
      if (matchedTag) {
        const b = ensureMachineBucket(matchedTag);
        for (const tag of tags) {
          const lower = tag.toLowerCase();
          tagToNormKey.set(lower, b.normKey);
          b.aliases.add(lower);
        }
        b.list.push(a);
        continue;
      }
      const host = tags[0];
      const normKey = `inferred:${host.toLowerCase()}`;
      let b = buckets.get(normKey);
      if (!b) {
        b = {
          normKey,
          key: host,
          kind: "inferred",
          title: host,
          aliases: new Set([host.toLowerCase()]),
          list: [],
          order: seq++,
        };
        buckets.set(normKey, b);
      }
      for (const tag of tags) b.aliases.add(tag.toLowerCase());
      b.list.push(a);
      continue;
    }
    if (a.local) {
      let b = buckets.get("local");
      if (!b) {
        const localTitle = tr("usage.source_local");
        b = {
          normKey: "local",
          key: "local",
          kind: "local",
          title: localTitle,
          aliases: new Set([localTitle.toLowerCase()]),
          list: [],
          order: seq++,
        };
        buckets.set("local", b);
      }
      b.list.push(a);
      continue;
    }
    let b = buckets.get("global");
    if (!b) {
      b = {
        normKey: "global",
        key: "global",
        kind: "global",
        title: "",
        aliases: new Set(),
        list: [],
        order: seq++,
      };
      buckets.set("global", b);
    }
    b.list.push(a);
  }

  return [...buckets.values()]
    .map((b) => {
      const providers = groupByProvider(b.list).sort((x, y) => providerGroupOrder(y) - providerGroupOrder(x));
      const newest = b.list.reduce((m, a) => Math.max(m, Date.parse(a.sampled_at) || -Infinity), -Infinity);
      const topScore = providers.length ? providerGroupOrder(providers[0]) : -1;
      return {
        bucket: b,
        topScore,
        group: {
          key: b.key,
          kind: b.kind,
          title: b.title,
          ...(b.subtitle ? { subtitle: b.subtitle } : {}),
          ...(b.online !== undefined ? { online: b.online } : {}),
          aliases: [...b.aliases],
          providers,
          accounts: providers.flatMap((p) => p.accounts),
          sampledAt: Number.isFinite(newest) ? new Date(newest).toISOString() : "",
        } satisfies DeviceGroup,
      };
    })
    .sort((x, y) => {
      const xGlobal = x.group.kind === "global" ? 1 : 0;
      const yGlobal = y.group.kind === "global" ? 1 : 0;
      if (xGlobal !== yGlobal) return xGlobal - yGlobal;
      if (y.topScore !== x.topScore) return y.topScore - x.topScore;
      return x.bucket.order - y.bucket.order;
    })
    .map((x) => x.group);
}

/** Nodes and external sources can use either spelling for the same usage provider. Account IDs remain separate. */
export const usageProvider = (p: string) => p === "antigravity" ? "agy" : p;
const PROVIDER_TITLES: Record<string, string> = { xai: "xAI", deepseek: "DeepSeek", kimi: "Kimi", agy: "Antigravity" };
/** A provider's name for a card title: its own spelling when known, else the id with a capital. */
export const providerTitle = (p: string) => PROVIDER_TITLES[usageProvider(p)] ?? p.charAt(0).toUpperCase() + p.slice(1);

const HOUR_MS = 3600_000;
/** The shared axis: now, then these hours from now. The first 24 hours take 45% of the width, 1 to 7 days the other 55%. */
export const TIMELINE_TICKS_HOURS = [6, 12, 24, 72, 120, 168] as const;
/** Where `ms` from now falls along the axis, 0-1 (clamped at 7 days). */
export function timelinePosition(ms: number): number {
  const h = Math.max(0, Math.min(ms, TIMELINE_CAP_MS)) / HOUR_MS;
  return h <= 24 ? (h / 24) * 0.45 : 0.45 + ((h - 24) / 144) * 0.55;
}

interface TimelineLane {
  key: string;
  label: string;
  /** A window longer than six hours (the weekly one): the blue capsule; a short one is the teal one. */
  long: boolean;
  /** Where the capsule ends along the axis, 0-1. */
  end: number;
  /** Time left, in ms (the end may be beyond the axis; `end` is then 1). */
  remainingMs: number;
  resetsAt: string;
}
export interface TimelineRow {
  account: UsageAccount;
  /** The plan windows with a reset time still ahead, 5h first. */
  lanes: TimelineLane[];
}

/** The rows of one provider card's reset timeline: one per account, in the order given (the card's quota order), each
 *  with a lane per plan window that has a reset time. Accounts with no lane have no row. Pure client computation. */
export function providerTimeline(accounts: UsageAccount[], now = Date.now()): TimelineRow[] {
  const rows: TimelineRow[] = [];
  for (const account of accounts) {
    const lanes: TimelineLane[] = [];
    for (const w of sortedWindows(account)) {
      const at = w.resets_at ? Date.parse(w.resets_at) : NaN;
      if (w.model || !Number.isFinite(at) || at <= now) continue;
      lanes.push({ key: windowKey(w), label: w.label, long: windowMinutes(w) > 360, end: timelinePosition(at - now), remainingMs: at - now, resetsAt: w.resets_at as string });
    }
    if (lanes.length) rows.push({ account, lanes });
  }
  return rows;
}

/** Time left as a short label: "45m", "1.5h", "2d 3h" (each language's units). */
export function remainingText(ms: number): string {
  const mins = Math.ceil(ms / 60_000);
  if (mins < 60) return tr("usage.minutes", { m: Math.max(1, mins) });
  const hours = ms / HOUR_MS;
  if (hours < 24) return tr("usage.hours", { h: Math.round(hours * 10) / 10 });
  const d = Math.floor(hours / 24);
  const h = Math.round(hours - d * 24);
  return h === 0 ? tr("usage.days", { d }) : tr("usage.days_hours", { d, h });
}

export const isWarn = (percent: number) => percent >= WARN_PERCENT;
export const isCritical = (percent: number) => percent >= CRITICAL_PERCENT;

export interface UnconnectedProvider {
  id: string;
  title: string;
  agent: string;
}

const emptyAccounts: UsageAccount[] = [];
const noProviders: readonly string[] = [];
let usageAccounts = emptyAccounts;
/** Providers the owner hid from the bars (`usage.hidden_providers`, #13179), as the hub last said; none from an old hub. */
let usageHidden = noProviders;
let usageReady = false;
let stopPolling: (() => void) | undefined;
let refreshPolling: (() => Promise<boolean>) | undefined;
const listeners = new Set<() => void>();

function publishUsage(accounts: UsageAccount[], ready = usageReady, hidden = usageHidden) {
  usageAccounts = accounts;
  usageHidden = hidden;
  usageReady = ready;
  for (const listener of listeners) listener();
}

function pollUsage() {
  let dead = false;
  // The tab coming back to the front can overlap the poll. Only the newest reply counts.
  let seq = 0;
  let manual: Promise<boolean> | undefined;
  const load = async () => {
    const mine = ++seq;
    const current = () => !dead && mine === seq;
    try {
      const r = await api<{ accounts?: UsageAccount[]; hidden_providers?: string[] }>("/usage/windows");
      if (!current()) return false;
      publishUsage(r.accounts ?? emptyAccounts, true, r.hidden_providers?.length ? r.hidden_providers.map((p) => p.toLowerCase()) : noProviders);
      return true;
    } catch (e: unknown) {
      if (current()) {
        const missing = e instanceof ApiError && e.status === 404;
        publishUsage(missing ? emptyAccounts : usageAccounts.slice(), missing ? false : usageReady, missing ? noProviders : usageHidden);
      }
      return false;
    }
  };
  const background = () => {
    if (document.visibilityState === "visible" && !manual) void load();
  };
  refreshPolling = () => {
    manual ??= load().finally(() => { manual = undefined; });
    return manual;
  };
  background();
  const timer = setInterval(background, POLL_MS);
  document.addEventListener("visibilitychange", background);
  return () => {
    dead = true;
    refreshPolling = undefined;
    clearInterval(timer);
    document.removeEventListener("visibilitychange", background);
  };
}

function subscribeUsage(listener: () => void) {
  listeners.add(listener);
  stopPolling ??= pollUsage();
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      stopPolling?.();
      stopPolling = undefined;
      usageAccounts = emptyAccounts;
      usageHidden = noProviders;
      usageReady = false;
    }
  };
}

/** The sidebar and Agent status lines share one poll, including while switching chats. */
export function useUsageWindows(): UsageAccount[] {
  return useSyncExternalStore(subscribeUsage, () => usageAccounts, () => emptyAccounts);
}

/** Re-read the hub's latest quota snapshots using the same store and ordering as the background poll. */
export function refreshUsageWindows(): Promise<boolean> {
  return refreshPolling?.() ?? Promise.resolve(false);
}

/** The providers the owner hid from the usage bars (lowercase), so nothing else in the strip lists them either (#13179). */
export function useHiddenUsageProviders(): readonly string[] {
  return useSyncExternalStore(subscribeUsage, () => usageHidden, () => noProviders);
}

/** True once GET /api/v1/usage/windows has completed a supported (non-404) response. */
export function useUsageWindowsReady(): boolean {
  return useSyncExternalStore(subscribeUsage, () => usageReady, () => false);
}
