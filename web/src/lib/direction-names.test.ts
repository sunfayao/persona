import { describe, expect, it } from "vitest";
import { countsLabel, directionNames, leaderExternalDirections, treeLeaders } from "./direction-names";

describe("direction names", () => {
  it("keep the current leaders' names by id, leaving out ended ones and blank names", () => {
    const names = directionNames([
      { id: "a", name: "Product" },
      { id: "b", name: "  " },
      { id: "c", name: "Old", ended: true },
      { id: "", name: "Nobody" },
    ]);
    expect([...names]).toEqual([["a", "Product"]]);
    expect(directionNames(undefined).size).toBe(0);
  });

  it("preserve external metadata in tree and leaders-list projections, keyed by direction id", () => {
    const list = [
      { id: "direction-outside", name: "Outside", external: true, machine_id: "mac", session_id: "agent-7" },
      { id: "direction-targetless", name: "Targetless", external: true },
      { id: "direction-ended", name: "Ended", external: true, machine_id: "mac", session_id: "old", ended: true },
      { id: "lead-a", name: "Product", machine_id: "hub", session_id: "lead-a" },
    ];
    expect([...leaderExternalDirections(list)]).toEqual([
      ["direction-outside", { external: true, machine_id: "mac", session_id: "agent-7" }],
      ["direction-targetless", { external: true }],
    ]);
    expect(treeLeaders({ directions: [
      { id: "direction-outside", name: "Outside", external: true, machine_id: "mac", session_id: "agent-7", counts: { working: 1, needs_you: 2, stuck: 0, review: 0 }, running: true },
      { id: "main", name: "", secretary: true, counts: { working: 0, needs_you: 0, stuck: 0, review: 0 }, running: false },
    ] })).toEqual([{ id: "direction-outside", name: "Outside", external: true, machine_id: "mac", session_id: "agent-7", counts: { running: true, needs_you: 2 } }]);
  });
});

describe("what a direction shows of its tasks (#4331, #6626)", () => {
  const tt = (key: string, o: { count: number }) => `${key === "sidebar.leader_needs_you" ? "等你" : key} ${o.count}`;
  it("says only what waits on you: what runs is the mark, never words as well", () => {
    expect(countsLabel({ needs_you: 3 }, tt)).toBe("等你 3");
    expect(countsLabel({ needs_you: 0 }, tt)).toBe("");
    expect(countsLabel(undefined, tt)).toBe("");
  });
});
