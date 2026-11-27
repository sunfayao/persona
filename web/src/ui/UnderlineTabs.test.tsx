// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
});

import {
  FloatingSegment,
  UnderlineTabs,
  type FloatingSegmentItem,
  type UnderlineTabItem,
} from "./UnderlineTabs";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const ITEMS: readonly UnderlineTabItem<"all" | "working" | "needs">[] = [
  { key: "all", label: "All", dataAttrs: { "data-usage-filter": "all" } },
  {
    key: "working",
    label: "Working",
    count: 3,
    emphasized: true,
    ariaLabel: "Working 3",
    dataAttrs: { "data-usage-filter": "working" },
  },
  { key: "needs", label: "Needs you", count: 0, dataAttrs: { "data-usage-filter": "needs" } },
];

const SEGMENT_ITEMS: readonly FloatingSegmentItem<"detailed" | "compact">[] = [
  { key: "detailed", label: "详细", dataAttrs: { "data-mode": "detailed" } },
  { key: "compact", label: "紧凑", dataAttrs: { "data-mode": "compact" } },
];

function ControlledUnderline({ onChange }: { onChange?: (k: "all" | "working" | "needs") => void }) {
  const [value, setValue] = useState<"all" | "working" | "needs">("all");
  return (
    <UnderlineTabs
      items={ITEMS}
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange?.(next);
      }}
      ariaLabel="Quick filters"
    />
  );
}

function ControlledSegment({ onChange }: { onChange?: (k: "detailed" | "compact") => void }) {
  const [value, setValue] = useState<"detailed" | "compact">("detailed");
  return (
    <FloatingSegment
      items={SEGMENT_ITEMS}
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange?.(next);
      }}
      ariaLabel="Display mode"
    />
  );
}

describe("UnderlineTabs", () => {
  it("renders radiogroup + radio semantics, indicator slot, forwards dataAttrs, and selects on click", () => {
    const onChange = vi.fn();
    act(() => {
      root.render(<UnderlineTabs items={ITEMS} value="all" onChange={onChange} ariaLabel="Quick filters" />);
    });

    const container = host.querySelector<HTMLElement>('[role="radiogroup"]');
    expect(container).not.toBeNull();
    expect(container?.getAttribute("aria-label")).toBe("Quick filters");
    expect(container?.getAttribute("data-variant")).toBe("underline");

    const indicator = host.querySelector<HTMLElement>('[data-slot="segmented-control-indicator"]');
    expect(indicator).not.toBeNull();
    expect(indicator?.hasAttribute("data-underline-indicator")).toBe(true);

    const allBtn = host.querySelector<HTMLButtonElement>('[data-usage-filter="all"]')!;
    const workingBtn = host.querySelector<HTMLButtonElement>('[data-usage-filter="working"]')!;
    const needsBtn = host.querySelector<HTMLButtonElement>('[data-usage-filter="needs"]')!;

    expect(allBtn.getAttribute("role")).toBe("radio");
    expect(allBtn.getAttribute("aria-checked")).toBe("true");
    expect(allBtn.getAttribute("data-selected")).toBe("true");
    expect(allBtn.hasAttribute("data-checked")).toBe(true);
    expect(allBtn.tabIndex).toBe(0);

    expect(workingBtn.getAttribute("role")).toBe("radio");
    expect(workingBtn.getAttribute("aria-checked")).toBe("false");
    expect(workingBtn.getAttribute("data-selected")).toBe("false");
    expect(workingBtn.hasAttribute("data-unchecked")).toBe(true);
    expect(workingBtn.tabIndex).toBe(-1);
    expect(workingBtn.getAttribute("aria-label")).toBe("Working 3");
    expect(workingBtn.textContent).toBe("Working3");
    expect(workingBtn.className).toContain("text-ink-2");
    expect(needsBtn.className).toContain("text-ink-3");

    act(() => workingBtn.click());
    expect(onChange).toHaveBeenCalledWith("working");
  });

  it("supports keyboard roving focus with ArrowRight and ArrowLeft", () => {
    const onChange = vi.fn();
    act(() => {
      root.render(<ControlledUnderline onChange={onChange} />);
    });

    const allBtn = host.querySelector<HTMLButtonElement>('[data-usage-filter="all"]')!;
    const workingBtn = host.querySelector<HTMLButtonElement>('[data-usage-filter="working"]')!;
    const needsBtn = host.querySelector<HTMLButtonElement>('[data-usage-filter="needs"]')!;

    act(() => allBtn.focus());
    expect(document.activeElement).toBe(allBtn);

    act(() => {
      allBtn.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true }));
    });
    expect(onChange).toHaveBeenLastCalledWith("working");
    expect(workingBtn.getAttribute("aria-checked")).toBe("true");
    expect(allBtn.getAttribute("aria-checked")).toBe("false");
    expect(document.activeElement).toBe(workingBtn);

    act(() => {
      workingBtn.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true }));
    });
    expect(onChange).toHaveBeenLastCalledWith("needs");
    expect(needsBtn.getAttribute("aria-checked")).toBe("true");
    expect(document.activeElement).toBe(needsBtn);

    act(() => {
      needsBtn.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true, cancelable: true }));
    });
    expect(onChange).toHaveBeenLastCalledWith("working");
    expect(workingBtn.getAttribute("aria-checked")).toBe("true");
    expect(document.activeElement).toBe(workingBtn);
  });

  it("accepts legacy role prop without breaking native SegmentedControl radiogroup semantics", () => {
    const onChange = vi.fn();
    act(() => {
      root.render(
        <UnderlineTabs
          items={ITEMS}
          value="all"
          onChange={onChange}
          role="group"
          ariaLabel="Devices"
          wrap
        />,
      );
    });

    const group = host.querySelector<HTMLElement>('[role="radiogroup"]');
    expect(group?.getAttribute("aria-label")).toBe("Devices");
    expect(host.querySelectorAll('[role="radio"]')).toHaveLength(3);
  });
});

