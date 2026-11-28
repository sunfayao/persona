import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Switch } from "./atoms";
import { Switch as AssistantSwitch } from "../views/assistant/parts";

const css = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const knobShadows = [...css.matchAll(/--shadow-knob:\s*([^;]+);/g)].map((m) => m[1]!.trim()).filter((v) => !v.startsWith("var("));

describe("Switch (#3675)", () => {
  it("draws the knob with the knob shadow, never the button's ring", () => {
    for (const on of [true, false]) {
      const html = renderToStaticMarkup(<Switch on={on} label="x" onChange={() => {}} />);
      expect(html).toContain("shadow-knob");
      expect(html).not.toContain("shadow-btn");
      expect(html).toContain(on ? "bg-switch-on" : "bg-switch-off");
      expect(html).toMatch(/class="switch /);
    }
  });

  it("has a knob shadow without a spread ring in light and dark", () => {
    expect(knobShadows).toHaveLength(2);
    // a ring is a shadow with no offset and no blur, like `0 0 0 1px`
    for (const v of knobShadows) expect(v).not.toMatch(/(^|,)\s*0 0 0 /);
  });

  it("is the same component on the assistant pages", () => {
    const a = renderToStaticMarkup(<AssistantSwitch checked label="x" onChange={() => {}} />);
    const b = renderToStaticMarkup(<Switch on label="x" title="x" onChange={() => {}} />);
    expect(a).toContain(b);
  });

  it("rounds its keyboard focus ring to the pill and keeps it clear of the track", () => {
    // the ring follows the switch's own radius: the focus rule's 6 px is a base-layer default that `rounded-full` beats
    expect(renderToStaticMarkup(<Switch on label="x" title="x" onChange={() => {}} />)).toMatch(/class="switch [^"]*\brounded-full\b/);
    expect(css).toMatch(/@layer base \{ :is\(button, a, summary\):focus-visible \{ border-radius: 6px; \} \}/);
    expect(css).toMatch(/\.switch \{ --focus-ring-offset: 2px; \}/);
  });
});
