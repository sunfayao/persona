// @vitest-environment jsdom
// A leader conversation's direction name (lib/direction-names.ts) is taken from the one tree lib/direction-tree.ts reads
// (#4352): a page header and a screen drawing the tree cost one GET, and a change read for one shows in the other.
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { isExternalDirection, readDirectionTree, resetDirectionNames, useDirectionName } from "./direction-names";
import { useDirectionTree } from "./direction-tree";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const tree = (name: string) => ({
  directions: [
    { id: "lead-a", name, counts: { working: 1, needs_you: 1, stuck: 0, review: 0 }, running: true, tasks: [] },
    { id: "direction-outside", name: "Outside", external: true, machine_id: "mac", session_id: "agent-7", counts: { working: 0, needs_you: 0, stuck: 0, review: 0 }, running: false, tasks: [] },
    { id: "main", name: "", secretary: true, counts: { working: 9, needs_you: 9, stuck: 0, review: 0 }, tasks: [] },
  ],
});

let seen: { name?: string; tree?: unknown } = {};
function Probe() {
  seen = { name: useDirectionName("lead-a"), tree: useDirectionTree() };
  return null;
}

afterEach(() => {
  resetDirectionNames();
  vi.unstubAllGlobals();
});

describe("a direction's name", () => {
  it("comes from the one direction tree: one GET for every screen, and follows its reads", async () => {
    let name = "Product";
    const fetch = vi.fn(async (url: string) => new Response(JSON.stringify(url.includes("/directions/tree") ? tree(name) : {}), { headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetch);
    const host = document.createElement("div");
    const root = createRoot(host);
    act(() => root.render(<><Probe /><Probe /></>));
    await act(async () => { await readDirectionTree(); });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(seen.name).toBe("Product");
    expect(isExternalDirection("direction-outside")).toBe(true);
    expect(isExternalDirection("lead-a")).toBe(false);
    expect((seen.tree as { directions: unknown[] }).directions).toHaveLength(3);
    // a directions push: one more GET, and the name follows
    name = "Growth";
    await act(async () => { await readDirectionTree(true); });
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(seen.name).toBe("Growth");
    act(() => root.unmount());
  });
});
