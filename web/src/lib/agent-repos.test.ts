import { describe, expect, it } from "vitest";
import { repoOf, sessionWorkspace } from "./agent-repos";
import type { AgentState, Session } from "./types";

const date = (hour: number) => `2026-01-01T${String(hour).padStart(2, "0")}:00:00Z`;
const session = (id: string, cwd: string, patch: Partial<Session> = {}): Session => ({
  machine_id: "machine-a", session_id: id, title: id, cwd, status: "idle" as AgentState, state: "idle" as AgentState, updated_at: date(1), ...patch,
});

describe("从 cwd 推断仓库与工作树", () => {
  it("主工作树就是仓库目录本身", () => {
    expect(repoOf("/code/app")).toEqual({ repo: "/code/app", name: "app", worktree: "/code/app", main: true });
  });

  it("常见的工作树布局都归到仓库根", () => {
    for (const cwd of ["/code/app/.worktrees/fix-login", "/code/app/.claude/worktrees/fix-login"]) {
      const got = repoOf(cwd);
      expect(got.repo).toBe("/code/app");
      expect(got.name).toBe("app");
      expect(got.worktree).toBe(cwd);
      expect(got.main).toBe(false);
    }
  });

  it("~/.worktrees/<仓库>/<工作树> 这类集中布局也认得仓库", () => {
    const got = repoOf("/home/me/.worktrees/persona/fix-login");
    expect(got.repo).toBe("/home/me/.worktrees/persona");
    expect(got.name).toBe("persona");
    expect(got.worktree).toBe("/home/me/.worktrees/persona/fix-login");
    expect(got.main).toBe(false);
  });

  it("没有工作树标记时，目录本身就是仓库", () => {
    expect(repoOf("/code/app/server/internal")).toEqual({ repo: "/code/app/server/internal", name: "internal", worktree: "/code/app/server/internal", main: true });
    expect(repoOf("")).toEqual({ repo: "", name: "", worktree: "", main: true });
  });

  it("宿主给了仓库（repo_root，#2840）时按它分：在它里面是主工作树，别处是它的工作树", () => {
    const wt = "/home/me/.paseo/worktrees/1a2b3c4d/brave-fox";
    expect(repoOf(wt, "/code/app")).toEqual({ repo: "/code/app", name: "app", worktree: wt, main: false });
    expect(repoOf("/code/app", "/code/app/")).toEqual({ repo: "/code/app", name: "app", worktree: "/code/app", main: true });
    expect(repoOf("/code/app/server", "/code/app")).toEqual({ repo: "/code/app", name: "app", worktree: "/code/app", main: true });
    expect(repoOf("/code/app-2", "/code/app").main).toBe(false);
    expect(repoOf(wt, "")).toEqual(repoOf(wt)); // an empty repo_root is the same as none
  });
});

  expect(built.map((m) => m.machine.id)).toEqual(["machine-a"]);
  return built[0]!.repos;
};

describe("Windows repository paths", () => {
  it("recognizes both separators and matches a repository root without case differences", () => {
    expect(repoOf("c:\\work\\app\\src", "C:/Work/App/")).toEqual({
      repo: "C:/Work/App", name: "App", worktree: "C:/Work/App", main: true,
    });
    expect(repoOf("C:\\Work\\App-extra", "C:/Work/App").main).toBe(false);
    expect(repoOf("C:\\Work\\App", "C:\\").main).toBe(true);
    expect(repoOf("\\\\SERVER\\SHARE\\Project\\src", "//server/share/project").main).toBe(true);
  });

  it("preserves worktree marker offsets in Unicode Windows paths", () => {
    expect(repoOf("C:\\İ\\App\\.CLAUDE\\WORKTREES\\fix-login")).toEqual({
      repo: "C:/İ/App", name: "App", worktree: "C:/İ/App/.CLAUDE/WORKTREES/fix-login", main: false,
    });
  });

  it("matches desktop sessions to Windows workspace directories including drive and share roots", () => {
    const workspace = (cwd: string) => ({ id: "workspace", machine_id: "machine-a", source: "directory", source_id: cwd, title: "Project", cwd });
    const root = workspace("C:\\");
    const project = workspace("C:\\Work\\App");
    expect(sessionWorkspace(session("desktop", "c:/work/app/src", { space_kind: "codex_app" }), [root, project])).toBe(project);
    expect(sessionWorkspace(session("desktop", "c:/other", { space_kind: "codex_app" }), [root, project])).toBe(root);
    const share = workspace("\\\\SERVER\\SHARE\\");
    expect(sessionWorkspace(session("desktop", "//server/share/project"), [share])).toBe(share);
    expect(sessionWorkspace(session("desktop", "/work/a/b"), [workspace("/work/a\\b")])).toBeUndefined();
    expect(sessionWorkspace(session("desktop", "c:/work/app/.claude/worktrees/child"), [project])).toBeUndefined();
  });

  it.each(["codex_app", "claude_desktop"])("uses directory placement when a %s project id has no exact catalog entry", (host) => {
    const project = { id: "workspace", machine_id: "machine-a", source: "directory", source_id: "directory-project", title: "Project", cwd: "C:\\Work\\App" };
    const desktop = session("desktop", "c:/work/app/src", { space_kind: host, space_id: "desktop-project" });
    expect(sessionWorkspace(desktop, [project])).toBe(project);
    expect(sessionWorkspace({ ...desktop, workspace_id: "explicit-binding" }, [project])).toBeUndefined();
    const exact = { ...project, id: "desktop-workspace", source: host, source_id: "desktop-project" };
    expect(sessionWorkspace(desktop, [project, exact])).toBe(exact);
  });
});
