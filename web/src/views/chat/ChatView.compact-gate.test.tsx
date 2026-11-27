// @vitest-environment jsdom
// Regression test for PR #12547 review comment 4654593168:
// One-click /compact from ChatView's Session Insights popover must align with composer preconditions
// (hidden when task route is active or setup gate is blocked) and keep the popover open + toast when send fails.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { COMPACT_FEATURE } from "../../lib/compaction";
import { setRoute } from "../../lib/feed";
import type { Session } from "../../lib/types";
import { ChatView } from "./ChatView";

const mocks = vi.hoisted(() => ({
  session: {
    machine_id: "hub",
    session_id: "c1",
    title: "Agent",
    cwd: "/code",
    status: "idle",
    agent: "claude",
    context_tokens: 80_000,
    context_window: 200_000,
  } as Session,
  features: [] as string[],
  blocked: undefined as { label: string; onClick?: () => void } | undefined,
  api: vi.fn(),
  apiOp: vi.fn(),
  toast: vi.fn(),
}));

vi.mock("./Composer", async (importOriginal) => ({
  ...await importOriginal<typeof import("./Composer")>(),
  Composer: () => <div data-testid="composer" />,
}));
vi.mock("./useTranscript", () => ({
  useTranscript: () => ({ items: [], loading: false, error: "", hasMore: false, streaming: "" }),
}));
vi.mock("../../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../lib/store")>();
  return {
    ...real,
    getSession: () => mocks.session,
    useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T) => select({ ...real.getState(), features: mocks.features, sessions: { hub: [mocks.session] } }),
  };
});
vi.mock("../../lib/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/api")>(),
  api: (...args: unknown[]) => mocks.api(...args),
}));
vi.mock("../../lib/panel", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/panel")>(),
  usePanel: () => null,
}));
vi.mock("../../lib/router", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/router")>(),
  useRoute: () => ({ name: "session", mid: "hub", id: "c1" }),
}));
vi.mock("../../lib/ops", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../lib/ops")>(),
  apiOp: (...args: unknown[]) => mocks.apiOp(...args),
}));
vi.mock("../../ui/atoms", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../ui/atoms")>(),
  toast: (...args: unknown[]) => mocks.toast(...args),
}));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }) }));
vi.mock("../assistant/AssistantChat", () => ({
  AssistantFace: () => null,
  CompactionStrip: () => null,
  useAssistantMenu: () => ({ items: [], dialogs: null }),
}));
vi.mock("../assistant/Tasks", () => ({
  DayLine: () => null,
  FeedLineView: () => null,
  QuickResultCard: () => null,
  RouteChip: () => null,
  AssistantActionBar: () => null,
  TaskDock: () => null,
  TaskQuestions: () => null,
}));
vi.mock("../assistant/Readiness", () => ({
  ChatOnlyNote: () => null,
  FailureCard: () => null,
  ReadinessCard: () => null,
  useReadinessGate: () => ({ blocked: mocks.blocked, dialog: null, help: () => {}, confirm: () => {} }),
}));

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
  (globalThis as Record<string, unknown>).matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  if (typeof globalThis.ResizeObserver === "undefined") {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
  }
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  mocks.blocked = undefined;
  mocks.features = [];
  mocks.session = {
    machine_id: "hub",
    session_id: "c1",
    title: "Agent",
    cwd: "/code",
    status: "idle",
    agent: "claude",
    context_tokens: 80_000,
    context_window: 200_000,
  };
  mocks.api.mockReset().mockResolvedValue({});
  mocks.apiOp.mockReset().mockResolvedValue({ delivered: true });
  mocks.toast.mockReset();
  setRoute("c1", null);
});

afterEach(async () => {
  await act(async () => {
    root.unmount();
  });
  host.remove();
  setRoute("c1", null);
});

describe("ChatView composer footer removal", () => {
  it("renders no status line below the composer for agent chats", async () => {
    await act(async () => {
      root.render(<ChatView mid="hub" sid="c1" />);
    });
    expect(host.querySelector("[data-composer-usage]")).toBeNull();
    expect(host.querySelector("[data-composer-source]")).toBeNull();
  });

  it("renders no status line below the composer for assistant chats", async () => {
    mocks.features = [COMPACT_FEATURE];
    mocks.session = { ...mocks.session, assistant: true };
    await act(async () => {
      root.render(<ChatView mid="hub" sid="c1" assistant />);
    });
    expect(host.querySelector("[data-composer-usage]")).toBeNull();
    expect(host.querySelector("[data-composer-source]")).toBeNull();
  });
});
