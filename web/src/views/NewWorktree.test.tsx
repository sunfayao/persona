// @vitest-environment jsdom
import { webcrypto } from "node:crypto";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { credentialOwner, setToken } from "../lib/api";
import { getState } from "../lib/store";
import { setRosterDefault } from "../lib/manager-choice";
import { deliverLaunch, launchAgent, onOpenNewAgent } from "../lib/new-agent";
import { NewWorktree } from "./NewWorktree";
import type { WorktreeRequest } from "../lib/worktree";
import { t } from "../i18n";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const info = { root: "/projects/demo", repoName: "Demo", path: "/worktrees", branch: "feature/current", automatic_name: true };
const rows = [
  { id: "qualified-source-a", machine_id: "computer", source: "herdr", source_id: "source-a", cwd: "/projects/demo", title: "Main checkout", project_id: "demo", project_name: "Demo" },
  { id: "qualified-source-b", machine_id: "computer", source: "herdr", source_id: "source-b", cwd: "/projects/demo-b", title: "Other checkout", project_id: "demo", project_name: "Demo" },
];
let root: Root; let host: HTMLDivElement; let request: WorktreeRequest;
let fetcher: ReturnType<typeof vi.fn<(url: string, init: { body: string; headers: HeadersInit }) => Promise<Response>>>; let closes: ReturnType<typeof vi.fn<() => void>>;
let calls: { url: string; body?: Record<string, unknown>; headers: Headers }[];
const created = { workspace_id: "actual-new", path: "/worktrees/generated-actual", branch: "dev/actual", terminal: "pane:runtime-created-tab" };
const flush = async () => { await act(async () => { await new Promise((resolve) => setTimeout(resolve, 12)); }); };
const button = (label: string) => [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label)!;
const field = (label: string) => {
  const el = [...document.querySelectorAll("label")].find((l) => l.textContent === label)!;
  return document.getElementById(el.htmlFor) as HTMLInputElement;
};
const click = async (label: string) => { await act(async () => button(label).click()); await flush(); };
const labelled = (label: string) => [...document.querySelectorAll("label")].some((l) => l.textContent === label);
async function input(label: string, value: string) {
  await act(async () => {
    const el = field(label); const proto = el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : el.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event(el.tagName === "SELECT" ? "change" : "input", { bubbles: true }));
  });
}
async function mount() { await act(async () => root.render(<NewWorktree request={request} onClose={closes} />)); await flush(); }
const creates = () => calls.filter((c) => c.url.endsWith("/worktree/create"));

