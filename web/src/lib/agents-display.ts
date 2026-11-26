// One acknowledged display choice for the paired hub's default manager. Inventory
// and to-do attribution stay unchanged. The old local key is migration input only.
import { useSyncExternalStore } from "react";
import { api, credentialOwner, onCredentialChange } from "./api";

const LEGACY = "persona.agentHideBotmux";
const PATH = "/agents/display-settings";
interface AgentsDisplay { show_botmux: boolean; configured: boolean; use_herdr_tab_name?: boolean }
let value: AgentsDisplay | null = null;
let revision = 0;
let reading: Promise<void> | undefined;
const listeners = new Set<() => void>();
const subscribe = (f: () => void) => { listeners.add(f); return () => { listeners.delete(f); }; };
const set = (next: AgentsDisplay | null) => { value = next; listeners.forEach((f) => f()); };
function checked(value: AgentsDisplay): AgentsDisplay {
  if (typeof value.show_botmux !== "boolean" || typeof value.configured !== "boolean" ||
    (value.use_herdr_tab_name !== undefined && typeof value.use_herdr_tab_name !== "boolean")) throw new Error("Invalid Agents display settings");
  return value;
}
function legacyHidden() { try { return localStorage.getItem(LEGACY) === "1"; } catch { return false; } }
function forgetLegacy() { try { localStorage.removeItem(LEGACY); } catch { /* a later read may retry */ } }

/** Called on hello and each Agents list refresh. Stale reads cannot overwrite the value for a new credential. */
export function refreshAgentsDisplay(): Promise<void> {
  credentialOwner();
  if (reading) return reading;
  const mine = revision;
  const work = (async () => {
    try {
      let value = checked(await api<AgentsDisplay>(PATH));
      if (mine !== revision) return;
      if (!value.configured && legacyHidden()) {
        value = checked(await api<AgentsDisplay>(PATH, { method: "PUT", json: { show_botmux: false, only_if_unset: true } }));
      }
      if (mine !== revision) return;
      if (value.configured) forgetLegacy();
      set(value);
    } catch {
      // Keep the last acknowledged value when a refresh fails.
    }
  })();
  reading = work;
  void work.finally(() => { if (reading === work) reading = undefined; });
  return work;
}

export function resetAgentsDisplay() {
  revision++;
  reading = undefined;
  set(null);
}
onCredentialChange(resetAgentsDisplay);


// A session-list refresh also refreshes display settings. Rows subscribe to
// the choice they use so a fresh answer or error with the same choice does not
// redraw them.
const botmuxHidden = () => value?.show_botmux !== true;
const herdrTabName = () => value?.use_herdr_tab_name === true;

export function useBotmuxHidden(): boolean {
  // An unavailable preference might hide these sessions. Keep the last acknowledged choice.
  return useSyncExternalStore(subscribe, botmuxHidden, botmuxHidden);
}

export function useHerdrTabName(): boolean {
  return useSyncExternalStore(subscribe, herdrTabName, herdrTabName);
}
