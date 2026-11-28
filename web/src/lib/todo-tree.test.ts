import { describe, expect, it } from "vitest";
import { agentTreeState, groupTodosByProject, projectOf, STALE_MS, todoTreeState } from "./todo-tree";
import { makeSessionLookup, type Todo } from "./todos";
import { matchesFilter } from "./agent-filter";
import { sessionSource } from "./agent-groups";
import type { Session } from "./types";

const NOW = Date.parse("2026-11-05T12:00:00Z");
const fresh = new Date(NOW - 60_000).toISOString();
const s = (sid: string, state: Session["state"], cwd: string, patch: Partial<Session> = {}): Session => ({
  machine_id: "m1", session_id: sid, title: sid, cwd, status: state === "working" ? "working" : "idle", state, updated_at: fresh, ...patch,
});
const todo = (id: string, agents: Todo["agents"], patch: Partial<Todo> = {}): Todo => ({ id, title: id, created_at: fresh, updated_at: fresh, agents, ...patch });
const link = (sid: string, patch: object = {}) => ({ machine_id: "m1", session_id: sid, linked_at: fresh, ...patch });
const noMutes = { spaces: [], sessions: [] };

describe("projects of to-dos", () => {
  it("projectOf 取多数仓库", () => {
    expect(projectOf(["/r/a", "/r/b", "/r/b/.claude/worktrees/x"])).toBe("b");
    expect(projectOf([])).toBe("");
  });

  it("待办页按项目分段，没 Agent 的放最后", () => {
    const all = [s("a", "working", "/code/persona")];
    const groups = groupTodosByProject([todo("t1", [link("a")]), todo("t2", [])], makeSessionLookup(all), NOW);
    expect(groups.map((g) => [g.name, g.state, g.todos.map((t) => t.id)])).toEqual([["persona", "working", ["t1"]], ["未归类", "done", ["t2"]]]);
  });

  it("a folder holding a to-do the page has in Needs you says so, a failed start the hub closed too (audit item 13)", () => {
    const old = new Date(NOW - 3 * STALE_MS).toISOString();
    const failedStart = todo("f1", [], { done_at: old, updated_at: old, start_error: "machine offline", origin: "conv" });
    // by its agents alone it reads Done, and a quiet folder of it folds away
    expect(groupTodosByProject([failedStart], makeSessionLookup([]), NOW).map((g) => [g.state, g.folded])).toEqual([["done", true]]);
    const waiting = groupTodosByProject([failedStart, todo("t2", [])], makeSessionLookup([]), NOW, (td) => td.id === "f1");
    expect(waiting.map((g) => [g.state, g.folded, g.todos.map((t) => t.id)])).toEqual([["needs_you", false, ["f1", "t2"]]]);
  });

  it("待办页先按待办所属的知识库项目分段，再按仓库，都没有的最后 (#2581)", () => {
    const all = [s("a", "working", "/code/persona"), s("b", "needs_you", "/code/persona")];
    const inArea = (id: string, project: string, links: ReturnType<typeof link>[] = []) => ({ ...todo(id, links), project });
    const groups = groupTodosByProject(
      [todo("t1", [link("a")]), inArea("t2", "community"), todo("t3", []), inArea("t4", "community", [link("b")]), inArea("t5", "gone")],
      makeSessionLookup(all),
      NOW,
    );
    // an area's to-dos stay together whatever their agents' repository; its order kept; a project shows its slug
    expect(groups.map((g) => [g.key, g.name, g.todos.map((t) => t.id)])).toEqual([
      ["todos-kb:community", "community", ["t2", "t4"]],
      ["todos-kb:gone", "gone", ["t5"]],
      ["todos-project:persona", "persona", ["t1"]],
      ["todos-project:\u0000", "未归类", ["t3"]],
    ]);
    expect(groups[0].state).toBe("needs_you");
    // an area with nothing at work stays open however long it was quiet; a repository's folds as before
    const old = { updated_at: new Date(NOW - 3 * STALE_MS).toISOString() };
    const quiet = groupTodosByProject([todo("q1", [], { ...old, project: "community" }), todo("q2", [link("gone")], old)], makeSessionLookup([]), NOW);
    expect(quiet.map((g) => [g.name, g.state, g.folded, g.area ?? false])).toEqual([
      ["community", "done", false, true],
      ["未归类", "done", true, false],
    ]);
  });
});

describe("来源：飞书群 / 终端 / 桌面端 (#2526)", () => {
  it("botmux 是飞书群，终端引擎的窗格是终端，桌面 App 自己的对话是桌面端，其余没有", () => {
    expect(sessionSource(s("b", "idle", "/", { pane_id: "botmux:abc" }))).toBe("feishu");
    expect(sessionSource(s("h", "idle", "/", { pane_id: "w1:p1" }))).toBe("terminal");
    expect(sessionSource(s("x", "idle", "/", { space_kind: "codex_app", source_kind: "codex_history" }))).toBe("desktop");
    expect(sessionSource(s("d", "idle", "/", { source_kind: "runtime", source_instance_id: "dsh" }))).toBeUndefined();
    expect(sessionSource(s("n", "idle", "/"))).toBeUndefined();
    expect(matchesFilter(s("b", "idle", "/", { pane_id: "botmux:abc" }), "feishu", noMutes)).toBe(true);
    expect(matchesFilter(s("h", "idle", "/", { pane_id: "w1:p1" }), "feishu", noMutes)).toBe(false);
    expect(matchesFilter(s("x", "idle", "/", { space_kind: "codex_app" }), "terminal", noMutes)).toBe(false);
  });
});


describe("waiting in the background in the tree (#6045)", () => {
  const sess = (id: string, patch: Partial<Session>): Session => ({ machine_id: "m", session_id: id, state: "working", status: "done", ...patch }) as Session;
  it("ranks between Working and Done, for an agent and for a todo whose agents only wait", () => {
    const waiting = sess("w", { background_waiting: true });
    const busy = sess("b", {});
    expect(agentTreeState(waiting)).toBe("bg_waiting");
    expect(agentTreeState(sess("s", { background_waiting: true, starting: true }))).toBe("working");
    expect(todoTreeState("working", [waiting])).toBe("bg_waiting");
    expect(todoTreeState("working", [waiting, busy])).toBe("working");
    expect(todoTreeState("working")).toBe("working");
    expect(todoTreeState("needs_you", [waiting])).toBe("needs_you");
  });
});