describe("FloatingSegment", () => {
  it("renders with data-variant='float', indicator slot, and switches values on click and ArrowRight", () => {
    const onChange = vi.fn();
    act(() => {
      root.render(<ControlledSegment onChange={onChange} />);
    });

    const container = host.querySelector<HTMLElement>('[role="radiogroup"]');
    expect(container).not.toBeNull();
    expect(container?.getAttribute("aria-label")).toBe("Display mode");
    expect(container?.getAttribute("data-variant")).toBe("float");
    expect(host.querySelector('[data-slot="segmented-control-indicator"]')).not.toBeNull();

    const detailedBtn = host.querySelector<HTMLButtonElement>('[data-mode="detailed"]')!;
    const compactBtn = host.querySelector<HTMLButtonElement>('[data-mode="compact"]')!;

    expect(detailedBtn.textContent).toBe("详细");
    expect(compactBtn.textContent).toBe("紧凑");
    expect(detailedBtn.getAttribute("role")).toBe("radio");
    expect(detailedBtn.getAttribute("aria-checked")).toBe("true");
    expect(compactBtn.getAttribute("aria-checked")).toBe("false");

    act(() => compactBtn.click());
    expect(onChange).toHaveBeenLastCalledWith("compact");
    expect(compactBtn.getAttribute("aria-checked")).toBe("true");
    expect(detailedBtn.getAttribute("aria-checked")).toBe("false");

    act(() => compactBtn.focus());
    act(() => {
      compactBtn.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true }));
    });
    expect(onChange).toHaveBeenLastCalledWith("detailed");
    expect(detailedBtn.getAttribute("aria-checked")).toBe("true");
    expect(compactBtn.getAttribute("aria-checked")).toBe("false");
    expect(document.activeElement).toBe(detailedBtn);
  });
});
