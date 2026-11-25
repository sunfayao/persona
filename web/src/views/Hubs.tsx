// Going between hubs (#1014, lib/hubs.ts): the other hubs in the account menu and on the sign-in
// page. Switching opens the other hub's own page; with no other hub in the list, the menu and the sign-in page look as
// before.
import { useEffect, useState } from "react";
import { here, hostOf, hubTitle, loadHubs, onHubsChange, switchURL, type SavedHub } from "../lib/hubs";
import { type MenuItem } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { t } from "../i18n";

export function useHubs(): SavedHub[] {
  const [list, setList] = useState(loadHubs);
  useEffect(() => {
    const sync = () => setList(loadHubs());
    // another tab of this hub's page changed it
    const storage = (e: StorageEvent) => e.key === "persona.hubs" && sync();
    addEventListener("storage", storage);
    const off = onHubsChange(sync);
    return () => {
      off();
      removeEventListener("storage", storage);
    };
  }, []);
  return list;
}

/** Open another hub's page, handing it the list. */
function goToHub(h: SavedHub) {
  location.href = switchURL(h.url, loadHubs());
}

/** The account menu's rows for the other hubs (none when there is no other one). */
export function hubMenuItems(list: SavedHub[]): MenuItem[] {
  return list
    .filter((h) => h.url !== here())
    .map((h, i) => ({ key: `hub:${h.url}`, label: hubTitle(h), hint: t("hubs.switch"), icon: "hub" as const, divider: i === 0, onSelect: () => goToHub(h) }));
}





// ── Sign-in page ───────────────────────────────────────────

/** On the sign-in page: the other hubs, so a hub not signed in on yet is not a dead end. */
export function OtherHubs() {
  const others = useHubs().filter((h) => h.url !== here());
  if (others.length === 0) return null;
  return (
    <div className="mt-6">
      <div className="mb-1.5 px-1 text-[11.5px] font-medium text-ink-3">{t("hubs.other_hubs")}</div>
      <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
        {others.map((h) => (
          <button key={h.url} type="button" onClick={() => goToHub(h)} className="flex w-full items-center gap-2.5 whitespace-nowrap px-3 py-2.5 text-left transition-colors hover:bg-hover">
            <Icon name="hub" size={14} className="shrink-0 text-ink-3" />
            <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{hubTitle(h)}</span>
            <span className="shrink-0 truncate font-mono text-[11px] text-ink-3">{hostOf(h.url)}</span>
            <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
          </button>
        ))}
      </div>
    </div>
  );
}
