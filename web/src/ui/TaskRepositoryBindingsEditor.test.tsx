// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
vi.hoisted(() => { window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia; });
import { initI18n } from "../i18n";
import { credentialOwner, setToken } from "../lib/api";
import type { TaskRepositorySet } from "../lib/task-repositories";
import { TodoRepositoryBindings } from "./TaskRepositoryBindingsEditor";
import { Toasts } from "./atoms";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
type Call = { path: string; init: RequestInit; answered?: boolean; answer: (body: unknown, status?: number) => void };
let root: Root, host: HTMLDivElement, calls: Call[];
const identity = { provider: "github" as const, instance_url: "https://github.com", path: "example/web", canonical_url: "https://github.com/example/web" };
const set: TaskRepositorySet = { schema_version: 1, manager: "assistant", todo_id: "todo_example", revision: 1, repositories: [{ repo_id: "web", requested_ref: { kind: "default" }, source_snapshot: { identity, clone_url: identity.canonical_url, catalog_revision: 4, transport_origin: "canonical_default" }, links: [{ kind: "pull_request", number: "7", canonical_url: identity.canonical_url + "/pull/7" }], catalog_state: "current" }] };
const emptySet: TaskRepositorySet = { schema_version: 1, manager: "assistant", todo_id: "todo_example", revision: 1, repositories: [] };
const twoLinks: TaskRepositorySet = { ...set, revision: 2, repositories: [{ ...set.repositories[0], links: [
  { kind: "pull_request", number: "7", canonical_url: identity.canonical_url + "/pull/7" },
  { kind: "pull_request", number: "8", canonical_url: identity.canonical_url + "/pull/8" },
] }] };
const gitlabIdentity = { provider: "gitlab" as const, instance_url: "https://gitlab.example.com", path: "team/api", canonical_url: "https://gitlab.example.com/team/api" };
const gitlabSet: TaskRepositorySet = { schema_version: 1, manager: "assistant", todo_id: "todo_example", revision: 1, repositories: [{
  repo_id: "api", requested_ref: { kind: "default" },
  source_snapshot: { identity: gitlabIdentity, clone_url: gitlabIdentity.canonical_url, catalog_revision: 4, transport_origin: "canonical_default" },
  links: [{ kind: "pull_request", number: "9", canonical_url: gitlabIdentity.canonical_url + "/-/merge_requests/9" }], catalog_state: "current",
}] };
const titledSet: TaskRepositorySet = { ...twoLinks, repositories: [{ ...twoLinks.repositories[0], links: [
  { kind: "pull_request", number: "7", canonical_url: identity.canonical_url + "/pull/7", title: `Fix "quoted" <b>redirect</b> for the login flow across every provider` },
  { kind: "issue", number: "8", canonical_url: identity.canonical_url + "/issues/8", title: "Reported crash on the first screen" },
] }] };

const answer = async (call: Call, body: unknown, status = 200) => { call.answered = true; await act(async () => call.answer(body, status)); };
const legacy = (url: string) => ({ url, text: url.endsWith("/3211") ? "PR #3211" : "PR" });
const render = async (manager = "assistant", todoId = "todo_example", prURL?: string, version?: unknown) => act(async () => root.render(<><TodoRepositoryBindings manager={manager} todoId={todoId} owner={credentialOwner()} legacyPRs={prURL ? [legacy(prURL)] : []} version={version} /><Toasts /></>));
const section = () => document.querySelector("[data-todo-repositories]")!;

beforeEach(async () => {
  await initI18n("en"); setToken("synthetic-task-repository-session"); calls = [];
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  vi.stubGlobal("fetch", vi.fn((url: RequestInfo | URL, init: RequestInit = {}) => new Promise<Response>((resolve) => calls.push({ path: String(url).split("/api/v1")[1], init, answer: (body, status = 200) => resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })) }))));
});
afterEach(async () => { act(() => root.unmount()); host.remove(); document.body.innerHTML = ""; vi.unstubAllGlobals(); setToken(""); await initI18n("zh-Hans"); });

it("shows the Agent's saved links with no control to add, edit or remove one, and reads them again on a new version", async () => {
  await render("assistant", "todo_example", undefined, "v1");
  expect(calls.map((c) => c.path)).toEqual(["/todos/todo_example/repositories"]);
  await answer(calls[0], twoLinks);
  expect(section().textContent).toContain("#7");
  expect(section().textContent).toContain("#8");
  // links open their pages; nothing in the section changes the store
  expect(section().querySelectorAll("button, input, select, textarea")).toHaveLength(0);
  expect(calls.every((c) => !c.init.method || c.init.method === "GET")).toBe(true);
  await render("assistant", "todo_example", undefined, "v2");
  expect(calls.map((c) => c.path)).toEqual(["/todos/todo_example/repositories", "/todos/todo_example/repositories"]);
  await answer(calls[1], set);
  expect(section().textContent).toContain("#7");
  expect(section().textContent).not.toContain("#8");
});

