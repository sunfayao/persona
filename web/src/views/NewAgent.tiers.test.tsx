// @vitest-environment jsdom
// Model-profile Agent presets (tiers) stay on the hub for Assistant and A2A dispatch; the web model selector's
// Recent and favorites view lists only saved favorites and recent picks, never tier rows such as backend or frontend.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Machine } from "../lib/types";

const state = vi.hoisted(() => ({ loaded: true, features: [] as string[], machines: [] as Machine[] }));
vi.mock("../lib/current-assistant", async () => {
  const store = (await import("../lib/store")) as unknown as { useStore: <T>(select: (s: { machines: unknown[] }) => T) => T };
  return { useScopedMachines: () => store.useStore((s) => s.machines) };
});
vi.mock("../lib/store", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/store")>();
  return {
    ...actual,
    getState: () => state,
    useStore: (selector: (value: typeof state) => unknown) => selector(state),
    hubHas: (features: string[], feature: string, unknown = false) => (features.length ? features.includes(feature) : unknown),
  };
});
vi.mock("react-dom", async (original) => ({ ...(await original<typeof import("react-dom")>()), createPortal: (children: React.ReactNode) => children }));

import { FolderAgentDraft } from "./FolderAgentDraft";
import { NewAgent } from "./NewAgent";
import { credentialOwner } from "../lib/api";
import { defaultManager } from "../lib/manager-choice";
import { resetModelListsForTests } from "../lib/session-models";
import { panelStoreKey } from "../ui/model-panel/store";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const TIERS = [
  { name: "backend", launcher: "claude", model: "opus", effort: "max", description: "Backend tasks" },
  { name: "frontend", launcher: "claude", model: "opus", effort: "max", description: "Frontend tasks" },
  { name: "simple", launcher: "agy", model: "gemini-4-neon-xhigh", description: "Simple tasks" },
];

let host: HTMLDivElement;
let root: Root;
const flush = () => act(async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); });
const pill = () => document.body.querySelector<HTMLButtonElement>("[data-model-pill]")!;
const panel = () => document.body.querySelector<HTMLElement>("[data-model-panel]");

let wideMode = true;
const mediaListeners = new Set<() => void>();

function installMatchMedia() {
  vi.stubGlobal("matchMedia", (q: string) => ({
    get matches() {
      return wideMode ? q === "(min-width: 768px)" || q === "(any-pointer: fine)" : false;
    },
    media: q,
    addEventListener(_type: string, fn: () => void) { mediaListeners.add(fn); },
    removeEventListener(_type: string, fn: () => void) { mediaListeners.delete(fn); },
  }));
}

function setWideMode(wide: boolean) {
  wideMode = wide;
  for (const fn of mediaListeners) fn();
}

beforeEach(() => {
  resetModelListsForTests();
  state.machines = [{ id: "mac", name: "Mac", online: true }];
  localStorage.clear();
  localStorage.setItem("new.cwd.mac", "/code/app");
  setWideMode(true);
  installMatchMedia();
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/model-config/purposes/")) return json({ error: "not found" }, 404);
    if (url.includes("/launchers/claude/models")) return json({ agent: "claude", models: [{ id: "default", name: "Default", default: true }, { id: "opus", name: "Opus 5.5", efforts: ["high", "max"] }] });
    if (url.includes("/launchers/agy/models")) return json({ agent: "agy", models: [{ id: "gemini-4-neon-xhigh", name: "Gemini 4 Neon (xhigh)", default_effort: "xhigh" }] });
    if (url.includes("/machines/mac/launchers")) return json({ launchers: [{ name: "claude", agent: "claude" }, { name: "agy", agent: "agy" }], tiers: TIERS });
    return json({ path: "/code", entries: [] });
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function openModelPanel() {
  act(() => pill().click());
  await act(async () => { await vi.dynamicImportSettled(); });
  await flush();
  expect(panel()).not.toBeNull();
}

describe("web model selector omits hub Agent presets (tiers)", () => {
  it.each([true, false])("FolderAgentDraft hides profile tiers in wide=%s mode and keeps Recent and favorites to saved setups only", async (wide) => {
    setWideMode(wide);
    localStorage.setItem(panelStoreKey("agent", "recent"), JSON.stringify([{ provider: "claude", model: "opus", effort: "high" }]));
    await act(async () =>
      root.render(
        <FolderAgentDraft
          draft={{
            id: "d1",
            key: "d1",
            owner: credentialOwner(),
            manager: defaultManager(),
            machine: "mac",
            cwd: "/code/app",
            label: "app",
            choice: { launcher: "", model: "", effort: "" },
            permissionMode: "",
            attachments: { images: [] },
            submitting: false,
          }}
          onToggleSidebar={() => {}}
        />,
      ),
    );
    await flush();
    await openModelPanel();

    if (wide) {
      const savedSide = [...document.body.querySelectorAll<HTMLButtonElement>("[data-panel-side]")].find((b) => b.dataset.panelSide === "\nsaved")!;
      expect(savedSide).not.toBeNull();
      act(() => savedSide.click());
      await flush();
    }

    expect(document.body.querySelector('[data-panel-section="presets"]')).toBeNull();
    const quick = document.body.querySelector<HTMLElement>("[data-panel-quick]")!;
    expect(quick).not.toBeNull();
    expect(quick.textContent).toContain("Opus 5.5");
    for (const tierName of ["backend", "frontend", "simple"]) {
      expect(panel()!.textContent).not.toContain(tierName);
    }
  });

  it("NewAgent hides Recent and favorites when there are no saved setups even if the hub returns tiers", async () => {
    await act(async () => root.render(<NewAgent open onClose={() => {}} />));
    await flush();
    await openModelPanel();

    expect([...document.body.querySelectorAll<HTMLButtonElement>("[data-panel-side]")].find((b) => b.dataset.panelSide === "\nsaved")).toBeUndefined();
    expect(document.body.querySelector('[data-panel-section="presets"]')).toBeNull();
    for (const tierName of ["backend", "frontend", "simple"]) {
      expect(panel()!.textContent).not.toContain(tierName);
    }
  });
});
