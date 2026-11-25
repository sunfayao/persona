// @vitest-environment node
// touch-hit's square on a touch screen (#11511): a button that is not positioned takes its row's box for the square, so
// the whole row answers a tap. jsdom does not hit-test; scripts/test/web_mobile_shell_e2e.py (agents_list_phone) taps the
// rows in Chrome.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

describe("touch-hit's square", () => {
  it("is the box it sits in once that box is 44 px or more each way: so an unpositioned button's is the row", () => {
    const css = readFileSync(join(__dirname, "../styles.css"), "utf8");
    const rule = css.match(/@utility touch-hit \{([\s\S]*?)\n\}/)?.[1] ?? "";
    for (const side of ["top", "bottom", "left", "right"]) expect(rule).toContain(`${side}: min(0px, calc(50% - 22px));`);
  });
});
