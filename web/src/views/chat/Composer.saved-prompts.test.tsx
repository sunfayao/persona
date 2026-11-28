// @vitest-environment jsdom
// Saved prompts in the composer's "/" list (#7329): read once a box has the focus, listed after the chat's own commands
// with "Saved" as their source, filtered as the rest are, and picking one puts its text in the box (never sends it); a
// change on another device (assistant_saved_prompts) shows in an open box. A box with no commands of its own still offers
// them.
import { act, useRef, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { t } from "../../i18n";

import type { SlashCommandCatalog } from "../../lib/types";

const hub = vi.hoisted(() => ({
  slash: new Map<string, Promise<SlashCommandCatalog>>(),
  requested: [] as string[],
  prompts: [] as { id: string; name: string; text: string }[],
  listeners: new Map<string, Set<(m: Record<string, unknown>) => void>>(),
}));
vi.mock("../../lib/api", async (original) => {
  const real = await original<typeof import("../../lib/api")>();
  return {
    ...real,
    api: vi.fn(async (path: string) => {
      if (path.includes("/slash-commands")) {
        hub.requested.push(path);
        return hub.slash.get(path) ?? { commands: [] };
      }
      if (path === "/assistant/saved-prompts") return { prompts: hub.prompts };
      throw new real.ApiError(404, "not found", {});
    }),
  };
});
vi.mock("../../lib/store", async (original) => ({
  ...(await original<typeof import("../../lib/store")>()),
  onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
    if (!hub.listeners.has(type)) hub.listeners.set(type, new Set());
    hub.listeners.get(type)!.add(l);
    return () => void hub.listeners.get(type)?.delete(l);
  },
}));
import { resetSavedPrompts } from "../../lib/saved-prompts";
import { Composer, type Outgoing } from "./Composer";
import { NewAgentSlashField } from "./NewAgentSlashField";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const box = () => host.querySelector("textarea")!;
const typeInto = (text: string) =>
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(box(), text);
    box().dispatchEvent(new Event("input", { bubbles: true }));
  });
const key = (name: string) => act(() => void box().dispatchEvent(new KeyboardEvent("keydown", { key: name, bubbles: true, cancelable: true })));
const rows = () => [...host.querySelectorAll("[role=option]")].map((o) => o.textContent);
const until = async (check: () => void) => {
  for (let i = 0; i < 200; i++) {
    await act(async () => { await new Promise((ok) => setTimeout(ok, 5)); });
    try {
      return check();
    } catch {
      // not yet
    }
  }
  check();
};
const saved = (name: string) => `${name}${t("chat-savedprompts.source")}`;

beforeEach(() => {
  hub.prompts = [
    { id: "sp1", name: "review", text: "Review this diff for security issues." },
    { id: "sp2", name: "standup", text: "Write today's standup from the commits." },
  ];
  hub.listeners.clear();
  hub.slash.clear();
  hub.requested.length = 0;
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: q.includes("fine"), addEventListener() {}, removeEventListener() {} }));
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  resetSavedPrompts();
});

it("lists them after the chat's own commands, filters them, and picking one fills the box without sending", async () => {
  const onSend = vi.fn(async (_: Outgoing) => true);
  act(() => root.render(<Composer draftKey="sp" placeholder="" onSend={onSend} />));
  act(() => box().focus());
  typeInto("/");
  await until(() => expect(rows().slice(-2)).toEqual([saved("review"), saved("standup")]));
  expect(rows().length).toBeGreaterThan(2);
  typeInto("/stand");
  expect(rows()).toEqual([saved("standup")]);
  key("Enter");
  expect(box().value).toBe("Write today's standup from the commits.");
  expect(onSend).not.toHaveBeenCalled();
});

it("shows another device's change in an open box", async () => {
  act(() => root.render(<Composer draftKey="sp" placeholder="" onSend={async () => true} />));
  act(() => box().focus());
  typeInto("/re");
  await until(() => expect(rows()).toContain(saved("review")));
  hub.prompts = [{ id: "sp1", name: "release", text: "Write the release notes." }];
  act(() => hub.listeners.get("assistant_saved_prompts")?.forEach((l) => l({ type: "assistant_saved_prompts" })));
  await until(() => expect(rows()).toContain(saved("release")));
  expect(rows()).not.toContain(saved("review"));
});

it("are offered where the chat has no commands of its own", async () => {
  act(() => root.render(<Composer draftKey="none" placeholder="" onSend={async () => true} commands={[]} />));
  typeInto("/");
  await until(() => expect(rows()).toEqual([saved("review"), saved("standup")]));
});

it("filters native commands, completes them with the keyboard, and keeps saved prompts", async () => {
  const path = "/sessions/computer/cli/slash-commands";
  hub.slash.set(path, Promise.resolve({ commands: [
    { name: "/compact", source: "builtin", description: "Free context" },
    { name: "/compare", source: "skill", description: "Compare the reports" },
  ] }));
  const onSend = vi.fn(async (_: Outgoing) => true);
  act(() => root.render(<Composer draftKey="native" placeholder="" slashTarget={{ machine: "computer", session: "cli" }} onSend={onSend} />));
  typeInto("/");
  await until(() => expect(rows()).toContain(saved("review")));
  await until(() => expect(rows().some((row) => row?.includes("/compact"))).toBe(true));
  typeInto("/co");
  expect(rows().filter((row) => row?.includes(t("chat-slashpalette.source_skill")))).toHaveLength(1);
  key("ArrowDown");
  key("Tab");
  expect(box().value).toBe("/compare ");
  expect(onSend).not.toHaveBeenCalled();
  expect(hub.requested).toEqual([path]);
});

it("discards a previous session's late catalog", async () => {
  const old = "/sessions/computer/old-cli/slash-commands";
  const next = "/sessions/second/next-cli/slash-commands";
  let finish!: (rows: SlashCommandCatalog) => void;
  hub.slash.set(old, new Promise((resolve) => { finish = resolve; }));
  hub.slash.set(next, Promise.resolve({ commands: [{ name: "/models", source: "builtin", description: "Choose a model" }] }));
  function render(machine: string, session: string) {
    act(() => root.render(<Composer draftKey="changing" placeholder="" slashTarget={{ machine, session }} />));
  }
  render("computer", "old-cli");
  typeInto("/");
  await until(() => expect(hub.requested).toContain(old));
  render("second", "next-cli");
  await until(() => expect(rows().some((row) => row?.includes("/models"))).toBe(true));
  await act(async () => finish({ commands: [{ name: "/foreign", source: "command", description: "Other computer" }] }));
  expect(rows().some((row) => row?.includes("/foreign"))).toBe(false);
});

it("requests the launcher's exact workspace and fills a command on click", async () => {
  const path = "/machines/computer/slash-commands?launcher=agy&cwd=%2Fproject+space";
  hub.slash.set(path, Promise.resolve({ commands: [{ name: "/local-skill", source: "skill", description: "Workspace action" }] }));
  function LaunchField() {
    const [value, setValue] = useState("");
    const ref = useRef<HTMLTextAreaElement>(null);
    return <NewAgentSlashField input={ref} value={value} onValue={setValue}
      target={{ machine: "computer", launcher: "agy", cwd: "/project space" }} />;
  }
  act(() => root.render(<LaunchField />));
  typeInto("/local");
  await until(() => expect(rows().some((row) => row?.includes("/local-skill"))).toBe(true));
  act(() => host.querySelector<HTMLButtonElement>("[role=option]")!.click());
  expect(box().value).toBe("/local-skill ");
  expect(hub.requested).toEqual([path]);
});
