import { describe, expect, it } from "vitest";
import { foldsBackgroundRounds } from "./background-fold";
import type { AssistantInfo } from "./types";

const assistant = (over: Partial<AssistantInfo> = {}): AssistantInfo =>
  ({ machine_id: "m1", conversations: [], ...over }) as AssistantInfo;

describe("foldsBackgroundRounds", () => {
  it("folds in the main conversation while the switch is on", () => {
    expect(foldsBackgroundRounds("main", assistant({ main: "main", main_background_folded: true }))).toBe(true);
  });

  it("never folds another conversation", () => {
    const info = assistant({ main: "main", main_background_folded: true });
    expect(foldsBackgroundRounds("side", info)).toBe(false);
  });

  it("the switch off disables folding in the main conversation", () => {
    const info = assistant({ main: "main", main_background_folded: false });
    expect(foldsBackgroundRounds("main", info)).toBe(false);
  });

  it("a hub that omits the switch means on, and one that sends no assistant means off", () => {
    expect(foldsBackgroundRounds("main", assistant({ main: "main" }))).toBe(true);
    expect(foldsBackgroundRounds("main", undefined)).toBe(false);
  });
});