it("says nothing is linked on a hub without the association route and no legacy PR", async () => {
  await render();
  await answer(calls[0], { code: "not_found", error: "Not Found" }, 404);
  expect(section().textContent).toContain("No repositories linked");
});

it("keeps a source snapshot whose catalog entry changed", async () => {
  await render();
  await answer(calls[0], { ...set, repositories: [{ ...set.repositories[0], catalog_state: "identity_changed" }] });
  expect(section().textContent).toContain("This saved repository now has a different source.");
  expect(section().textContent).toContain("#7");
});

it("reports a current read failure normally", async () => {
  await render();
  await answer(calls[0], { code: "task_repositories_store_unavailable", error: "unavailable" }, 503);
  expect(document.querySelector("[data-todo-repositories]")!.textContent).toContain("Couldn't confirm repository links were loaded or saved.");
});

it("falls back to the legacy PR link on a hub without the association route", async () => {
  await render("assistant", "todo_example", "https://example.com/pull/3211");
  await answer(calls[0], { code: "not_found", error: "Not Found" }, 404);
  const link = document.querySelector<HTMLAnchorElement>('[data-todo-repositories] a[href="https://example.com/pull/3211"]');
  expect(link).toBeTruthy();
  expect(link!.textContent).toBe("PR #3211");
  expect(document.querySelector("[data-todo-repositories]")!.textContent).toContain("Linked before repositories were stored here.");
});

it("never draws the legacy link beside the store's links", async () => {
  await render("assistant", "todo_example", "https://example.com/pull/3211");
  await answer(calls[0], twoLinks);
  const section = document.querySelector("[data-todo-repositories]")!;
  expect(section.querySelector('a[href="https://example.com/pull/3211"]')).toBeNull();
  expect(section.textContent).toContain("#7");
});

// Backend 2fb96c7b1 imports the legacy PR history into the canonical store once, and an explicit removal sticks. A
// successful empty answer is therefore the whole truth: the old pr_url must not come back as a fallback (#review).
it("trusts a successful empty store over an existing legacy PR", async () => {
  await render("assistant", "todo_example", "https://example.com/pull/3211");
  await answer(calls[0], emptySet);
  const section = document.querySelector("[data-todo-repositories]")!;
  expect(section.querySelector('a[href="https://example.com/pull/3211"]')).toBeNull();
  expect(section.textContent).toContain("No repositories linked");
});

it("draws the provider's own mark and the project address inline, with a merge mark for a GitLab merge request", async () => {
  await render();
  await answer(calls[0], gitlabSet);
  const section = document.querySelector("[data-todo-repositories]")!;
  const mark = section.querySelector('[data-repository-mark="gitlab"]')!;
  expect(mark).toBeTruthy();
  const row = section.querySelector("[data-repository-row]")!;
  // The mark is inline in the row itself, not inside the old 28 px tile.
  expect(mark.parentElement).toBe(row);
  expect(row.querySelector("a")!.getAttribute("href")).toBe(gitlabIdentity.canonical_url);
  expect(row.textContent).toContain("gitlab.example.com/team/api");
  expect(section.textContent).not.toContain("GitLab ·");
  expect(section.querySelector('[data-link-icon="merge"]')).toBeTruthy();
  expect(section.querySelector('a[href$="/-/merge_requests/9"]')).toBeTruthy();
});

it("puts a saved link's complete title in its single hyperlink instead of only type and number", async () => {
  await render();
  await answer(calls[0], titledSet);
  const links = [...document.querySelectorAll<HTMLAnchorElement>("[data-repository-link] a")];
  expect(links).toHaveLength(2);
  expect(links[0].textContent).toContain(`Fix "quoted" <b>redirect</b> for the login flow across every provider`);
  expect(links[0].textContent).toContain("#7");
  expect(links[1].textContent).toContain("Reported crash on the first screen");
  expect(links[1].textContent).toContain("#8");
  // A title is plain text: markup in it is escaped, never drawn, and the whole text stays in one hyperlink.
  expect(document.querySelector("[data-todo-repositories]")!.innerHTML).not.toContain("<b>redirect</b>");
  expect(links[0].querySelector("span")!.textContent).toBe(`Fix "quoted" <b>redirect</b> for the login flow across every provider`);
});

it("ignores late results after manager, todo or credential changes", async () => {
  await render();
  const old = [...calls];
  await render("other", "todo_other");
  // The retired read's answer lands on a page that has moved on: nothing of it is drawn.
  await answer(old[0], set);
  expect(document.querySelector("[data-todo-repositories]")!.textContent).not.toContain("#7");
  const pending = calls.slice(1);
  expect(pending).toHaveLength(1);
  setToken("replacement-task-repository-session");
  await answer(pending[0], { ...set, manager: "other", todo_id: "todo_other" });
  expect(document.querySelector("[data-todo-repositories]")!.textContent).not.toContain("#7");
});

