import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t } from "../../i18n";
import { NoOtherLooks } from "./Persona";

// The avatar picker with no looks beyond the built-in ones (#3597): it says so, never a file in the repository nor a path on
// the hub machine.
describe("the picker with no other looks", () => {
  const html = renderToStaticMarkup(<NoOtherLooks />);

  it("says there are none", () => {
    expect(html).toContain(t("assistant-persona.none"));
    expect(html).not.toContain("~/.");
  });

  it("names no repository file", () => {
    expect(html).not.toMatch(/docs\/|\.md\b/);
  });
});
