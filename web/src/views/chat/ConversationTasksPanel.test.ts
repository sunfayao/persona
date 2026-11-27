import { describe, expect, it } from "vitest";
import type { Todo } from "../../lib/todos";
import { conversationTasks } from "./ConversationTasksPanel";

function task(id: string, fields: Partial<Todo>): Todo {
  return { id, title: id, created_at: "", updated_at: "", ...fields };
}

describe("conversation tasks", () => {
  it("includes tasks dispatched by the main chat and tasks supervised by it, once each", () => {
    const supervised: NonNullable<Todo["supervision"]> = { supervisor: "main", done_when: [], round: 0, reviews: [] };
    const todos = [
      task("dispatched", { origin: "main", supervision: { ...supervised, supervisor: "background" } }),
      task("supervised", { origin: "other", supervision: supervised }),
      task("both", { origin: "main", supervision: supervised }),
      task("unrelated", { origin: "other" }),
      task("other-manager", { origin: "main", manager: "another" }),
    ];
    expect(conversationTasks(todos, "main", "home", "home").map((todo) => todo.id))
      .toEqual(["dispatched", "supervised", "both"]);
  });

  it("includes todos linked to an agent conversation via todo.agents, responsible, or session todo links", () => {
    const todos = [
      task("linked-agent", {
        agents: [{ machine_id: "mac", session_id: "agent-1", linked_at: "2026-11-27T00:00:00Z" }],
      }),
      task("dismissed-agent", {
        agents: [{ machine_id: "mac", session_id: "agent-1", linked_at: "2026-11-27T00:00:00Z", dismissed_at: "2026-11-27T01:00:00Z" }],
      }),
      task("other-machine", {
        agents: [{ machine_id: "pc", session_id: "agent-1", linked_at: "2026-11-27T00:00:00Z" }],
      }),
      task("responsible-agent", {
        responsible: { kind: "session", name: "Agent 1", machine_id: "mac", session_id: "agent-1" },
      }),
      task("session-link-only", { manager: "other-mgr" }),
      task("unrelated", { origin: "main" }),
    ];
    expect(
      conversationTasks(todos, "agent-1", "home", "home", {
        mid: "mac",
        linkedTodoIds: new Set(["session-link-only"]),
      }).map((todo) => todo.id),
    ).toEqual(["linked-agent", "responsible-agent", "session-link-only"]);
  });
});
