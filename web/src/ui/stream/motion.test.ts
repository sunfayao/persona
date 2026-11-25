import { afterEach, describe, expect, it, vi } from "vitest";
import { replyMotion, scrollBehavior } from "./motion";

function stub(reduce: boolean) {
  vi.stubGlobal("matchMedia", () => ({ matches: reduce }));
}

afterEach(() => vi.unstubAllGlobals());

describe("回复动效", () => {
  it("is on unless the system asks for reduced motion", () => {
    stub(false);
    expect(replyMotion()).toBe(true);
    stub(true);
    expect(replyMotion()).toBe(false);
  });
});

describe("scrollBehavior", () => {
  it("animates unless the system asks for reduced motion", () => {
    stub(false);
    expect(scrollBehavior()).toBe("smooth");
    stub(true);
    expect(scrollBehavior()).toBe("auto");
  });

  it("animates where matchMedia is missing", () => {
    vi.stubGlobal("matchMedia", undefined);
    expect(scrollBehavior()).toBe("smooth");
  });
});
