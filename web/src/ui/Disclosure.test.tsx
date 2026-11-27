// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Disclosure, DisclosurePanel, DisclosureTrigger } from "./Disclosure";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let originalGetAnimations: typeof Element.prototype.getAnimations | undefined;

beforeEach(() => {
  originalGetAnimations = Element.prototype.getAnimations;
  if (typeof Element.prototype.getAnimations !== "function") {
    Element.prototype.getAnimations = () => [];
  }
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  if (originalGetAnimations === undefined) {
    delete (Element.prototype as { getAnimations?: unknown }).getAnimations;
  } else {
    Element.prototype.getAnimations = originalGetAnimations;
  }
});

describe("Disclosure (@ui-sdk/react/collapsible)", () => {
  it("toggles uncontrolled open/close with aria-expanded, data-open/data-closed, and collapsible-panel slot", () => {
    act(() =>
      root.render(
        <Disclosure className="disc-root">
          <DisclosureTrigger className="disc-btn">Show details</DisclosureTrigger>
          <DisclosurePanel className="disc-panel">
            <p>Expanded body</p>
          </DisclosurePanel>
        </Disclosure>,
      ),
    );

    const rootEl = host.querySelector('[data-slot="collapsible"]')!;
    const trigger = host.querySelector("button")!;
    expect(rootEl.className).toContain("disc-root");
    expect(rootEl.hasAttribute("data-closed")).toBe(true);
    expect(rootEl.hasAttribute("data-open")).toBe(false);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(trigger.hasAttribute("data-closed")).toBe(true);
    expect(trigger.hasAttribute("data-open")).toBe(false);
    expect(host.querySelector('[data-slot="collapsible-panel"]')).toBeNull();

    act(() => trigger.click());

    const panel = host.querySelector('[data-slot="collapsible-panel"]')!;
    expect(panel).not.toBeNull();
    expect(panel.className).toContain("disc-panel");
    expect(panel.textContent).toBe("Expanded body");
    expect(panel.hasAttribute("data-open")).toBe(true);
    expect(panel.hasAttribute("data-closed")).toBe(false);
    expect(rootEl.hasAttribute("data-open")).toBe(true);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(trigger.getAttribute("aria-controls")).toBe(panel.id);
    expect(trigger.hasAttribute("data-open")).toBe(true);
    expect(trigger.hasAttribute("data-closed")).toBe(false);

    act(() => trigger.click());

    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(trigger.hasAttribute("data-closed")).toBe(true);
    expect(trigger.hasAttribute("data-open")).toBe(false);
    expect(rootEl.hasAttribute("data-closed")).toBe(true);
    expect(host.querySelector('[data-slot="collapsible-panel"]')).toBeNull();
  });

  it("supports controlled open and onOpenChange", () => {
    const onChange = vi.fn();
    function Controlled() {
      const [open, setOpen] = useState(false);
      return (
        <Disclosure
          open={open}
          onOpenChange={(next) => {
            onChange(next);
            setOpen(next);
          }}
        >
          <DisclosureTrigger>More files</DisclosureTrigger>
          <DisclosurePanel>
            <span>file-a.ts</span>
          </DisclosurePanel>
        </Disclosure>
      );
    }

    act(() => root.render(<Controlled />));
    const trigger = host.querySelector("button")!;
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(host.querySelector('[data-slot="collapsible-panel"]')).toBeNull();

    act(() => trigger.click());
    expect(onChange).toHaveBeenLastCalledWith(true);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(host.querySelector('[data-slot="collapsible-panel"]')?.textContent).toBe("file-a.ts");

    act(() => trigger.click());
    expect(onChange).toHaveBeenLastCalledWith(false);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(host.querySelector('[data-slot="collapsible-panel"]')).toBeNull();
  });

  it("supports defaultOpen, disabled, and keepMounted", () => {
    act(() =>
      root.render(
        <Disclosure defaultOpen>
          <DisclosureTrigger>Open by default</DisclosureTrigger>
          <DisclosurePanel>Visible initially</DisclosurePanel>
        </Disclosure>,
      ),
    );
    expect(host.querySelector("button")!.getAttribute("aria-expanded")).toBe("true");
    expect(host.querySelector('[data-slot="collapsible-panel"]')?.textContent).toBe("Visible initially");

    act(() =>
      root.render(
        <Disclosure key="locked" disabled>
          <DisclosureTrigger>Locked</DisclosureTrigger>
          <DisclosurePanel keepMounted>Kept hidden</DisclosurePanel>
        </Disclosure>,
      ),
    );
    const trigger = host.querySelector("button")!;
    const panel = host.querySelector('[data-slot="collapsible-panel"]')!;
    expect(panel).not.toBeNull();
    expect(panel.hasAttribute("hidden")).toBe(true);
    expect(panel.hasAttribute("data-closed")).toBe(true);
    expect(trigger.getAttribute("aria-disabled")).toBe("true");
    act(() => trigger.click());
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(panel.hasAttribute("hidden")).toBe(true);
  });
});
