// Where Settings is: which category the URL opens, and the list of them a phone shows first. Wide, the categories are in
// the left sidebar and /settings is "General"; phone width, /settings is the list (as on iOS), a category is
// /settings?tab=… pushed over it, and Back goes back to the list. A link to a category that no longer exists opens the
// Settings root.
import { t } from "../../i18n";
import { settingsTab } from "../../lib/channels";
import type { IconName } from "../../ui/icons";
import { SUB } from "./href";

export { settingsHref } from "./href";

export type Tab = "general" | "notifications" | "models" | "devices";
export const TABS: Tab[] = ["general", "notifications", "models", "devices"];
export type SettingsSection = "providers" | "model-config";

// Links from before the categories were combined keep their extra provider parameters. A fragment is the legacy
// category's destination within its new page, so a shared link still lands on the same section.
const LEGACY_TABS: Record<string, { tab: Tab; section: SettingsSection | null }> = {
  "model-config": { tab: "models", section: "model-config" },
};

// `hint`: one short line under the category on the phone's list, so what it holds is clear without opening it.
export const TAB_INFO: Record<Tab, { label: string; icon: IconName; hint: string }> = {
  general: { label: t("settings-nav.tab_general"), icon: "gear", hint: t("settings-nav.hint_general") },
  notifications: { label: t("settings-nav.tab_notifications"), icon: "bell", hint: t("settings-nav.hint_notifications") },
  models: { label: t("settings-nav.tab_models"), icon: "layers", hint: t("settings-nav.hint_models") },
  devices: { label: t("settings-nav.tab_devices"), icon: "key", hint: t("settings-nav.hint_devices") },
};

/** The current assistant's pages: they read and write that assistant only (its model providers and configuration), and
 *  an item opened in one goes back to its list when the pick changes. */
export const ASSISTANT_TABS: ReadonlySet<Tab> = new Set<Tab>(["models"]);

/** The category and item a URL's query names; `tab` is null when it names none (the phone's list). */
export function settingsAt(search: string, hash = ""): { tab: Tab | null; sub: string | null; section: SettingsSection | null } {
  const q = new URLSearchParams(search);
  const old = settingsTab(q.get("tab"));
  const legacy = old ? LEGACY_TABS[old] : undefined;
  const t = (legacy?.tab ?? old) as Tab;
  if (!TABS.includes(t)) return { tab: null, sub: null, section: null };
  const param = SUB[t];
  const sub = param ? q.get(param) : null;
  const section = legacy?.section ?? sectionOfHash(hash, t);
  return { tab: t, sub, section };
}

/** Replace a legacy category URL in place, retaining provider parameters. */
export function redirectLegacySettings(search: string, hash: string): string | null {
  const q = new URLSearchParams(search);
  const old = settingsTab(q.get("tab"));
  if (!old || !LEGACY_TABS[old]) return null;
  const at = settingsAt(q.toString(), hash);
  if (!at.tab) return null;
  q.set("tab", at.tab);
  return `/settings?${q.toString()}${at.section ? `#settings-${at.section}` : ""}`;
}

function sectionOfHash(hash: string, tab: Tab): SettingsSection | null {
  const section = hash.startsWith("#settings-") ? hash.slice("#settings-".length) : "";
  if (tab === "models" && (section === "providers" || section === "model-config")) return section;
  return null;
}

/** The history state of a category opened from the phone's list: Back from it returns to the list. Its value is how many
 *  entries back the list is. */
export const FROM_LIST = "personaSettingsList";

const marker = (state: unknown, key: string): unknown => (state && typeof state === "object" ? (state as Record<string, unknown>)[key] : undefined);

export function openedFromList(state: unknown): boolean {
  return Boolean(marker(state, FROM_LIST));
}

/** How many entries back the phone's list is from this one; 0: it is not under it. */
export function stepsToList(state: unknown): number {
  const n = marker(state, FROM_LIST);
  return typeof n === "number" && n > 0 ? n : n ? 1 : 0;
}

/** The state an entry keeps when another category replaces it: its way back to the list. */
export function categoryState(state: unknown): Record<string, unknown> | null {
  return openedFromList(state) ? { ...(state as Record<string, unknown>) } : null;
}
