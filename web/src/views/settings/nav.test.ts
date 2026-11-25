import { describe, expect, it } from "vitest";
import { t } from "../../i18n";
import { ASSISTANT_TABS, categoryState, FROM_LIST, openedFromList, redirectLegacySettings, settingsAt, settingsHref, stepsToList, TAB_INFO, TABS } from "./nav";

describe("settings location", () => {
  it("names no category on plain /settings: the phone's list (通用 on a wide screen)", () => {
    expect(settingsAt("")).toEqual({ tab: null, sub: null, section: null });
    expect(settingsAt("?tab=nope")).toEqual({ tab: null, sub: null, section: null });
  });
  it("opens the root for a link to a category that was removed", () => {
    for (const tab of ["persona", "agents", "hub", "channels", "connectors", "extensions", "plugins", "knowledge", "labs", "prompts", "shortcuts", "sources", "managers", "avatar", "claude", "translate", "assistant", "sent"]) {
      expect(settingsAt(`?tab=${tab}`), tab).toEqual({ tab: null, sub: null, section: null });
      expect(redirectLegacySettings(`?tab=${tab}`, ""), tab).toBeNull();
    }
  });
  it("reads the category and the purpose its page has open", () => {
    expect(settingsAt("?tab=models&purpose=agent.triage", "#settings-model-config")).toEqual({ tab: "models", sub: "agent.triage", section: "model-config" });
    expect(settingsAt("?tab=devices&purpose=x")).toEqual({ tab: "devices", sub: null, section: null });
  });
  it("names 通用 on a phone, where /settings is the list, and keeps /settings for it on a wide screen", () => {
    expect(settingsHref("general", null, false)).toBe("/settings");
    expect(settingsHref("general", null, true)).toBe("/settings?tab=general");
    expect(settingsHref("models", "agent.new", false)).toBe("/settings?tab=models&purpose=agent.new");
    expect(settingsHref("devices", "ignored", true)).toBe("/settings?tab=devices");
  });
  it("preserves an explicit assistant owner in configuration links", () => {
    expect(settingsHref("models", "agent.new", false, "work", "model-config")).toBe("/settings?tab=models&purpose=agent.new&manager=work#settings-model-config");
  });
  it("round-trips every category through its URL", () => {
    for (const t of TABS) expect(settingsAt(settingsHref(t, null, true).split("?")[1] ?? "").tab).toBe(t);
  });
  it("knows a category was opened from the list (Back goes to it)", () => {
    expect(openedFromList({ [FROM_LIST]: true, personaOverlay: "o1" })).toBe(true);
    expect(openedFromList(null)).toBe(false);
    expect(openedFromList({ personaOverlay: "o1" })).toBe(false);
    expect(stepsToList({ [FROM_LIST]: 2 })).toBe(2);
    expect(stepsToList(null)).toBe(0);
    expect(categoryState({ [FROM_LIST]: 2 })).toEqual({ [FROM_LIST]: 2 });
    expect(categoryState(null)).toBeNull();
  });
  it("redirects the old model configuration category to its section and preserves its destination", () => {
    expect(redirectLegacySettings("?tab=model-config&purpose=agent.triage&manager=work", "")).toBe("/settings?tab=models&purpose=agent.triage&manager=work#settings-model-config");
    expect(redirectLegacySettings("?tab=models&launcher=example&add=1", "")).toBeNull();
  });
  it("keeps only Models among the current assistant's pages", () => {
    expect([...ASSISTANT_TABS]).toEqual(["models"]);
  });
  it("gives every category a one-line hint and a mark of its own", () => {
    for (const k of TABS) expect(TAB_INFO[k].hint.trim()).not.toBe("");
    expect(new Set(TABS.map((k) => TAB_INFO[k].icon)).size).toBe(TABS.length);
    expect(TAB_INFO.models.label).toBe(t("settings-nav.tab_models"));
  });
});