// An extension's repository on a placeholder host: its links keep the addresses the manager stored.
const acme = { provider: "ext:acme-code/acme", instance_url: "https://code.acme.test", path: "acme-code/acme", canonical_url: "https://code.acme.test/acme-code/acme" };
const acmeReview = { kind: "pull_request" as const, number: "12", canonical_url: acme.canonical_url + "/reviews/12" };
const acmeIssue = { kind: "issue" as const, number: "4", canonical_url: acme.canonical_url + "/tickets/4", title: "Login loops" };
const acmeSet = (state: "available" | "off" | "missing" = "available"): TaskRepositorySet => ({ schema_version: 1, manager: "assistant", todo_id: "todo_example", revision: 1, repositories: [{
  repo_id: "acme", requested_ref: { kind: "default" },
  source_snapshot: { identity: acme, clone_url: acme.canonical_url, catalog_revision: 4, transport_origin: "catalog", provider_name: "Acme Code" },
  links: [acmeReview, acmeIssue], catalog_state: "current", provider_state: state, link_kinds: [{ kind: "pull_request", label: "MR" }],
}] });

it("opens an extension's links at their stored addresses, in its own words, with a generic mark and its name", async () => {
  await render();
  await answer(calls[0], acmeSet());
  // No request beyond the to-do's own read: the row carries its source's state and words.
  expect(calls.map((c) => c.path)).toEqual(["/todos/todo_example/repositories"]);
  const section = document.querySelector("[data-todo-repositories]")!;
  const mark = section.querySelector('[data-repository-mark="extension"]')!;
  expect(mark.getAttribute("aria-hidden")).toBe("true");
  expect(mark.parentElement).toBe(section.querySelector("[data-repository-row]"));
  // The address first, then its source's name: a narrow line cuts the name before the repository's path.
  expect(section.querySelector("[data-repository-row] a")!.textContent).toBe("code.acme.test/acme-code/acme · Acme Code");
  expect(section.querySelector("[data-repository-row] a")!.getAttribute("title")).toBe(`${acme.canonical_url} · Acme Code`);
  expect(section.textContent).not.toContain("ext:");
  const links = [...section.querySelectorAll<HTMLAnchorElement>("[data-repository-link] a")];
  expect(links.map((a) => a.getAttribute("href"))).toEqual([acmeReview.canonical_url, acmeIssue.canonical_url]);
  // The source's word for a pull request, as given, and its number with #; an issue keeps the localized word under its title.
  expect(links[0].textContent).toContain("MR");
  expect(links[0].textContent).toContain("#12");
  expect(links[0].getAttribute("aria-label")).toBe("MR #12 · acme-code/acme");
  expect(section.querySelector('[data-link-icon="pullRequest"]')).toBeTruthy();
  expect(section.querySelector('[data-link-icon="merge"]')).toBeNull();
  expect(links[1].textContent).toContain("Login loops");
  expect(links[1].getAttribute("aria-label")).toBe("Issue #4 · acme-code/acme · Login loops");
  expect(section.textContent).not.toContain("Unavailable");
});

it("reads a row an extension's source cloned from its own address, and refuses that origin on a built-in row", async () => {
  await render();
  const set = acmeSet();
  set.repositories[0].source_snapshot = { ...set.repositories[0].source_snapshot, clone_url: "ssh://git@code.acme.test/acme-code/acme.git", transport_origin: "provider" };
  await answer(calls[0], set);
  expect(document.querySelector("[data-todo-repositories] [data-repository-row]")).toBeTruthy();
  act(() => root.unmount()); root = createRoot(host); calls = [];
  await render();
  await answer(calls[0], { ...set, repositories: [{ ...set.repositories[0], source_snapshot: { ...set.repositories[0].source_snapshot, identity, clone_url: identity.canonical_url }, links: [], provider_state: undefined, link_kinds: undefined }] });
  expect(document.querySelector("[data-todo-repositories] [data-repository-row]")).toBeNull();
  expect(document.querySelector("[data-todo-repositories]")!.textContent).toContain("Couldn't confirm repository links were loaded or saved.");
});

it("says an unavailable source on its row and keeps its links opening", async () => {
  await render();
  await answer(calls[0], acmeSet("missing"));
  expect(section().querySelector("[data-repository-row]")!.textContent).toContain("Unavailable");
  expect(section().querySelector("[data-repository-note]")!.textContent).toContain("Acme Code is unavailable.");
  expect(section().querySelector(`a[href="${acmeReview.canonical_url}"]`)).toBeTruthy();
});
