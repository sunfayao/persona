import { describe, expect, it } from "vitest";
import { sessionWorkspace } from "./agent-repos";
import type { HostWorkspace } from "./workspace-catalog";
import type { Session } from "./types";

const workspace = (id: string, patch: Partial<HostWorkspace> = {}): HostWorkspace => ({ id: `qualified-${id}`, machine_id: "machine-a", source: "herdr", source_id: id, title: id, cwd: `/code/${id}`, project_id: "project-a", project_name: "Project A", ...patch });
const session = (id: string, workspaceId: string, patch: Partial<Session> = {}): Session => ({ session_id: id, machine_id: "machine-a", title: id, cwd: "/elsewhere", state: "idle", status: "idle", space_kind: "herdr", space_id: workspaceId, ...patch });
const desktops = [["codex_app", "codex-project:home"], ["claude_desktop", "claude-environment:account"]] as const;

describe("sessionWorkspace against the host catalog", () => {
  it("uses explicit placement ahead of directory inference and refuses a foreign host's binding", () => {
    const catalog = [workspace("a", { cwd: "/code/a" }), workspace("b", { cwd: "/code/b" })];
    expect(sessionWorkspace(session("agent", "b", { cwd: "/code/a", workspace_id: "b" }), catalog)?.source_id).toBe("b");
    expect(sessionWorkspace(session("foreign", "b", { cwd: "/code/a", space_kind: "tmux", workspace_id: "b" }), catalog)).toBeUndefined();
    expect(sessionWorkspace(session("unknown", "missing", { cwd: "/code/a" }), catalog)).toBeUndefined();
    expect(sessionWorkspace(session("other-manager", "a", { manager: "manager-b" }), catalog, "manager-a")).toBeUndefined();
  });

  it.each(desktops)("uses a %s linked worktree's common Git root when its cwd is outside the project's roots", (source, prefix) => {
    const project = `${prefix}:project-a`;
    const app = workspace("app", { source, source_id: project, cwd: "C:\\Projects\\App" });
    const docs = { ...app, id: "docs-root", cwd: "D:\\Docs" };
    const agent = session("worktree", project, { space_kind: source, cwd: "C:\\Worktrees\\fix", repo_root: "c:/projects/app" });
    expect(sessionWorkspace(agent, [app, docs])).toBe(app);
    expect(sessionWorkspace({ ...agent, cwd: "d:/docs/pages" }, [app, docs])).toBe(docs);
    expect(sessionWorkspace({ ...agent, repo_root: undefined, workspace_cwd: "D:\\Docs" }, [app, docs])).toBe(docs);
    expect(sessionWorkspace({ ...agent, repo_root: undefined }, [app, docs])).toBeUndefined();
  });

  it.each(desktops)("never cross-binds saved %s projects that share a directory", (source, prefix) => {
    const a = workspace("a", { source, source_id: `${prefix}:a`, cwd: "C:\\Projects\\App" });
    const b = { ...a, id: "project-b-root", source_id: `${prefix}:b` };
    const otherRoot = { ...a, id: "project-a-docs", cwd: "D:\\Docs" };
    const agent = session("agent", a.source_id, { space_kind: source, cwd: "c:/projects/app/src" });
    expect(sessionWorkspace(agent, [a, b, otherRoot])).toBe(a);
    expect(sessionWorkspace({ ...agent, space_id: b.source_id }, [a, b, otherRoot])).toBe(b);
    expect(sessionWorkspace(agent, [b])).toBeUndefined();
    expect(sessionWorkspace({ ...agent, cwd: "C:/Unrelated" }, [a, b, otherRoot])).toBeUndefined();
    expect(sessionWorkspace(agent, [a, { ...a, id: "duplicate-root" }, otherRoot])).toBeUndefined();
  });

  it.each([["codex_app", "codex-project:"], ["claude_desktop", "codex-project:"], ["claude_desktop", "claude-project:"]])("matches directory-derived %s placement under %s only when a unique saved project contains its directory", (source, directoryPrefix) => {
    const project = workspace("app", { source, source_id: `${directoryPrefix}profile:saved`, cwd: "C:\\Projects\\App" });
    const inferred = session("unassigned", `${directoryPrefix}C:\\Projects\\App`, { space_kind: source, cwd: "c:/projects/app/src" });
    expect(sessionWorkspace(inferred, [project])).toBe(project);
    expect(sessionWorkspace({ ...inferred, cwd: "C:\\Users\\User\\.codex\\worktrees\\fix" }, [project])).toBe(project);
    expect(sessionWorkspace(inferred, [project, { ...project, id: "other", source_id: `${directoryPrefix}profile:other` }])).toBeUndefined();
  });

  it("keeps shared directory project ids bound to their exact desktop host and legacy namespace", () => {
    const projectID = "codex-project:C:/Projects/App";
    const codex = workspace("codex", { source: "codex_app", source_id: projectID, cwd: "C:/Projects/App" });
    const claude = { ...codex, id: "claude", source: "claude_desktop" };
    const legacy = { ...claude, id: "legacy", source_id: "claude-project:C:/Projects/App" };
    const catalog = [codex, claude, legacy];
    const chat = session("desktop", projectID, { cwd: "c:/projects/app/src" });
    expect(sessionWorkspace({ ...chat, space_kind: "codex_app" }, catalog)).toBe(codex);
    expect(sessionWorkspace({ ...chat, space_kind: "claude_desktop" }, catalog)).toBe(claude);
    expect(sessionWorkspace({ ...chat, space_kind: "claude_desktop", space_id: legacy.source_id }, catalog)).toBe(legacy);
    expect(sessionWorkspace({ ...chat, space_kind: "claude_desktop", space_id: "claude-environment:profile:missing" }, catalog)).toBeUndefined();
  });

  it.each(desktops)("limits an unavailable %s project's fallback to generic directory workspaces", (source, prefix) => {
    const native = workspace("native", { source: "herdr", cwd: "C:/Projects/App" });
    const directory = { ...native, id: "directory", source: "directory", source_id: "directory-project" };
    const chat = session("desktop", `${prefix}:missing`, { space_kind: source, cwd: "c:/projects/app/src" });
    expect(sessionWorkspace(chat, [native])).toBeUndefined();
    expect(sessionWorkspace(chat, [native, directory])).toBe(directory);
    expect(sessionWorkspace({ ...chat, workspace_id: "explicit-missing" }, [native, directory])).toBeUndefined();
  });

  it("attributes unbound native Agents only to a unique deepest containing directory and respects worktree markers", () => {
    const a = workspace("a", { cwd: "/code/app" }), b = workspace("b", { cwd: "/code/app/child" });
    const native = session("native", "", { space_kind: undefined, cwd: "/code/app/child/src" });
    expect(sessionWorkspace(native, [a, b])).toBe(b);
    expect(sessionWorkspace(native, [a, b, { ...b, source_id: "other-id" }])).toBeUndefined();
    expect(sessionWorkspace(native, [a, b, { ...b, source: "herdr" }])).toBeUndefined();
    expect(sessionWorkspace({ ...native, space_id: "b" }, [a, b, { ...b, cwd: "/different", source: "herdr" }])).toBeUndefined();
    expect(sessionWorkspace({ ...native, cwd: "/tmp" }, [workspace("root", { cwd: "/" })])?.source_id).toBe("root");
    expect(sessionWorkspace({ ...native, cwd: "/code/app-copy/src" }, [a])).toBeUndefined();
    expect(sessionWorkspace({ ...native, cwd: "/code/app/.worktrees/independent" }, [a])).toBeUndefined();
    expect(sessionWorkspace({ ...native, cwd: "/outside", workspace_cwd: "/code/app/child" }, [a, b])).toBe(b);
    expect(sessionWorkspace({ ...native, machine_id: "machine-b" }, [a, b])).toBeUndefined();
  });

  it("keeps Windows directory inference inside its verified Git checkout", () => {
    const home = workspace("home", { source: "directory", cwd: "C:/Users/User" });
    const checkout = workspace("checkout", { source: "directory", cwd: "C:/Users/User/.codex/worktrees/fix/App" });
    const chat = session("windows", "codex-project:C:/Work/App", {
      space_kind: "codex_app", cwd: "c:/users/user/.codex/worktrees/fix/app/src", repo_root: "C:/Work/App",
      worktree: "C:/Users/User/.codex/worktrees/fix/App",
    });
    expect(sessionWorkspace(chat, [home])).toBeUndefined();
    expect(sessionWorkspace(chat, [home, checkout])).toBe(checkout);
  });

  it("keeps an Agent inside its working tree when a symlinked folder leads to its cwd", () => {
    const herdr = { source: "herdr", project_id: undefined, project_name: undefined };
    const home = workspace("home", { ...herdr, cwd: "/home/me" });
    const repo = workspace("repo", { ...herdr, cwd: "/home/me/code/app" });
    const physical = workspace("physical", { ...herdr, cwd: "/data/home/me/code/app" });
    const chat = session("chat", "codex-project:/data/home/me/code/app", {
      space_kind: "codex_app", cwd: "/home/me/code/app/web", workspace_cwd: "/data/home/me/code/app/web", repo_root: "/data/home/me/code/app",
    });
    expect(sessionWorkspace(chat, [home, repo])).toBe(repo);
    expect(sessionWorkspace(chat, [physical])).toBe(physical);
    expect(sessionWorkspace(chat, [home])).toBeUndefined();
  });

});
