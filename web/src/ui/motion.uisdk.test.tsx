// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    get matches() {
      return query.includes("reduce") && (globalThis as { __reduce?: boolean }).__reduce === true;
    },
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

import { springCurve, useAnimatedPresence } from "./motion";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let frames = new Map<number, FrameRequestCallback>();
let nextFrame = 0;
let host: HTMLDivElement;
let root: Root;

function flushFrames() {
  act(() => {
    const due = [...frames.values()];
    frames.clear();
    due.forEach((f) => f(performance.now()));
  });
}

function createMockAnimation() {
  let resolve!: () => void;
  const finished = new Promise<void>((r) => {
    resolve = r;
  });
  return {
    animation: {
      finished,
      finish: () => resolve(),
      cancel: () => {},
      effect: {
        getComputedTiming: () => ({ endTime: 200, localTime: 0 }),
      },
    } as unknown as Animation,
    resolve,
  };
}

function Pill({ present }: { present: boolean }) {
  const { mounted, ref } = useAnimatedPresence<HTMLDivElement>(present);
  if (!mounted) return null;
  return (
    <div ref={ref} data-presence-pill="">
      Jump to latest
    </div>
  );
}

beforeEach(() => {
  frames = new Map();
  nextFrame = 0;
  (globalThis as { __reduce?: boolean }).__reduce = false;
  vi.stubGlobal("requestAnimationFrame", (f: FrameRequestCallback) => {
    frames.set(++nextFrame, f);
    return nextFrame;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("springCurve (@ui-sdk/core/gesture)", () => {
  it("clamps endpoints to 0 and 1 and settles near 1", () => {
    const curve = springCurve();
    expect(curve(-0.25)).toBe(0);
    expect(curve(0)).toBe(0);
    expect(curve(1)).toBe(1);
    expect(curve(1.5)).toBe(1);
    expect(curve(0.1)).toBeGreaterThan(0);
    expect(curve(0.1)).toBeLessThan(1);
    expect(Math.abs(curve(0.95) - 1)).toBeLessThan(0.01);
  });

  it("is strictly monotonic without overshoot when critically damped (damping = 1)", () => {
    const curve = springCurve(0.36, 1);
    let prev = 0;
    for (let i = 1; i <= 20; i++) {
      const val = curve(i / 20);
      expect(val).toBeGreaterThanOrEqual(prev);
      expect(val).toBeLessThanOrEqual(1);
      prev = val;
    }
  });

  it("overshoots past 1 before settling when underdamped", () => {
    const curve = springCurve(0.36, 0.55);
    let peak = 0;
    for (let i = 1; i < 100; i++) {
      peak = Math.max(peak, curve(i / 100));
    }
    expect(peak).toBeGreaterThan(1.05);
    expect(curve(1)).toBe(1);
  });
});

describe("useAnimatedPresence (@ui-sdk/core animateIn / animateOut)", () => {
  it("animates in with data-starting-style and waits for exit animation before unmounting", async () => {
    act(() => root.render(<Pill present={false} />));
    expect(host.querySelector("[data-presence-pill]")).toBeNull();

    act(() => root.render(<Pill present />));
    const el = host.querySelector<HTMLDivElement>("[data-presence-pill]")!;
    expect(el).not.toBeNull();
    expect(el.hasAttribute("data-starting-style")).toBe(true);

    flushFrames();
    act(() => vi.advanceTimersByTime(10));
    expect(el.hasAttribute("data-starting-style")).toBe(false);

    const mock = createMockAnimation();
    el.getAnimations = () => [mock.animation];

    act(() => root.render(<Pill present={false} />));
    expect(host.querySelector("[data-presence-pill]")).toBe(el);
    expect(el.hasAttribute("data-ending-style")).toBe(true);
    expect(el.hasAttribute("inert")).toBe(true);

    await act(async () => {
      mock.resolve();
      await Promise.resolve();
    });
    expect(host.querySelector("[data-presence-pill]")).toBeNull();
  });

  it("cancels an in-flight exit cleanly if present flips back to true", async () => {
    act(() => root.render(<Pill present />));
    const el = host.querySelector<HTMLDivElement>("[data-presence-pill]")!;
    flushFrames();
    act(() => vi.advanceTimersByTime(10));

    const mock = createMockAnimation();
    el.getAnimations = () => [mock.animation];

    act(() => root.render(<Pill present={false} />));
    expect(el.hasAttribute("data-ending-style")).toBe(true);
    expect(el.hasAttribute("inert")).toBe(true);

    act(() => root.render(<Pill present />));
    expect(host.querySelector("[data-presence-pill]")).toBe(el);
    expect(el.hasAttribute("data-ending-style")).toBe(false);
    expect(el.hasAttribute("inert")).toBe(false);
    expect(el.hasAttribute("data-starting-style")).toBe(true);

    await act(async () => {
      mock.resolve();
      await Promise.resolve();
    });
    expect(host.querySelector("[data-presence-pill]")).toBe(el);
  });

  it("unmounts immediately under prefers-reduced-motion or without Web Animations", () => {
    (globalThis as { __reduce?: boolean }).__reduce = true;
    act(() => root.render(<Pill present />));
    const el = host.querySelector<HTMLDivElement>("[data-presence-pill]")!;
    expect(el.hasAttribute("data-starting-style")).toBe(false);

    const mock = createMockAnimation();
    el.getAnimations = () => [mock.animation];
    act(() => root.render(<Pill present={false} />));
    expect(host.querySelector("[data-presence-pill]")).toBeNull();

    (globalThis as { __reduce?: boolean }).__reduce = false;
    act(() => root.render(<Pill present />));
    const noWaapi = host.querySelector<HTMLDivElement>("[data-presence-pill]")!;
    expect(noWaapi).not.toBeNull();
    delete (noWaapi as { getAnimations?: unknown }).getAnimations;
    act(() => root.render(<Pill present={false} />));
    expect(host.querySelector("[data-presence-pill]")).toBeNull();
  });
});