beforeEach(() => {
  vi.stubGlobal("crypto", webcrypto); localStorage.clear(); sessionStorage.clear();
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  Element.prototype.scrollIntoView = vi.fn();
  setToken(`synthetic-component-${Math.random()}`); setRosterDefault("assistant");
  Object.assign(getState(), { loaded: true, launches: {}, sessions: {}, machines: [{ id: "computer", name: "Development computer", online: true }] });
  closes = vi.fn(); calls = [];
  request = { target: { mid: "computer", workspaceId: "source-a", host: "herdr", cwd: "/projects/demo", label: "Main checkout", owner: credentialOwner(), manager: "assistant" },
    changed: vi.fn(), onCreated: vi.fn() };
  fetcher = vi.fn(async (url, init) => {
    calls.push({ url: String(url), body: init?.body ? JSON.parse(init.body) : undefined, headers: new Headers(init?.headers) });
    if (String(url).endsWith("/workspaces")) return response({ workspaces: rows });
    if (String(url).endsWith("/worktree/prepare")) return response(info);
    if (String(url).endsWith("/worktree/create")) return response(created);
    return response({});
  }); vi.stubGlobal("fetch", fetcher);
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

it("creates without an Agent by default and opens the explicit M3 stage using acknowledged IDs/path", async () => {
  const launch = vi.fn(); const stop = onOpenNewAgent(launch); await mount();
  await input(t("worktree.branch_label"), "feature/existing");
  await click(t("worktree-composer.create"));
  expect(creates()).toHaveLength(1); expect(launch).not.toHaveBeenCalled();
  expect(creates()[0].body).toEqual({ workspace_id: "source-a", kind: "herdr", branch: "feature/existing", path: "/worktrees/feature-existing" });
  expect(document.body.textContent).toContain("/worktrees/generated-actual");
  expect(request.onCreated).toHaveBeenCalledWith(created, expect.objectContaining({ workspaceId: "actual-new", cwd: "/worktrees/generated-actual", host: "herdr" }));
  await click(t("worktree-composer.start_agent"));
  expect(launch).toHaveBeenCalledWith({ workspace: expect.objectContaining({ workspaceId: "actual-new", cwd: "/worktrees/generated-actual", owner: credentialOwner(), manager: "assistant", host: "herdr" }) });
  expect(creates()).toHaveLength(1); expect(closes).toHaveBeenCalledOnce(); stop();
});

it("lets the computer name the checkout where it can", async () => {
  await mount();
  await input(t("worktree-composer.naming"), "auto");
  expect(labelled(t("worktree.branch_label"))).toBe(false);
  await click(t("worktree-composer.create"));
  expect(creates()[0].body).toEqual({ workspace_id: "source-a", kind: "herdr", auto_name: true });
});

it("close/reopen during a create retains its result and never overwrites a separate new draft", async () => {
  let answer!: (value: Response) => void;
  const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation((url, init) => {
    if (String(url).endsWith("/worktree/create")) { calls.push({ url: String(url), body: JSON.parse(init.body), headers: new Headers(init.headers) }); return new Promise((resolve) => { answer = resolve; }); }
    return original(url, init);
  });
  await mount(); await input(t("worktree.branch_label"), "feature/first"); await click(t("worktree-composer.create"));
  await click(t("common.cancel")); await act(async () => root.render(null));
  const oldRequest = request;
  request = { ...request, target: { ...request.target, workspaceId: "source-b", label: "Other checkout", cwd: "/projects/demo-b" } };
  await mount(); await input(t("worktree.branch_label"), "feature/new-draft");
  await act(async () => answer(response(created))); await flush();
  expect(field(t("worktree.branch_label")).value).toBe("feature/new-draft");
  expect(oldRequest.onCreated).not.toHaveBeenCalled();
  await act(async () => root.render(null)); request = oldRequest; await mount();
  expect(document.body.textContent).toContain("/worktrees/generated-actual"); expect(creates()).toHaveLength(1);
  expect(request.onCreated).toHaveBeenCalledWith(created, expect.objectContaining({ workspaceId: "actual-new" }));
});

it("rejects a stale prepare without wiping the branch or a new source selection", async () => {
  let oldPrepare!: (value: Response) => void;
  const original = fetcher.getMockImplementation()!; let deferPrepare = false;
  fetcher.mockImplementation((url, init) => {
    if (deferPrepare && String(url).endsWith("/worktree/prepare") && JSON.parse(init.body).workspace_id === "source-a") return new Promise((resolve) => { oldPrepare = resolve; });
    return original(url, init);
  });
  await mount(); await input(t("worktree.branch_label"), "feature/intended");
  await input(t("worktree-composer.git_source"), JSON.stringify(["assistant", "computer", "herdr", "source-b"])); await flush();
  deferPrepare = true;
  await input(t("worktree-composer.git_source"), JSON.stringify(["assistant", "computer", "herdr", "source-a"])); await flush();
  await input(t("worktree-composer.git_source"), JSON.stringify(["assistant", "computer", "herdr", "source-b"])); await flush();
  await act(async () => { oldPrepare(response({ ...info, path: "/stale" })); }); await flush();
  expect(field(t("worktree.branch_label")).value).toBe("feature/intended");
  expect(field(t("worktree.path_label")).value).toBe("/worktrees/feature-intended");
  await click(t("worktree-composer.create")); expect(creates()[0].body?.workspace_id).toBe("source-b");
});

it("never shows pending/unknown as success and reuses the same request after reopening", async () => {
  let attempts = 0; const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation((url, init) => {
    if (String(url).endsWith("/worktree/create")) { calls.push({ url: String(url), body: JSON.parse(init.body), headers: new Headers(init.headers) }); attempts++; return Promise.resolve(response(attempts === 1 ? { state: "pending" } : created, attempts === 1 ? 202 : 200)); }
    if (String(url).includes("/ops/")) return Promise.resolve(response({ state: "done" }));
    return original(url, init);
  });
  await mount(); await input(t("worktree.branch_label"), "feature/pending"); await click(t("worktree-composer.create"));
  expect(document.body.textContent).toContain(t("worktree-composer.pending")); expect(request.changed).not.toHaveBeenCalled();
  await act(async () => root.render(null)); await mount();
  await click(t("worktree-composer.check_result"));
  expect(creates()[1].headers.get("X-Persona-Op-Id")).toBe(creates()[0].headers.get("X-Persona-Op-Id"));
  expect(creates()[1].body).toEqual(creates()[0].body); expect(request.changed).toHaveBeenCalled();
});

it("blocks synchronous keyboard submit after credentials or manager ownership changes", async () => {
  await mount();
  await act(async () => { setToken("different-owner"); document.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); });
  expect(creates()).toHaveLength(0); expect(document.body.textContent).toContain(t("worktree.owner_changed"));
});

it("asks for the branch and path where the computer cannot name the checkout itself", async () => {
  const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation((url, init) => String(url).endsWith("/worktree/prepare") ? Promise.resolve(response({ root: "/projects/demo", repoName: "Demo", branch: "main", path: "/worktrees" })) : original(url, init));
  await mount(); expect(labelled(t("worktree-composer.naming"))).toBe(false);
  await input(t("worktree.branch_label"), "feature/basic"); await click(t("worktree-composer.create"));
  expect(creates()[0].body).toEqual({ workspace_id: "source-a", kind: "herdr", branch: "feature/basic", path: "/worktrees/feature-basic" });
});

it("says why the repository could not be read, and offers to read it again", async () => {
  const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation((url, init) => String(url).endsWith("/worktree/prepare") ? Promise.resolve(response({ error: "Full device required" }, 403)) : original(url, init));
  await mount(); expect(document.body.textContent).toContain("Full device required");
  expect(button(t("worktree-composer.retry"))).toBeDefined(); expect(creates()).toHaveLength(0);
});

it("retries only the optional M3 launch after failure while retaining the acknowledged creation receipt", async () => {
  let failLaunch = true; let launchId = "";
  const original = fetcher.getMockImplementation()!;
  fetcher.mockImplementation((url, init) => {
    if (String(url).endsWith("/sessions")) {
      calls.push({ url: String(url), body: JSON.parse(init.body), headers: new Headers(init.headers) });
      return Promise.resolve(failLaunch ? response({ error: "Launcher unavailable", delivered: "none" }, 409) : response({ session: { machine_id: "computer", session_id: "new-agent", cwd: created.path, launcher: "custom-codex", status: "idle", state: "idle" } }));
    }
    return original(url, init);
  });
  const stop = onOpenNewAgent((preset) => {
    launchId = launchAgent({ machineId: preset.workspace!.machineId, cwd: preset.workspace!.cwd,
      workspaceId: preset.workspace!.workspaceId, launcher: "custom-codex", model: "chosen-model", effort: "high", text: "First message", attachments: [] });
  });
  await mount(); await input(t("worktree.branch_label"), "feature/agent"); await click(t("worktree-composer.create")); await click(t("worktree-composer.start_agent"));
  // the start's transport is a chunk loaded on the first delivery (lib/new-agent.ts): settle it, not a wait
  await act(async () => vi.dynamicImportSettled()); await flush();
  expect(getState().launches[launchId].phase).toBe("failed");
  failLaunch = false; await act(async () => deliverLaunch(launchId)); await flush();
  expect(getState().launches[launchId].phase).toBe("ready");
  const starts = calls.filter((c) => c.url.endsWith("/sessions"));
  expect(starts).toHaveLength(2);
  expect(starts[1].body).toMatchObject({ workspace_id: "actual-new", cwd: "/worktrees/generated-actual", launcher: "custom-codex", model: "chosen-model", effort: "high" });
  expect(starts[1].body).not.toHaveProperty("host");
  expect(calls.find((c) => c.url.endsWith("/messages"))?.body?.text).toBe("First message");
  expect(creates()).toHaveLength(1);
  await act(async () => root.render(null)); await mount();
  expect(document.body.textContent).toContain("/worktrees/generated-actual"); expect(creates()).toHaveLength(1); stop();
});
