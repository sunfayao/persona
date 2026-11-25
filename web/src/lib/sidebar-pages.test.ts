// The sidebar's pages: the built-in ones listed in order, the first extension pages after them, the rest under More, and
// the groups extensions name.
import { describe, expect, it } from "vitest";
import { arrangeSections, EXTENSION_CAP } from "./sidebar-pages";

const keys = (ps: { k: string }[]) => ps.map((p) => p.k);

describe("arrangeSections", () => {
  it("lists the built-in pages in order and puts the extension pages past the cap under More", () => {
    const ext = Array.from({ length: EXTENSION_CAP + 2 }, (_, i) => ({ k: `p:e${i}/page` }));
    const { sections, more } = arrangeSections([{ k: "home" }, { k: "inbox" }, ...ext, { k: "settings" }]);
    expect(sections).toHaveLength(1);
    expect(keys(sections[0].pages)).toEqual(["home", "inbox", ...keys(ext.slice(0, EXTENSION_CAP)), "settings"]);
    expect(keys(more)).toEqual(keys(ext.slice(EXTENSION_CAP)));
  });

  it("draws the groups extensions name after Pages, in the order first named, and keeps Settings in Pages", () => {
    const g = (id: string) => ({ id, title: id.toUpperCase() });
    const { sections, more } = arrangeSections([
      { k: "home" },
      { k: "p:a/x", group: g("b") },
      { k: "p:b/x", group: g("a") },
      { k: "p:c/x", group: g("b") },
      { k: "settings", group: g("a") },
    ]);
    expect(sections.map((s) => [s.id, s.title, keys(s.pages)])).toEqual([
      ["", undefined, ["home", "settings"]],
      ["b", "B", ["p:a/x", "p:c/x"]],
      ["a", "A", ["p:b/x"]],
    ]);
    expect(more).toEqual([]);
  });
});
